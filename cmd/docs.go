package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var docsOutputDir string

var docsCmd = &cobra.Command{
	Use:    "docs",
	Short:  "Generate documentation",
	Long:   `Generate documentation in various formats (man, markdown, yaml).`,
	Hidden: true, // Hidden from normal help, used for development
}

var docsManCmd = &cobra.Command{
	Use:   "man",
	Short: "Generate man pages",
	Long: `Generate man pages for all commands.

The generated man pages can be installed to /usr/local/share/man/man1/
for use with the 'man' command.

Examples:
  # Generate man pages to ./man directory
  ags docs man

  # Generate to custom directory
  ags docs man -o /tmp/ags-man

  # Install man pages (after generation)
  sudo cp man/*.1 /usr/local/share/man/man1/
  sudo mandb  # Linux only, updates man database`,
	RunE: runDocsMan,
}

var docsMarkdownCmd = &cobra.Command{
	Use:   "markdown",
	Short: "Generate markdown documentation",
	Long: `Generate markdown documentation for all commands.

Examples:
  # Generate markdown docs to ./docs/cmd directory
  ags docs markdown

  # Generate to custom directory
  ags docs markdown -o ./my-docs`,
	Aliases: []string{"md"},
	RunE:    runDocsMarkdown,
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.AddCommand(docsManCmd)
	docsCmd.AddCommand(docsMarkdownCmd)

	docsManCmd.Flags().StringVarP(&docsOutputDir, "output", "o", "man", "Output directory for man pages")
	docsMarkdownCmd.Flags().StringVarP(&docsOutputDir, "output", "o", "docs/cmd", "Output directory for markdown docs")
}

func runDocsMan(cmd *cobra.Command, args []string) error {
	// Create output directory
	if err := os.MkdirAll(docsOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate man pages
	header := &doc.GenManHeader{
		Title:   "AGS",
		Section: "1",
		Source:  "AGS CLI",
		Manual:  "AGS Manual",
	}

	// Temporarily hide version command from docs generation
	if versionCmd != nil {
		versionCmd.Hidden = true
		defer func() { versionCmd.Hidden = false }()
	}

	if err := doc.GenManTree(rootCmd, header, docsOutputDir); err != nil {
		return fmt.Errorf("failed to generate man pages: %w", err)
	}

	// Count generated files
	files, _ := filepath.Glob(filepath.Join(docsOutputDir, "*.1"))
	fmt.Printf("Generated %d man pages in %s/\n", len(files), docsOutputDir)
	fmt.Println("\nTo install:")
	fmt.Println("  sudo cp " + docsOutputDir + "/*.1 /usr/local/share/man/man1/")
	fmt.Println("  sudo mandb  # Linux only")
	fmt.Println("\nThen use: man ags, man ags-tool, man ags-instance, etc.")

	return nil
}

func runDocsMarkdown(cmd *cobra.Command, args []string) error {
	// Create output directory
	if err := os.MkdirAll(docsOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Temporarily hide version command from docs generation
	if versionCmd != nil {
		versionCmd.Hidden = true
		defer func() { versionCmd.Hidden = false }()
	}

	// Generate markdown docs
	if err := doc.GenMarkdownTree(rootCmd, docsOutputDir); err != nil {
		return fmt.Errorf("failed to generate markdown docs: %w", err)
	}

	// Count generated files
	files, _ := filepath.Glob(filepath.Join(docsOutputDir, "*.md"))
	fmt.Printf("Generated %d markdown files in %s/\n", len(files), docsOutputDir)

	return nil
}
