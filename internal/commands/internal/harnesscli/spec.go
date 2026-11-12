//go:build preview

package harnesscli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Workflow flags that feed Harness spec fields excluded from generated flags.
var specShortcuts = append([]string{"model", "default-model", "skill", "plugin", "session-space-id", "tags"}, MCPShortcuts...)

// CreateShortcuts adds the create-only initialization shortcuts.
var CreateShortcuts = append(slices.Clone(specShortcuts), "role-arn", "subnet-id", "security-group-id")

// UpdateShortcuts are the update shortcut flags.
var UpdateShortcuts = specShortcuts

// SpecFlags returns the shared Harness spec shortcut flags.
func SpecFlags(update bool) []command.FlagSpec {
	replace := ""
	if update {
		replace = " Replaces the current list."
	}
	clearHint := ""
	if update {
		clearHint = " Pass '' once to remove all."
	}
	flags := []command.FlagSpec{
		StringArrayFlag("model", "Selected model as <model-provider-id>:<model-id>; repeat for more models."+replace, "--model mp-a1b2c3d4:hy3"),
		StringFlag("default-model", "Default model as <model-provider-id>:<model-id>; must be one of the selected models. Optional when exactly one model is selected."),
		StringArrayFlag("skill", "Skill from Agent Registry as <registry-id>/<record-id>/<version-id>; repeat for more."+replace+clearHint, "--skill MARKETPLACE/rec-0123abcd/rv-0123abcd"),
		StringArrayFlag("plugin", "Plugin from Agent Registry as <registry-id>/<record-id>/<version-id>; repeat for more."+replace+clearHint),
		StringFlag("session-space-id", "Session space that persists native session history (pi and dsh only)."+map[bool]string{true: " Pass '' to remove it.", false: ""}[update]),
		StringArrayFlag("tags", "Custom tag as key=value; repeat for more."+replace+map[bool]string{true: " Pass '' once to clear custom tags.", false: ""}[update]),
	}
	return append(flags, MCPFlags(update)...)
}

// InitializationFlags are the create-only SandboxTool shortcuts.
func InitializationFlags() []command.FlagSpec {
	return []command.FlagSpec{
		StringFlag("role-arn", "Role ARN for the Sandbox Tool and for reading Registry and TokenHub sources; sets InitializeConfiguration.SandboxTool.RoleArn. Cannot be changed later. (required unless set in --initialize-configuration)"),
		StringArrayFlag("subnet-id", "VPC subnet for the Sandbox Tool; repeat for more. Managed harnesses run in VPC mode only. Cannot be changed later. (required unless set in --initialize-configuration)", "--subnet-id subnet-a1b2c3d4"),
		StringArrayFlag("security-group-id", "VPC security group for the Sandbox Tool; repeat for more. Cannot be changed later. (required unless set in --initialize-configuration)", "--security-group-id sg-a1b2c3d4"),
	}
}

// listOrClear treats a single empty value as an explicit empty list.
func listOrClear(values []string) []string {
	if len(values) == 1 && strings.TrimSpace(values[0]) == "" {
		return []string{}
	}
	return values
}

// ApplySpecShortcuts merges shortcut flags into a generated request.
func ApplySpecShortcuts(req command.Request, request map[string]any, update bool) error {
	if Changed(req, "system-prompt") {
		prompt, err := TextFlag(req, "system-prompt")
		if err != nil {
			return err
		}
		request["SystemPrompt"] = prompt
	}
	var refs []ModelRef
	if Changed(req, "model") {
		providers, parsed, err := ModelProviders(Strings(req, "model"))
		if err != nil {
			return err
		}
		if len(providers) == 0 {
			return output.NewUsageError("MISSING_MODEL", "--model needs at least one value", "Example: --model mp-a1b2c3d4:hy3")
		}
		request["ModelProviders"] = providers
		refs = parsed
	}
	defaultModel, ok, err := DefaultModel(req, refs)
	if err != nil {
		return err
	}
	if ok {
		request["DefaultModel"] = defaultModel
	}
	for flag, field := range map[string]string{"skill": "Skills", "plugin": "Plugins"} {
		if !Changed(req, flag) {
			continue
		}
		refs, err := RegistryRefs(flag, listOrClear(Strings(req, flag)))
		if err != nil {
			return err
		}
		request[field] = refs
	}
	if Changed(req, "session-space-id") {
		space := strings.TrimSpace(String(req, "session-space-id"))
		if space == "" && !update {
			return output.NewUsageError("INVALID_SESSION_SPACE", "--session-space-id cannot be empty on create", "Omit the flag to create without a session space.")
		}
		request["SessionConfiguration"] = map[string]any{"SessionSpaceId": space}
	}
	if Changed(req, "tags") {
		tags, err := Tags(Strings(req, "tags"))
		if err != nil {
			return err
		}
		request["Tags"] = tags
	}
	servers, ok, err := MCPServers(req)
	if err != nil {
		return err
	}
	if ok {
		if len(servers) == 0 && !update {
			return output.NewUsageError("INVALID_MCP", "--mcp '' cannot be used on create", "Omit --mcp to create without MCP servers.")
		}
		request["MCPServers"] = servers
	}
	return nil
}

// BuildCreateRequest assembles CreateHarness from shortcuts or --request.
func BuildCreateRequest(ctx context.Context, req command.Request, env Env) (map[string]any, error) {
	if err := SingleStdin(req, "mcp-servers", "initialize-configuration"); err != nil {
		return nil, err
	}
	if RequestMode(req) {
		if err := RejectWithRequest(req, CreateShortcuts...); err != nil {
			return nil, err
		}
		return env.Builder.Build(WithoutFlags(req, append(CreateShortcuts, "dry-run", "wait")...))
	}
	if !Changed(req, "engine-version") && Changed(req, "engine") {
		return nil, missingEngineVersion(ctx, env, String(req, "engine"))
	}
	buildReq := WithoutFlags(req, append(CreateShortcuts, "dry-run", "wait")...)
	if ChangedAny(req, "role-arn", "subnet-id", "security-group-id") != "" && !Changed(req, "initialize-configuration") {
		// The generated field is required; an empty object lets the shortcuts
		// alone satisfy it before they are merged below.
		flag := buildReq.Flags["initialize-configuration"]
		flag.Changed, flag.String = true, "{}"
		buildReq.Flags["initialize-configuration"] = flag
	}
	request, err := env.Builder.Build(buildReq)
	if err != nil {
		if ChangedAny(req, "role-arn", "subnet-id", "security-group-id") != "" || !strings.Contains(err.Error(), "initialize-configuration") {
			return nil, err
		}
		return nil, missingInitialization("--role-arn", "SandboxTool.RoleArn")
	}
	if err := ApplySpecShortcuts(req, request, false); err != nil {
		return nil, err
	}
	if _, ok := request["ModelProviders"]; !ok {
		return nil, output.NewUsageError("MISSING_REQUIRED_FLAG", "--model is required",
			"Provide --model <model-provider-id>:<model-id>. List providers with 'agr model-provider list'.")
	}
	if _, ok := request["DefaultModel"]; !ok {
		return nil, output.NewUsageError("MISSING_REQUIRED_FLAG", "--default-model is required when more than one --model is selected",
			"Provide --default-model <model-provider-id>:<model-id> naming one of the --model values.")
	}
	if err := mergeInitialization(req, request); err != nil {
		return nil, err
	}
	return request, nil
}

func missingInitialization(flag, field string) error {
	return output.NewUsageError("MISSING_REQUIRED_FLAG", flag+" is required",
		fmt.Sprintf("Provide %s, or set %s in --initialize-configuration.", flag, field))
}

// mergeInitialization applies the create-only shortcuts to
// InitializeConfiguration.SandboxTool and checks the fields the platform
// requires: managed harnesses run only in VPC mode, and Tool creation fails
// asynchronously without subnets and security groups.
func mergeInitialization(req command.Request, request map[string]any) error {
	init, _ := request["InitializeConfiguration"].(map[string]any)
	if init == nil {
		return output.NewUsageError("INVALID_JSON_FLAG", "--initialize-configuration must be a JSON object", `Example: {"SandboxTool":{"RoleArn":"<arn>"}}`)
	}
	tool := childObject(init, "SandboxTool")
	if Changed(req, "role-arn") {
		arn := strings.TrimSpace(String(req, "role-arn"))
		if existing, _ := tool["RoleArn"].(string); existing != "" && existing != arn {
			return output.NewUsageError("ROLE_ARN_CONFLICT", "--role-arn differs from SandboxTool.RoleArn in --initialize-configuration",
				"Set the role in one place.")
		}
		tool["RoleArn"] = arn
	}
	if arn, _ := tool["RoleArn"].(string); strings.TrimSpace(arn) == "" {
		return missingInitialization("--role-arn", "SandboxTool.RoleArn")
	}
	network := childObject(tool, "NetworkConfiguration")
	if mode, ok := network["NetworkMode"].(string); ok && mode != "VPC" {
		return output.NewUsageError("INVALID_NETWORK_MODE", fmt.Sprintf("NetworkMode %q is not supported", mode),
			"Managed harnesses run in VPC mode only; omit NetworkMode or set it to VPC.")
	}
	vpc := childObject(network, "VpcConfig")
	for _, item := range []struct{ flag, field string }{{"subnet-id", "SubnetIds"}, {"security-group-id", "SecurityGroupIds"}} {
		if Changed(req, item.flag) {
			values := nonEmpty(Strings(req, item.flag))
			if existing := StringList(vpc[item.field]); len(existing) > 0 && !slices.Equal(existing, values) {
				return output.NewUsageError("NETWORK_CONFLICT", fmt.Sprintf("--%s differs from VpcConfig.%s in --initialize-configuration", item.flag, item.field),
					"Set the VPC network in one place.")
			}
			vpc[item.field] = values
		}
		if len(nonEmpty(StringList(vpc[item.field]))) == 0 {
			return missingInitialization("--"+item.flag, "SandboxTool.NetworkConfiguration.VpcConfig."+item.field)
		}
	}
	network["NetworkMode"] = "VPC"
	return nil
}

func childObject(parent map[string]any, key string) map[string]any {
	child, _ := parent[key].(map[string]any)
	if child == nil {
		child = map[string]any{}
		parent[key] = child
	}
	return child
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func missingEngineVersion(ctx context.Context, env Env, engine string) error {
	err := output.NewUsageError("MISSING_REQUIRED_FLAG", "--engine-version is required",
		"List available versions with 'agr harness engine list'.")
	engines, listErr := env.Client.Engines(ctx)
	if listErr != nil {
		return err
	}
	for _, item := range engines {
		if item.String("HarnessEngine") != engine {
			continue
		}
		versions := StringList(item["HarnessEngineVersion"])
		if len(versions) > 0 {
			err.Failure.Hint = fmt.Sprintf("Available for %s: %s", engine, strings.Join(versions, ", "))
			err.Failure.Fix = fmt.Sprintf("Add --engine-version %s", versions[0])
			err.Failure.Details = map[string]any{"HarnessEngine": engine, "AvailableVersions": versions}
		}
		return err
	}
	err.Failure.Hint = fmt.Sprintf("Harness Engine %q is not offered in this region. List engines with 'agr harness engine list'.", engine)
	return err
}

// StringList converts a decoded JSON string array or a []string.
func StringList(value any) []string {
	if values, ok := value.([]string); ok {
		return slices.Clone(values)
	}
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

// BuildUpdateRequest assembles ModifyHarness from shortcuts or --request.
func BuildUpdateRequest(req command.Request, env Env) (map[string]any, error) {
	if err := SingleStdin(req, "mcp-servers"); err != nil {
		return nil, err
	}
	if RequestMode(req) {
		if err := RejectWithRequest(req, UpdateShortcuts...); err != nil {
			return nil, err
		}
		return env.Builder.Build(WithoutFlags(req, append(UpdateShortcuts, "dry-run")...))
	}
	request, err := env.Builder.Build(WithoutFlags(req, append(UpdateShortcuts, "dry-run")...))
	if err != nil {
		return nil, err
	}
	if err := ApplySpecShortcuts(req, request, true); err != nil {
		return nil, err
	}
	if len(request) <= 1 {
		return nil, output.NewUsageError("NOTHING_TO_UPDATE", "no changes were specified",
			"Pass at least one flag, for example: agr harness update <harness-id> --system-prompt @prompt.md")
	}
	return request, nil
}

// HarnessID returns the harness ID from a decoded Harness response.
func HarnessID(response apivalue.Object) string {
	return response.Object("Harness").String("HarnessId")
}
