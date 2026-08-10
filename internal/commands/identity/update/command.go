package update

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/identity"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Cloud API Action: UpdateWorkloadIdentity
func Module() command.Module {
	spec := command.Spec{
		ID:    "identity.update",
		Path:  []string{"identity", "update"},
		Use:   "update <workload-identity-id>",
		Short: "Update a workload identity",
		Examples: []string{
			"agr identity update wi-xxxx --name new-name",
			"agr identity update wi-xxxx --allowed-oauth2-return-urls https://new.example.com/cb",
		},
		Args: []command.ArgSpec{{Name: "workload-identity-id", Description: "Workload Identity ID", Required: true}},
		Flags: []command.FlagSpec{
			{Name: "name", Shorthand: "n", Usage: "New name (max 128 chars)", Type: command.FlagString},
			{Name: "allowed-oauth2-return-urls", Usage: "Allowed OAuth2 return URLs (replaces all)", Type: command.FlagStringArray},
			{Name: "tags", Usage: "Tags in key=value format (replaces all)", Type: command.FlagStringArray},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{Effects: []string{"update:identity"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec:   spec,
			Groups: []command.GroupSpec{{Path: []string{"identity"}, Use: "identity", Short: "Manage workload identities"}},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp := deps.ControlPlane.(identity.ControlPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				wid := req.Args[0]
				params := map[string]any{"WorkloadIdentityId": wid}
				hasUpdate := false
				if name := req.Flags["name"]; name.Changed {
					params["Name"] = name.String
					hasUpdate = true
				}
				if urls := req.Flags["allowed-oauth2-return-urls"]; urls.Changed {
					params["AllowedOAuth2ReturnUrls"] = urls.Strings
					hasUpdate = true
				}
				if tags := req.Flags["tags"]; tags.Changed {
					params["Tags"] = parseTags(tags.Strings)
					hasUpdate = true
				}
				if !hasUpdate {
					return nil, output.NewUsageError("NO_UPDATE_FIELDS", "at least one field must be specified", "Provide --name, --allowed-oauth2-return-urls, or --tags.")
				}
				_, err := cp.Call(ctx, "UpdateWorkloadIdentity", params)
				if err != nil {
					return nil, err
				}
				return &command.Result{
					Data:    map[string]any{"WorkloadIdentityId": wid, "Updated": true},
					Effects: []output.Effect{{Kind: "update", Resource: "identity", Id: wid}},
					Text:    func(w io.Writer) { fmt.Fprintf(w, "Identity updated: %s\n", wid) },
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
