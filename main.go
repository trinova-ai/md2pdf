package main

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	md2pdf "github.com/alnah/picoloom/v2"
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
		Usage:   "Convert a Markdown file to PDF",
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
				Usage:   "output PDF `FILE` (default: input with .pdf extension)",
			},
		},
		ArgsUsage: "<input.md>",
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
	if cmd.NArg() == 0 {
		return cli.ShowAppHelp(cmd)
	}
	inputPath := cmd.Args().First()

	// Load base config from YAML file
	cfg := &Config{}
	if configPath := cmd.String("config"); configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	// Resolve input path: if not found as given and input.defaultDir is set,
	// try joining with defaultDir before giving up.
	inputPath = resolveInputPath(inputPath, cfg.Input.DefaultDir)

	// Read markdown file
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}

	// Extract frontmatter and body; frontmatter overrides config
	body, fm := extractFrontmatter(string(data))
	applyFrontmatter(fm, cfg)

	// Resolve "auto" date
	if strings.EqualFold(cfg.Document.Date, "auto") {
		cfg.Document.Date = time.Now().Format("2 January 2006")
	}

	// Resolve output path
	outPath := cmd.String("output")
	if outPath == "" {
		base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)) + ".pdf"
		if cfg.Output.DefaultDir != "" {
			outPath = filepath.Join(cfg.Output.DefaultDir, base)
		} else {
			outPath = filepath.Join(filepath.Dir(inputPath), base)
		}
	}
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
	}

	// Resolve CSS from style name via the asset loader (respects assets.basePath)
	var css string
	if cfg.Style != "" {
		loader, loaderErr := md2pdf.NewAssetLoader(cfg.Assets.BasePath)
		if loaderErr != nil {
			return fmt.Errorf("initializing asset loader: %w", loaderErr)
		}
		css, err = loader.LoadStyle(cfg.Style)
		if err != nil {
			return fmt.Errorf("loading style %q: %w", cfg.Style, err)
		}
	}

	// Build converter options from config
	var opts []md2pdf.Option
	if cfg.Assets.BasePath != "" {
		opts = append(opts, md2pdf.WithAssetPath(cfg.Assets.BasePath))
	}
	if cfg.Timeout != "" {
		d, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return fmt.Errorf("parsing timeout %q: %w", cfg.Timeout, err)
		}
		if d <= 0 {
			return fmt.Errorf("timeout must be positive: %q", cfg.Timeout)
		}
		opts = append(opts, md2pdf.WithTimeout(d))
	}

	conv, err := md2pdf.NewConverter(opts...)
	if err != nil {
		return fmt.Errorf("initializing converter: %w", err)
	}
	defer conv.Close()

	result, err := conv.Convert(ctx, buildInput(body, inputPath, css, cfg))
	if err != nil {
		return fmt.Errorf("converting: %w", err)
	}

	if err := os.WriteFile(outPath, result.PDF, 0o644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	fmt.Printf("Created %s\n", outPath)
	return nil
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
			Title:    cfg.TOC.Title,
			MinDepth: cfg.TOC.MinDepth,
			MaxDepth: maxDepth,
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
