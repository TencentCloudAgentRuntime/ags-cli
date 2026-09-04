// Command apipatch validates, renders, and rebases the temporary RFC 6902
// patch layered over the canonical tccli api.json.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

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
		return fmt.Errorf("usage: apipatch <check|check-all|render|rebase> [flags]")
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout)
	case "check-all":
		return runCheckAll(args[1:], stdout)
	case "render":
		return runRender(args[1:], stdout)
	case "rebase":
		return runRebase(args[1:], stdout)
	default:
		return fmt.Errorf("unknown subcommand %q (allowed: check, check-all, render, rebase)", args[0])
	}
}

func runCheck(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apiDir := fs.String("api", defaultAPIDir, "directory containing api.json and api.patch.json")
	requireEmpty := fs.Bool("require-empty", false, "reject non-empty API patches")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("check does not accept positional arguments")
	}
	return checkAPIDir(*apiDir, *requireEmpty, stdout)
}

func runCheckAll(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("check-all", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	apiRoot := flags.String("root", filepath.Join("api", "ags"), "root directory containing API versions")
	requireEmpty := flags.Bool("require-empty", false, "reject non-empty API patches")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("check-all does not accept positional arguments")
	}

	apiDirs, err := findAPIDirs(*apiRoot)
	if err != nil {
		return err
	}
	for _, apiDir := range apiDirs {
		if err := checkAPIDir(apiDir, *requireEmpty, stdout); err != nil {
			return err
		}
	}
	return nil
}

func findAPIDirs(root string) ([]string, error) {
	var apiDirs []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "api.patch.json" {
			return nil
		}
		apiDirs = append(apiDirs, filepath.Dir(path))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan API patches under %s: %w", root, err)
	}
	if len(apiDirs) == 0 {
		return nil, fmt.Errorf("no api.patch.json files found under %s", root)
	}
	slices.Sort(apiDirs)
	return apiDirs, nil
}

func checkAPIDir(apiDir string, requireEmpty bool, stdout io.Writer) error {
	spec, err := apimeta.LoadEffectiveSpec(apiDir)
	if err != nil {
		return fmt.Errorf("check API directory %s: %w", apiDir, err)
	}
	operationCount, err := apiPatchOperationCount(apiDir)
	if err != nil {
		return fmt.Errorf("check API directory %s: %w", apiDir, err)
	}
	if requireEmpty && operationCount != 0 {
		return fmt.Errorf("API patch must be empty: %s contains %d operations", filepath.Join(apiDir, "api.patch.json"), operationCount)
	}
	operationLabel := "operations"
	if operationCount == 1 {
		operationLabel = "operation"
	}
	fmt.Fprintf(stdout, "API patch is valid: %s (%d %s, %d actions, %d objects)\n", filepath.ToSlash(apiDir), operationCount, operationLabel, len(spec.Actions), len(spec.Objects))
	return nil
}

func apiPatchOperationCount(apiDir string) (int, error) {
	path := filepath.Join(apiDir, "api.patch.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read API patch: %w", err)
	}
	var operations []json.RawMessage
	if err := json.Unmarshal(data, &operations); err != nil {
		return 0, fmt.Errorf("decode API patch %s: %w", path, err)
	}
	return len(operations), nil
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
