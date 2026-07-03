package main

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	md2pdf "github.com/alnah/picoloom/v2"
	"github.com/trinova/md2pdf/transform"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "dev"

//go:embed all-options.yaml
var allOptionsYAML []byte

// Config mirrors the structure of all-options.yaml.
type Config struct {
	Document   DocumentConfig   `yaml:"document"`
	Author     AuthorConfig     `yaml:"author"`
	Input      InputConfig      `yaml:"input"`
	Output     OutputConfig     `yaml:"output"`
	Page       PageConfig       `yaml:"page"`
	PageBreaks PageBreaksConfig `yaml:"pageBreaks"`
	Cover      CoverConfig      `yaml:"cover"`
	TOC        TOCConfig        `yaml:"toc"`
	Signature  SignatureConfig  `yaml:"signature"`
	Watermark  WatermarkConfig  `yaml:"watermark"`
	Footer     FooterConfig     `yaml:"footer"`
	Assets     AssetsConfig     `yaml:"assets"`
	Style      string           `yaml:"style"`
	Timeout    string           `yaml:"timeout"`
}

type DocumentConfig struct {
	Title        string `yaml:"title"`
	Subtitle     string `yaml:"subtitle"`
	Version      string `yaml:"version"`
	Date         string `yaml:"date"`
	ClientName   string `yaml:"clientName"`
	ProjectName  string `yaml:"projectName"`
	DocumentType string `yaml:"documentType"`
	DocumentID   string `yaml:"documentID"`
	Description  string `yaml:"description"`
}

type AuthorConfig struct {
	Name         string `yaml:"name"`
	Title        string `yaml:"title"`
	Organization string `yaml:"organization"`
	Email        string `yaml:"email"`
	Phone        string `yaml:"phone"`
	Address      string `yaml:"address"`
	Department   string `yaml:"department"`
}

type InputConfig struct {
	File       string `yaml:"file"` // markdown input for config-only runs (relative to the config file)
	DefaultDir string `yaml:"defaultDir"`
}

type OutputConfig struct {
	DefaultDir string `yaml:"defaultDir"`
}

type PageConfig struct {
	Size        string  `yaml:"size"`
	Orientation string  `yaml:"orientation"`
	Margin      float64 `yaml:"margin"`
}

type PageBreaksConfig struct {
	Enabled  bool `yaml:"enabled"`
	BeforeH1 bool `yaml:"beforeH1"`
	BeforeH2 bool `yaml:"beforeH2"`
	BeforeH3 bool `yaml:"beforeH3"`
	Duplex   bool `yaml:"duplex"` // blank verso after cover/TOC for double-sided printing
	Orphans  int  `yaml:"orphans"`
	Widows   int  `yaml:"widows"`
}

type CoverConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Logo           string `yaml:"logo"`
	ShowDepartment bool   `yaml:"showDepartment"`
}

type TOCConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Title    string `yaml:"title"`
	MinDepth int    `yaml:"minDepth"`
	MaxDepth int    `yaml:"maxDepth"`
	Numbered *bool  `yaml:"numbered"` // nil/true = auto-number entries; false = list headings verbatim
}

type SignatureConfig struct {
	Enabled   bool         `yaml:"enabled"`
	ImagePath string       `yaml:"imagePath"`
	Links     []LinkConfig `yaml:"links"`
}

type LinkConfig struct {
	Label string `yaml:"label"`
	URL   string `yaml:"url"`
}

type WatermarkConfig struct {
	Enabled bool    `yaml:"enabled"`
	Text    string  `yaml:"text"`
	Color   string  `yaml:"color"`
	Opacity float64 `yaml:"opacity"`
	Angle   float64 `yaml:"angle"`
}

type FooterConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Position       string `yaml:"position"`
	ShowPageNumber bool   `yaml:"showPageNumber"`
	ShowDocumentID bool   `yaml:"showDocumentID"`
	Text           string `yaml:"text"`
}

type AssetsConfig struct {
	BasePath string `yaml:"basePath"`
}

func main() {
	app := &cli.Command{
		Name:    "md2pdf",
		Usage:   "Convert Markdown files to PDF",
		Version: Version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "work config `FILE` (YAML)",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "output PDF `FILE`; output directory when the input is a directory (default: input with .pdf extension)",
			},
			&cli.BoolFlag{
				Name:  "keep-workspace",
				Usage: "keep the transformer workspace directory and print its path (for debugging)",
			},
		},
		ArgsUsage: "<input.md | input-dir | config.yaml>",
		Action:    run,
		Commands: []*cli.Command{
			{
				Name:      "init",
				Usage:     "Write an example configuration YAML with every option",
				ArgsUsage: "[filename]",
				Description: "Writes an exhaustively-commented md2pdf.yaml template to the current\n" +
					"directory. With no filename (or 'auto'), the file is named\n" +
					"md2pdf-YYYY-MM-DD.yaml. A literal {date} token in the filename is\n" +
					"replaced with today's date.",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "force",
						Aliases: []string{"f"},
						Usage:   "overwrite existing file",
					},
				},
				Action: runInit,
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "md2pdf: %v\n", err)
		os.Exit(1)
	}
}

func runInit(ctx context.Context, cmd *cli.Command) error {
	date := time.Now().Format("2006-01-02")

	name := cmd.Args().First()
	if name == "" || name == "auto" {
		name = fmt.Sprintf("md2pdf-%s.yaml", date)
	} else {
		name = strings.ReplaceAll(name, "{date}", date)
	}

	if _, err := os.Stat(name); err == nil && !cmd.Bool("force") {
		return fmt.Errorf("%s already exists (use --force to overwrite)", name)
	}

	if err := os.WriteFile(name, allOptionsYAML, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Printf("Created %s\n", name)
	return nil
}

func run(ctx context.Context, cmd *cli.Command) error {
	configPath := cmd.String("config")
	inputPath := cmd.Args().First()

	// A lone YAML argument is a config that names its own input: explicitly
	// via input.file, or implicitly as <config-basename>.md next to it.
	if isYAMLPath(inputPath) {
		if configPath != "" {
			return fmt.Errorf("config given twice: -c %s and %s", configPath, inputPath)
		}
		configPath = inputPath
		inputPath = ""
	}

	if inputPath == "" && configPath == "" {
		return cli.ShowAppHelp(cmd)
	}

	// Load base config from YAML file
	cfg := &Config{}
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	if inputPath == "" {
		// Config-only invocation: the config names the input.
		var err error
		inputPath, err = inputFromConfig(configPath, cfg)
		if err != nil {
			return err
		}
	} else {
		// Resolve input path: if not found as given and input.defaultDir is set,
		// try joining with defaultDir before giving up.
		inputPath = resolveInputPath(inputPath, cfg.Input.DefaultDir)
	}

	// Anchor a config-relative cover logo at the config file's directory and
	// make it absolute: the rendered HTML lives in a temp dir, so a relative
	// image path would not resolve at print time.
	if l := cfg.Cover.Logo; l != "" && !strings.Contains(l, "://") && !strings.HasPrefix(l, "data:") && !filepath.IsAbs(l) {
		if abs, err := filepath.Abs(filepath.Join(filepath.Dir(configPath), l)); err == nil {
			cfg.Cover.Logo = abs
		}
	}

	// Resolve style CSS and build the converter once. Both derive only from
	// config-level fields (style, assets, timeout) that frontmatter never
	// overrides, so they are safely shared by every file in batch mode.
	css, err := resolveStyleCSS(cfg)
	if err != nil {
		return err
	}
	opts, err := converterOptions(cfg)
	if err != nil {
		return err
	}
	conv, err := md2pdf.NewConverter(opts...)
	if err != nil {
		return fmt.Errorf("initializing converter: %w", err)
	}
	defer conv.Close()

	// A directory as input selects batch mode: every *.md directly inside it
	// (non-recursive) becomes its own PDF in the output directory. A stat
	// error is deliberately ignored here — the single-file path below reports
	// the missing input as "reading input".
	if info, err := os.Stat(inputPath); err == nil && info.IsDir() {
		files, err := listMarkdownFiles(inputPath)
		if err != nil {
			return err
		}
		outDir := batchOutputDir(cmd.String("output"), cfg.Output.DefaultDir, inputPath)
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
		return convertBatch(files, outDir, func(in, out string) error {
			return convertFile(ctx, cmd, conv, css, *cfg, in, out)
		}, os.Stderr)
	}

	// Single file: resolve the output path.
	outPath := cmd.String("output")
	if outPath == "" {
		if cfg.Output.DefaultDir != "" {
			outPath = filepath.Join(cfg.Output.DefaultDir, pdfBaseName(inputPath))
		} else {
			outPath = filepath.Join(filepath.Dir(inputPath), pdfBaseName(inputPath))
		}
	}
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
	}

	return convertFile(ctx, cmd, conv, css, *cfg, inputPath, outPath)
}

// convertFile converts one markdown file to one PDF: read, frontmatter
// overlay, transformer pipeline, conversion, write. It receives cfg BY VALUE
// so the frontmatter overlay of one file never leaks into the next file of a
// batch. Config contains a single slice field (Signature.Links); the shallow
// copy shares its backing array, which is safe because applyFrontmatter never
// mutates Links.
func convertFile(ctx context.Context, cmd *cli.Command, conv *md2pdf.Converter, css string, cfg Config, inputPath, outPath string) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}

	// Extract frontmatter and body; frontmatter overrides config
	body, fm := extractFrontmatter(string(data))
	applyFrontmatter(fm, &cfg)

	// Run the transformer pipeline over the body in a disposable workspace.
	// The pipeline is empty for now; concrete transformers (Mermaid, …)
	// register here as they land. Generated files must be referenced by
	// absolute path, since Input.SourceDir stays at the source directory.
	ws, err := transform.NewWorkspace()
	if err != nil {
		return fmt.Errorf("creating workspace: %w", err)
	}
	if cmd.Bool("keep-workspace") {
		fmt.Fprintf(os.Stderr, "md2pdf: keeping workspace %s\n", ws.Dir())
	} else {
		defer ws.Cleanup()
	}
	body, err = transform.NewPipeline().Run(body, ws.Dir(), filepath.Dir(inputPath))
	if err != nil {
		return fmt.Errorf("transforming: %w", err)
	}

	// Resolve "auto" date
	if strings.EqualFold(cfg.Document.Date, "auto") {
		cfg.Document.Date = time.Now().Format("2 January 2006")
	}

	result, err := conv.Convert(ctx, buildInput(body, inputPath, css, &cfg))
	if err != nil {
		return fmt.Errorf("converting: %w", err)
	}

	if err := os.WriteFile(outPath, result.PDF, 0o644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	fmt.Printf("Created %s\n", outPath)
	return nil
}

// resolveStyleCSS loads the CSS for cfg.Style via the library's asset loader
// (respects assets.basePath). An empty style name means no CSS.
func resolveStyleCSS(cfg *Config) (string, error) {
	if cfg.Style == "" {
		return "", nil
	}
	loader, err := md2pdf.NewAssetLoader(cfg.Assets.BasePath)
	if err != nil {
		return "", fmt.Errorf("initializing asset loader: %w", err)
	}
	css, err := loader.LoadStyle(cfg.Style)
	if err != nil {
		return "", fmt.Errorf("loading style %q: %w", cfg.Style, err)
	}
	return css, nil
}

// converterOptions builds the md2pdf converter options from config.
func converterOptions(cfg *Config) ([]md2pdf.Option, error) {
	var opts []md2pdf.Option
	if cfg.Assets.BasePath != "" {
		opts = append(opts, md2pdf.WithAssetPath(cfg.Assets.BasePath))
	}
	if cfg.Timeout != "" {
		d, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("parsing timeout %q: %w", cfg.Timeout, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("timeout must be positive: %q", cfg.Timeout)
		}
		opts = append(opts, md2pdf.WithTimeout(d))
	}
	return opts, nil
}

// pdfBaseName returns the input's basename with its extension replaced by .pdf.
func pdfBaseName(inputPath string) string {
	return strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)) + ".pdf"
}

// listMarkdownFiles returns the paths of the *.md files directly inside dir
// (non-recursive), in the sorted order os.ReadDir guarantees. A directory
// without a single .md file is an error: batch mode would have nothing to do.
func listMarkdownFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading input directory: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .md files in %s", dir)
	}
	return files, nil
}

// batchOutputDir resolves the output directory for batch mode along the
// data-priority chain: -o flag → output.defaultDir from config → the input
// directory itself.
func batchOutputDir(flagOutput, cfgDefaultDir, inputDir string) string {
	if flagOutput != "" {
		return flagOutput
	}
	if cfgDefaultDir != "" {
		return cfgDefaultDir
	}
	return inputDir
}

// convertBatch converts every file to <outDir>/<basename>.pdf via convert,
// continuing past per-file failures. Failures are printed to errW at the end,
// one "file: error" line each, and folded into a single summary error so the
// process exits non-zero when any file failed.
func convertBatch(files []string, outDir string, convert func(inputPath, outPath string) error, errW io.Writer) error {
	var failures []string
	for _, f := range files {
		if err := convert(f, filepath.Join(outDir, pdfBaseName(f))); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", f, err))
		}
	}
	if len(failures) == 0 {
		return nil
	}
	for _, f := range failures {
		fmt.Fprintln(errW, f)
	}
	return fmt.Errorf("%d of %d files failed", len(failures), len(files))
}

// isYAMLPath reports whether the path looks like a YAML config file.
func isYAMLPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// inputFromConfig resolves the markdown input for a config-only invocation:
// input.file (relative paths are anchored at the config file's directory),
// or implicitly the config's own basename with a .md extension.
func inputFromConfig(configPath string, cfg *Config) (string, error) {
	dir := filepath.Dir(configPath)
	if f := cfg.Input.File; f != "" {
		if !filepath.IsAbs(f) {
			f = filepath.Join(dir, f)
		}
		return f, nil
	}
	implied := filepath.Join(dir, strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))+".md")
	if _, err := os.Stat(implied); err != nil {
		return "", fmt.Errorf("no input: %s sets no input.file and %s does not exist", configPath, implied)
	}
	return implied, nil
}

// resolveInputPath tries the path as given first; if that file is missing and
// defaultDir is set, it returns defaultDir/path. The caller will surface the
// read error for the resolved path if still missing.
func resolveInputPath(path, defaultDir string) string {
	if defaultDir == "" {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(defaultDir, path)
}

// extractFrontmatter splits YAML frontmatter (between --- delimiters) from body.
// The frontmatter uses flat dotted keys: "document.title", "author.name", etc.
// which are parsed directly as a map[string]string by the YAML library.
func extractFrontmatter(content string) (body string, fm map[string]string) {
	fm = make(map[string]string)

	scanner := bufio.NewScanner(strings.NewReader(content))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return content, fm
	}

	var fmLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		fmLines = append(fmLines, line)
	}

	var bodyLines []string
	for scanner.Scan() {
		bodyLines = append(bodyLines, scanner.Text())
	}

	// The dotted keys ("document.title" etc.) are valid YAML string keys,
	// so a standard unmarshal into map[string]string gives us exactly what we need.
	_ = yaml.Unmarshal([]byte(strings.Join(fmLines, "\n")), &fm)

	return strings.Join(bodyLines, "\n"), fm
}

// applyFrontmatter overrides config fields using the frontmatter dotted key map.
func applyFrontmatter(fm map[string]string, cfg *Config) {
	override := func(key string, target *string) {
		if v, ok := fm[key]; ok && v != "" {
			*target = v
		}
	}
	override("document.title", &cfg.Document.Title)
	override("document.subtitle", &cfg.Document.Subtitle)
	override("document.version", &cfg.Document.Version)
	override("document.date", &cfg.Document.Date)
	override("document.documentID", &cfg.Document.DocumentID)
	override("document.clientName", &cfg.Document.ClientName)
	override("document.projectName", &cfg.Document.ProjectName)
	override("document.documentType", &cfg.Document.DocumentType)
	override("document.description", &cfg.Document.Description)
	override("author.name", &cfg.Author.Name)
	override("author.title", &cfg.Author.Title)
	override("author.organization", &cfg.Author.Organization)
	override("author.email", &cfg.Author.Email)
	override("author.phone", &cfg.Author.Phone)
	override("author.address", &cfg.Author.Address)
	override("author.department", &cfg.Author.Department)

	// watermark.text both sets the text and enables the watermark, so a
	// document can declare itself DRAFT even when the config leaves the
	// watermark off. An empty value is ignored like any other override.
	if v, ok := fm["watermark.text"]; ok && v != "" {
		cfg.Watermark.Text = v
		cfg.Watermark.Enabled = true
	}
}

// buildInput constructs md2pdf.Input from the resolved config.
func buildInput(body, inputPath, css string, cfg *Config) md2pdf.Input {
	input := md2pdf.Input{
		Markdown:  body,
		SourceDir: filepath.Dir(inputPath),
		CSS:       css,
	}

	if cfg.Cover.Enabled {
		cover := &md2pdf.Cover{
			Title:        cfg.Document.Title,
			Subtitle:     cfg.Document.Subtitle,
			Version:      cfg.Document.Version,
			Date:         cfg.Document.Date,
			DocumentID:   cfg.Document.DocumentID,
			Author:       cfg.Author.Name,
			AuthorTitle:  cfg.Author.Title,
			Organization: cfg.Author.Organization,
			Logo:         cfg.Cover.Logo,
			ClientName:   cfg.Document.ClientName,
			ProjectName:  cfg.Document.ProjectName,
			DocumentType: cfg.Document.DocumentType,
			Description:  cfg.Document.Description,
		}
		if cfg.Cover.ShowDepartment {
			cover.Department = cfg.Author.Department
		}
		input.Cover = cover
	}

	if cfg.TOC.Enabled {
		maxDepth := cfg.TOC.MaxDepth
		if maxDepth == 0 {
			maxDepth = md2pdf.DefaultTOCMaxDepth
		}
		input.TOC = &md2pdf.TOC{
			Title:            cfg.TOC.Title,
			MinDepth:         cfg.TOC.MinDepth,
			MaxDepth:         maxDepth,
			DisableNumbering: cfg.TOC.Numbered != nil && !*cfg.TOC.Numbered,
		}
	}

	if cfg.Footer.Enabled {
		var docID string
		if cfg.Footer.ShowDocumentID {
			docID = cfg.Document.DocumentID
		}
		input.Footer = &md2pdf.Footer{
			Position:       cfg.Footer.Position,
			ShowPageNumber: cfg.Footer.ShowPageNumber,
			Date:           cfg.Document.Date,
			Status:         cfg.Document.Version,
			Text:           cfg.Footer.Text,
			DocumentID:     docID,
		}
	}

	if cfg.Signature.Enabled {
		sig := &md2pdf.Signature{
			Name:         cfg.Author.Name,
			Title:        cfg.Author.Title,
			Email:        cfg.Author.Email,
			Organization: cfg.Author.Organization,
			ImagePath:    cfg.Signature.ImagePath,
			Phone:        cfg.Author.Phone,
			Address:      cfg.Author.Address,
			Department:   cfg.Author.Department,
		}
		for _, l := range cfg.Signature.Links {
			sig.Links = append(sig.Links, md2pdf.Link{Label: l.Label, URL: l.URL})
		}
		input.Signature = sig
	}

	if cfg.Watermark.Enabled {
		opacity := cfg.Watermark.Opacity
		if opacity == 0 {
			opacity = md2pdf.DefaultWatermarkOpacity
		}
		angle := cfg.Watermark.Angle
		if angle == 0 {
			angle = md2pdf.DefaultWatermarkAngle
		}
		input.Watermark = &md2pdf.Watermark{
			Text:    cfg.Watermark.Text,
			Color:   cfg.Watermark.Color,
			Opacity: opacity,
			Angle:   angle,
		}
	}

	if cfg.PageBreaks.Enabled {
		input.PageBreaks = &md2pdf.PageBreaks{
			BeforeH1: cfg.PageBreaks.BeforeH1,
			BeforeH2: cfg.PageBreaks.BeforeH2,
			BeforeH3: cfg.PageBreaks.BeforeH3,
			Duplex:   cfg.PageBreaks.Duplex,
			Orphans:  orDefaultInt(cfg.PageBreaks.Orphans, md2pdf.DefaultOrphans),
			Widows:   orDefaultInt(cfg.PageBreaks.Widows, md2pdf.DefaultWidows),
		}
	}

	if cfg.Page.Size != "" || cfg.Page.Orientation != "" || cfg.Page.Margin != 0 {
		orientation := cfg.Page.Orientation
		if orientation == "" {
			orientation = md2pdf.OrientationPortrait
		}
		margin := cfg.Page.Margin
		if margin == 0 {
			margin = md2pdf.DefaultMargin
		}
		input.Page = &md2pdf.PageSettings{
			Size:        cfg.Page.Size,
			Orientation: orientation,
			Margin:      margin,
		}
	}

	return input
}

func orDefaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
