// Command apigen is the maintainer-facing entry point for API metadata
// reports. Cobra/runtime code generation lives in cmd/internal/cobragen.
//
//	go run ./cmd/internal/apigen coverage         -> textual coverage report
//	go run ./cmd/internal/apigen coverage --format json
//	go run ./cmd/internal/apigen list-actions     -> list effective API actions and their mapping status
//	go run ./cmd/internal/apigen schema [Object]  -> dump an api.json object schema
//
// The `coverage`, `list-actions`, and `schema` subcommands replace the
// removed `agr api coverage` / `agr api list-actions` / `agr api
// schema` user commands (NextPlan §9.4): they are maintainer tools,
// not part of the user-facing CLI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

const (
	apiService = "ags"
	apiVersion = "v20250920"
)

func main() {
	// Top-level flags (also accepted on the legacy form `apigen --check`).
	var (
		channel     = string(apimeta.Stable)
		legacyCheck bool
		apiDir      string
	)
	flag.StringVar(&channel, "channel", string(apimeta.Stable), "metadata channel: stable or preview")
	flag.BoolVar(&legacyCheck, "check", false, "deprecated: use `cobragen check` instead")
	flag.StringVar(&apiDir, "api", filepath.Join("api", apiService, apiVersion), "directory containing api.json, api.patch.json and mapping.yaml")
	flag.Parse()

	args := flag.Args()
	sub := "coverage"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	} else if legacyCheck {
		sub = "check"
	}

	var err error
	switch sub {
	case "generate":
		err = fmt.Errorf("generation moved to: go run ./cmd/internal/cobragen")
	case "check":
		err = fmt.Errorf("generation checks moved to: go run ./cmd/internal/cobragen check")
	case "coverage":
		err = runCoverage(apiDir, args, apimeta.Channel(channel))
	case "list-actions":
		err = runListActions(apiDir, args, apimeta.Channel(channel))
	case "schema":
		err = runSchema(apiDir, args, apimeta.Channel(channel))
	default:
		err = fmt.Errorf("unknown subcommand %q (allowed: coverage, list-actions, schema)", sub)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "apigen: %v\n", err)
		os.Exit(1)
	}
}

func metadataChannel(channels []apimeta.Channel) apimeta.Channel {
	if len(channels) > 0 {
		return channels[0]
	}
	return apimeta.Stable
}

func loadInputs(apiDir string, channels ...apimeta.Channel) (*apimeta.Spec, *apimeta.Mapping, error) {
	contract, err := apimeta.LoadContract(apiDir, metadataChannel(channels))
	if err != nil {
		return nil, nil, err
	}
	for _, issue := range contract.Mapping.Validate(contract.Spec) {
		if !issue.IsError() {
			fmt.Fprintf(os.Stderr, "WARN  %s\n", issue.String())
		}
	}
	return contract.Spec, contract.Mapping, nil
}

func runCoverage(apiDir string, args []string, channels ...apimeta.Channel) error {
	format := "text"
	asJSON := false
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	fs.StringVar(&format, "format", "text", "output format: text|json (NextPlan §9.3)")
	fs.BoolVar(&asJSON, "json", false, "deprecated: alias for --format json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if asJSON {
		format = "json"
	}
	spec, mapping, err := loadInputs(apiDir, channels...)
	if err != nil {
		return err
	}
	rep := apimeta.BuildCoverage(spec, mapping)
	rep.Channel = string(metadataChannel(channels))
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	fmt.Printf("Channel: %s\n", metadataChannel(channels))
	fmt.Printf("API version: %s\n", rep.APIVersion)
	fmt.Printf("Total actions: %d\n", rep.TotalActions)
	fmt.Printf("  mapped:   %d\n", rep.MappedActions)
	fmt.Printf("  raw_only: %d\n", rep.RawOnlyActions)
	fmt.Printf("  deferred: %d\n", rep.DeferredActions)
	fmt.Println()
	fmt.Println("ACTION                              STATUS   COMMAND")
	for _, a := range rep.Actions {
		fmt.Printf("%-35s %-8s %s\n", a.Action, a.Status, a.Command)
	}
	if len(rep.UnmappedActions) > 0 {
		fmt.Println()
		fmt.Println("Unmapped actions (CI fail):")
		for _, n := range rep.UnmappedActions {
			fmt.Printf("  - %s\n", n)
		}
	}
	if len(rep.StaleMappings) > 0 {
		fmt.Println()
		fmt.Println("Stale mappings (CI fail):")
		for _, n := range rep.StaleMappings {
			fmt.Printf("  - %s\n", n)
		}
	}
	return nil
}

func runListActions(apiDir string, args []string, channels ...apimeta.Channel) error {
	asJSON := false
	fs := flag.NewFlagSet("list-actions", flag.ContinueOnError)
	fs.BoolVar(&asJSON, "json", false, "emit JSON instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	spec, mapping, err := loadInputs(apiDir, channels...)
	if err != nil {
		return err
	}
	type row struct {
		Action  string `json:"Action"`
		Status  string `json:"Status"`
		Command string `json:"Command,omitempty"`
		Reason  string `json:"Reason,omitempty"`
	}
	var rows []row
	for _, n := range spec.SortedActionNames() {
		a, ok := mapping.Action(n)
		if !ok {
			rows = append(rows, row{Action: n, Status: "unknown"})
			continue
		}
		rows = append(rows, row{Action: n, Status: a.Status, Command: a.Command, Reason: a.Reason})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Action < rows[j].Action })
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"Channel": metadataChannel(channels), "ApiVersion": mapping.APIVersion, "Actions": rows})
	}
	fmt.Printf("Channel: %s\n", metadataChannel(channels))
	fmt.Println("ACTION                              STATUS               COMMAND")
	for _, r := range rows {
		fmt.Printf("%-35s %-20s %s\n", r.Action, r.Status, r.Command)
	}
	return nil
}

func runSchema(apiDir string, args []string, channels ...apimeta.Channel) error {
	fmt.Fprintf(os.Stderr, "Channel: %s\n", metadataChannel(channels))
	spec, _, err := loadInputs(apiDir, channels...)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		for _, n := range spec.SortedObjectNames() {
			fmt.Println(n)
		}
		return nil
	}
	name := args[0]
	obj := spec.Object(name)
	if obj == nil {
		return fmt.Errorf("unknown object: %s", name)
	}
	fmt.Printf("Object: %s\n", obj.Name)
	fmt.Println("Members:")
	for _, m := range obj.Members {
		req := ""
		if m.Required {
			req = " (required)"
		}
		fmt.Printf("  - %s %s%s\n", m.Name, m.Type, req)
	}
	return nil
}
