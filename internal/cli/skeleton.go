package cli

import (
	"encoding/json"
	"fmt"
	"io"

	requestio "github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
)

var generateSkeleton bool

func argsExactUnlessSkeleton(n int) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if generateSkeleton {
			return nil
		}
		return cobra.ExactArgs(n)(cmd, args)
	}
}

func supportsSkeleton(commandName string) bool {
	// A mapped command supports `--generate-skeleton` only when its
	// request object actually has request fields. Commands without
	// fields (e.g. apikey.list with an empty DescribeAPIKeyListRequest)
	// have nothing meaningful to skeletonize.
	if supports, ok := requestio.SupportsGeneratedSkeleton(commandName); ok {
		return supports
	}
	for _, s := range getAllSchemas() {
		if s.Name == commandName {
			return s.SupportsRequest
		}
	}
	return false
}

// skeletonResult emits an empty request body for the given command. The
// preferred source is the API generator's per-command skeleton, which
// is always derived from the effective API + mapping.yaml. Hand-written schema
// is only consulted as a fallback for commands that have no mapping yet.
func skeletonResult(commandName string) (*CmdResult, error) {
	if tmpl, ok := requestio.GeneratedSkeleton(commandName); ok {
		return OK(tmpl, func(w io.Writer) {
			b, _ := json.MarshalIndent(tmpl, "", "  ")
			fmt.Fprintln(w, string(b))
		}), nil
	}
	var schema *RequestSchema
	for _, s := range getAllSchemas() {
		if s.Name == commandName {
			schema = s.RequestSchema
			break
		}
	}
	if schema == nil {
		return nil, output.NewUsageError("SKELETON_UNSUPPORTED", "--generate-skeleton is only supported for request-based commands", "Run: agr schema -o json")
	}
	tmpl := map[string]any{}
	for name, prop := range schema.Properties {
		switch prop.Type {
		case "array":
			tmpl[name] = []any{}
		case "object":
			tmpl[name] = nil
		case "integer":
			tmpl[name] = 0
		case "bool":
			tmpl[name] = false
		case "enum":
			if len(prop.Values) > 0 {
				tmpl[name] = prop.Values[0]
			} else {
				tmpl[name] = ""
			}
		default:
			tmpl[name] = ""
		}
	}
	return OK(tmpl, func(w io.Writer) {
		b, _ := json.MarshalIndent(tmpl, "", "  ")
		fmt.Fprintln(w, string(b))
	}), nil
}
