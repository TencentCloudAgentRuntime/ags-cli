// Command apipatch validates, renders, and rebases the temporary RFC 6902
// patch layered over the canonical tccli api.json.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

const defaultAPIDir = "api/ags/v20250920"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "apipatch:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: apipatch <check|render|rebase> [flags]")
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout)
	case "render":
		return runRender(args[1:], stdout)
	case "rebase":
		return runRebase(args[1:], stdout)
	default:
		return fmt.Errorf("unknown subcommand %q (allowed: check, render, rebase)", args[0])
	}
}

func runCheck(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apiDir := fs.String("api", defaultAPIDir, "directory containing api.json and api.patch.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("check does not accept positional arguments")
	}
	spec, err := apimeta.LoadEffectiveSpec(*apiDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "API patch is valid: %d actions, %d objects\n", len(spec.Actions), len(spec.Objects))
	return nil
}

func runRender(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apiDir := fs.String("api", defaultAPIDir, "directory containing api.json and api.patch.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("render does not accept positional arguments")
	}
	data, err := apimeta.LoadEffectiveJSON(*apiDir)
	if err != nil {
		return err
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, data, "", "  "); err != nil {
		return fmt.Errorf("format effective API: %w", err)
	}
	formatted.WriteByte('\n')
	_, err = stdout.Write(formatted.Bytes())
	return err
}

func runRebase(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rebase", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apiDir := fs.String("api", defaultAPIDir, "directory containing api.patch.json")
	upstreamPath := fs.String("upstream", "", "path to a refreshed upstream api.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("rebase does not accept positional arguments")
	}
	if *upstreamPath == "" {
		return fmt.Errorf("rebase requires --upstream")
	}
	upstream, err := os.ReadFile(*upstreamPath)
	if err != nil {
		return fmt.Errorf("read refreshed upstream API: %w", err)
	}
	patchData, err := os.ReadFile(filepath.Join(*apiDir, "api.patch.json"))
	if err != nil {
		return fmt.Errorf("read API patch: %w", err)
	}
	report, err := apimeta.EvaluateAPIPatch(upstream, patchData)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, report.Status)
	for _, operation := range report.Operations {
		fmt.Fprintf(stdout, "[%s] op=%d %s %s: %s\n", operation.Status, operation.Index, operation.Op, operation.Path, operation.Detail)
	}
	if report.Status != apimeta.PatchStatusActive {
		return fmt.Errorf("API patch requires maintenance: %s", report.Status)
	}
	return nil
}
