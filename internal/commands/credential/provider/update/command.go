package update

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli/request"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/credential"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Module returns the "credential provider update" command module.
// Cloud API Action: UpdateCredentialProvider
func Module() command.Module {
	spec := command.Spec{
		ID:    "credential.provider.update",
		Path:  []string{"credential", "provider", "update"},
		Use:   "update <provider-id>",
		Short: "Update a credential provider",
		Long: `Update a credential provider's name, description, config, tags, or status.

Only OAuth2 and SecretMultiUser types allow status changes (ACTIVE/DISABLED).
A provider must be DISABLED before deletion.`,
		Examples: []string{
			"agr credential provider update agc-xxxx --description 'new description'",
			"agr credential provider update agc-xxxx --status DISABLED",
			"agr credential provider update agc-xxxx --name new-name --tags env=prod -o json",
		},
		Args: []command.ArgSpec{{Name: "provider-id", Description: "Provider ID to update", Required: true}},
		Flags: []command.FlagSpec{
			{Name: "name", Shorthand: "n", Usage: "New provider name (max 128 chars)", Type: command.FlagString},
			{Name: "description", Usage: "New description (max 256 chars)", Type: command.FlagString},
			{Name: "provider-config", Usage: "Updated config as JSON array of {Key,Value} or @file", Type: command.FlagString, Format: "json"},
			{Name: "tags", Usage: "Tags in key=value format (repeatable, replaces all tags)", Type: command.FlagStringArray},
			{Name: "status", Usage: "Provider status: ACTIVE or DISABLED (OAuth2/SecretMultiUser only)", Type: command.FlagString, Values: []string{"ACTIVE", "DISABLED"}},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{Effects: []string{"update:credential-provider"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{Path: []string{"credential"}, Use: "credential", Short: "Manage credentials and providers"},
				{Path: []string{"credential", "provider"}, Use: "provider", Short: "Manage credential providers"},
			},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(credential.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				pid := req.Args[0]
				params := map[string]any{
					"ProviderId": pid,
				}

				// At least one field must be provided.
				hasUpdate := false
				if name := req.Flags["name"]; name.Changed {
					params["Name"] = name.String
					hasUpdate = true
				}
				if desc := req.Flags["description"]; desc.Changed {
					params["Description"] = desc.String
					hasUpdate = true
				}
				if cfg := req.Flags["provider-config"]; cfg.Changed {
					var config []map[string]string
					if err := request.ParseJSONFlagValue("provider-config", cfg.String, &config); err != nil {
						return nil, err
					}
					params["Config"] = config
					hasUpdate = true
				}
				if tags := req.Flags["tags"]; tags.Changed {
					params["Tags"] = parseTags(tags.Strings)
					hasUpdate = true
				}
				if status := req.Flags["status"]; status.Changed {
					params["Status"] = status.String
					hasUpdate = true
				}

				if !hasUpdate {
					return nil, output.NewUsageError("NO_UPDATE_FIELDS", "at least one field must be specified to update", "Provide --name, --description, --provider-config, --tags, or --status.")
				}

				_, err := cp.Call(ctx, "UpdateCredentialProvider", params)
				if err != nil {
					return nil, err
				}

				return &command.Result{
					Data:    map[string]any{"ProviderId": pid, "Updated": true},
					Effects: []output.Effect{{Kind: "update", Resource: "credential-provider", Id: pid}},
					Text: func(w io.Writer) {
						fmt.Fprintf(w, "Provider updated: %s\n", pid)
					},
				}, nil
			})}, nil
		},
	}
}

func parseTags(tags []string) []map[string]string {
	var result []map[string]string
	for _, t := range tags {
		for i, c := range t {
			if c == '=' {
				result = append(result, map[string]string{"Key": t[:i], "Value": t[i+1:]})
				break
			}
		}
	}
	return result
}
