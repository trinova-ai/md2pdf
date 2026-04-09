package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	md2pdf "github.com/alnah/go-md2pdf"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "dev"

// Config mirrors the structure of work.yaml.
type Config struct {
	Document   DocumentConfig   `yaml:"document"`
	Author     AuthorConfig     `yaml:"author"`
	Page       PageConfig       `yaml:"page"`
	PageBreaks PageBreaksConfig `yaml:"pageBreaks"`
	Cover      CoverConfig      `yaml:"cover"`
	TOC        TOCConfig        `yaml:"toc"`
	Signature  SignatureConfig  `yaml:"signature"`
	Watermark  WatermarkConfig  `yaml:"watermark"`
	Footer     FooterConfig     `yaml:"footer"`
	Style      string           `yaml:"style"`
}

type DocumentConfig struct {
	Title      string `yaml:"title"`
	Subtitle   string `yaml:"subtitle"`
	Version    string `yaml:"version"`
	Date       string `yaml:"date"`
	DocumentID string `yaml:"documentID"`
}

type AuthorConfig struct {
	Name         string `yaml:"name"`
	Title        string `yaml:"title"`
	Organization string `yaml:"organization"`
	Email        string `yaml:"email"`
}

type PageConfig struct {
	Size string `yaml:"size"`
}

type PageBreaksConfig struct {
	Enabled  bool `yaml:"enabled"`
	BeforeH1 bool `yaml:"beforeH1"`
	BeforeH2 bool `yaml:"beforeH2"`
}

type CoverConfig struct {
	Enabled bool `yaml:"enabled"`
}

type TOCConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Title    string `yaml:"title"`
	MaxDepth int    `yaml:"maxDepth"`
}

type SignatureConfig struct {
	Enabled bool `yaml:"enabled"`
}

type WatermarkConfig struct {
	Enabled bool    `yaml:"enabled"`
	Text    string  `yaml:"text"`
	Opacity float64 `yaml:"opacity"`
	Angle   float64 `yaml:"angle"`
}

type FooterConfig struct {
	Enabled        bool `yaml:"enabled"`
	ShowPageNumber bool `yaml:"showPageNumber"`
	ShowDocumentID bool `yaml:"showDocumentID"`
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
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "md2pdf: %v\n", err)
		os.Exit(1)
	}
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
		outPath = strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + ".pdf"
	}

	// Resolve CSS from style name via the embedded asset loader
	var css string
	if cfg.Style != "" {
		loader, loaderErr := md2pdf.NewAssetLoader("")
		if loaderErr != nil {
			return fmt.Errorf("initializing asset loader: %w", loaderErr)
		}
		css, err = loader.LoadStyle(cfg.Style)
		if err != nil {
			return fmt.Errorf("loading style %q: %w", cfg.Style, err)
		}
	}

	// Convert
	conv, err := md2pdf.NewConverter()
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
	override("author.name", &cfg.Author.Name)
	override("author.title", &cfg.Author.Title)
	override("author.organization", &cfg.Author.Organization)
	override("author.email", &cfg.Author.Email)
}

// buildInput constructs md2pdf.Input from the resolved config.
func buildInput(body, inputPath, css string, cfg *Config) md2pdf.Input {
	input := md2pdf.Input{
		Markdown:  body,
		SourceDir: filepath.Dir(inputPath),
		CSS:       css,
	}

	if cfg.Cover.Enabled {
		input.Cover = &md2pdf.Cover{
			Title:        cfg.Document.Title,
			Subtitle:     cfg.Document.Subtitle,
			Version:      cfg.Document.Version,
			Date:         cfg.Document.Date,
			DocumentID:   cfg.Document.DocumentID,
			Author:       cfg.Author.Name,
			AuthorTitle:  cfg.Author.Title,
			Organization: cfg.Author.Organization,
		}
	}

	if cfg.TOC.Enabled {
		maxDepth := cfg.TOC.MaxDepth
		if maxDepth == 0 {
			maxDepth = md2pdf.DefaultTOCMaxDepth
		}
		input.TOC = &md2pdf.TOC{
			Title:    cfg.TOC.Title,
			MaxDepth: maxDepth,
		}
	}

	if cfg.Footer.Enabled {
		var docID string
		if cfg.Footer.ShowDocumentID {
			docID = cfg.Document.DocumentID
		}
		input.Footer = &md2pdf.Footer{
			ShowPageNumber: cfg.Footer.ShowPageNumber,
			Date:           cfg.Document.Date,
			Status:         cfg.Document.Version,
			DocumentID:     docID,
		}
	}

	if cfg.Signature.Enabled {
		input.Signature = &md2pdf.Signature{
			Name:         cfg.Author.Name,
			Title:        cfg.Author.Title,
			Email:        cfg.Author.Email,
			Organization: cfg.Author.Organization,
		}
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
			Opacity: opacity,
			Angle:   angle,
		}
	}

	if cfg.PageBreaks.Enabled {
		input.PageBreaks = &md2pdf.PageBreaks{
			BeforeH1: cfg.PageBreaks.BeforeH1,
			BeforeH2: cfg.PageBreaks.BeforeH2,
			Orphans:  md2pdf.DefaultOrphans,
			Widows:   md2pdf.DefaultWidows,
		}
	}

	if cfg.Page.Size != "" {
		input.Page = &md2pdf.PageSettings{
			Size:        cfg.Page.Size,
			Orientation: md2pdf.OrientationPortrait,
			Margin:      md2pdf.DefaultMargin,
		}
	}

	return input
}
