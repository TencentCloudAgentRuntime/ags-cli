//go:build preview

// Package harnesscli holds request shortcuts, waiters and text views shared by
// the preview harness and model-provider commands.
package harnesscli

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Flag builders keep names, types and usage identical across commands.

func StringFlag(name, usage string) command.FlagSpec {
	return command.FlagSpec{Name: name, Usage: usage, Type: command.FlagString, Workflow: true}
}

func StringArrayFlag(name, usage string, examples ...string) command.FlagSpec {
	return command.FlagSpec{Name: name, Usage: usage, Type: command.FlagStringArray, Workflow: true, Examples: examples}
}

func BoolFlag(name, usage string) command.FlagSpec {
	return command.FlagSpec{Name: name, Usage: usage, Type: command.FlagBool, Workflow: true}
}

func EnumFlag(name, usage string, values ...string) command.FlagSpec {
	return command.FlagSpec{Name: name, Usage: usage, Type: command.FlagString, Workflow: true, Values: values}
}

// DryRunFlag previews the final request without sending it.
func DryRunFlag() command.FlagSpec {
	return BoolFlag("dry-run", "Print the final request without sending it")
}

// YesFlag skips the interactive confirmation of destructive commands.
func YesFlag() command.FlagSpec {
	flag := BoolFlag("yes", "Skip the confirmation prompt; required when stdin is not a terminal")
	flag.Shorthand = "y"
	return flag
}

// Changed reports whether the user explicitly set the flag.
func Changed(req command.Request, name string) bool {
	flag, ok := req.Flags[name]
	return ok && flag.Changed
}

func String(req command.Request, name string) string { return req.Flags[name].String }

func Strings(req command.Request, name string) []string {
	return append([]string(nil), req.Flags[name].Strings...)
}

func Bool(req command.Request, name string) bool {
	flag, ok := req.Flags[name]
	return ok && flag.Bool
}

// RequestMode reports whether the complete body comes from --request.
func RequestMode(req command.Request) bool {
	flag, ok := req.Flags["request"]
	return ok && flag.Changed && strings.TrimSpace(flag.String) != ""
}

// WithoutFlags returns a request copy that hides workflow flags from the
// generated request builder, which rejects unknown flags in --request mode.
func WithoutFlags(req command.Request, names ...string) command.Request {
	out := req
	out.Flags = maps.Clone(req.Flags)
	for _, name := range names {
		delete(out.Flags, name)
	}
	return out
}

// ChangedAny returns the first explicitly set flag among names.
func ChangedAny(req command.Request, names ...string) string {
	for _, name := range names {
		if Changed(req, name) {
			return name
		}
	}
	return ""
}

// RejectWithRequest fails when shortcut flags are mixed with --request, matching
// the generated builder's REQUEST_FLAG_CONFLICT contract.
func RejectWithRequest(req command.Request, shortcuts ...string) error {
	if !RequestMode(req) {
		return nil
	}
	if name := ChangedAny(req, shortcuts...); name != "" {
		return output.NewUsageError("REQUEST_FLAG_CONFLICT",
			fmt.Sprintf("--request cannot be combined with --%s", name),
			"Use --request for the complete request body, or use individual flags.")
	}
	return nil
}

// ModelRef is one provider/model selection parsed from <mp-id>:<model-id>.
type ModelRef struct {
	ProviderID string
	Model      string
}

// ParseModelRef splits on the first colon only: provider IDs never contain a
// colon, while upstream model IDs may contain ':' or '/'.
func ParseModelRef(flag, value string) (ModelRef, error) {
	provider, model, ok := strings.Cut(strings.TrimSpace(value), ":")
	provider, model = strings.TrimSpace(provider), strings.TrimSpace(model)
	if !ok || provider == "" || model == "" {
		return ModelRef{}, output.NewUsageError("INVALID_MODEL_REFERENCE",
			fmt.Sprintf("--%s %q must be <model-provider-id>:<model-id>", flag, value),
			fmt.Sprintf("Example: --%s mp-a1b2c3d4:hy3. List registered models with 'agr model-provider get <model-provider-id>'.", flag))
	}
	return ModelRef{ProviderID: provider, Model: model}, nil
}

// ModelProviders groups repeated --model values by provider, keeping the order
// in which providers and models first appear.
func ModelProviders(values []string) ([]map[string]any, []ModelRef, error) {
	var refs []ModelRef
	var order []string
	models := map[string][]string{}
	for _, value := range values {
		ref, err := ParseModelRef("model", value)
		if err != nil {
			return nil, nil, err
		}
		if _, seen := models[ref.ProviderID]; !seen {
			order = append(order, ref.ProviderID)
		}
		if slices.Contains(models[ref.ProviderID], ref.Model) {
			return nil, nil, output.NewUsageError("DUPLICATE_MODEL_REFERENCE",
				fmt.Sprintf("--model %s is repeated", value), "Pass each provider/model pair once.")
		}
		models[ref.ProviderID] = append(models[ref.ProviderID], ref.Model)
		refs = append(refs, ref)
	}
	out := make([]map[string]any, 0, len(order))
	for _, provider := range order {
		out = append(out, map[string]any{"ModelProviderId": provider, "Models": models[provider]})
	}
	return out, refs, nil
}

// DefaultModel resolves --default-model. Without the flag, a single selected
// model is the only unambiguous default; anything else must be explicit.
func DefaultModel(req command.Request, refs []ModelRef) (map[string]any, bool, error) {
	if Changed(req, "default-model") {
		ref, err := ParseModelRef("default-model", String(req, "default-model"))
		if err != nil {
			return nil, false, err
		}
		if len(refs) > 0 && !slices.Contains(refs, ref) {
			return nil, false, output.NewUsageError("DEFAULT_MODEL_NOT_SELECTED",
				fmt.Sprintf("--default-model %s is not one of the --model values", String(req, "default-model")),
				"Add it with --model, or choose one of the selected models.")
		}
		return map[string]any{"ModelProviderId": ref.ProviderID, "Model": ref.Model}, true, nil
	}
	if len(refs) == 1 {
		return map[string]any{"ModelProviderId": refs[0].ProviderID, "Model": refs[0].Model}, true, nil
	}
	return nil, false, nil
}

// RegistryRefs converts repeated <registry-id>/<record-id>/<version-id> values
// into Skill or Plugin references with a fixed Registry source.
func RegistryRefs(flag string, values []string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		parts := strings.Split(strings.TrimSpace(value), "/")
		if len(parts) != 3 || slices.Contains(parts, "") {
			return nil, output.NewUsageError("INVALID_REGISTRY_REFERENCE",
				fmt.Sprintf("--%s %q must be <registry-id>/<record-id>/<version-id>", flag, value),
				fmt.Sprintf("Example: --%s MARKETPLACE/rec-0123abcd/rv-0123abcd. Find versions with 'agr registry record version list'.", flag))
		}
		out = append(out, map[string]any{"Source": map[string]any{"Registry": map[string]any{
			"RegistryId": parts[0], "RecordId": parts[1], "VersionId": parts[2],
		}}})
	}
	return out, nil
}

// Tags converts repeated key=value flags; an empty list is an explicit [] that
// clears custom tags on update.
func Tags(values []string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		key, tagValue, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, output.NewUsageError("INVALID_TAG",
				fmt.Sprintf("--tags %q must be key=value", value), "Example: --tags env=prod --tags team=agents")
		}
		if seen[key] {
			return nil, output.NewUsageError("DUPLICATE_TAG", fmt.Sprintf("tag key %q is repeated", key), "Pass each tag key once.")
		}
		seen[key] = true
		out = append(out, map[string]any{"Key": key, "Value": tagValue})
	}
	return out, nil
}
