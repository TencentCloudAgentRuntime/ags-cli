// Package precacheimagecleanuppolicy owns image pre-cache cleanup policy
// validation and presentation.
package precacheimagecleanuppolicy

import (
	"context"
	"fmt"
	"io"
	"regexp"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/textview"
)

const resource = "pre-cache-image-cleanup-policy"

var idleTTLPattern = regexp.MustCompile(`^[1-9][0-9]*[mhd]$`)

// NewModule adds idle-TTL and update-patch validation plus text output to a
// generated API module.
func NewModule(api apicli.APIDescriptor) command.Module {
	module := apicli.NewModule(api)
	generated := module.Descriptor
	module.Descriptor.Generated = &generated
	module.Descriptor.Source = command.SourceMixedAPI
	module.Build = func(deps command.Deps) (command.Runtime, error) {
		deps = deps.WithDefaults()
		builder := apicli.NewRequestBuilder(api)
		executor := apicli.NewExecutor(api, deps.ControlPlane)
		return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
			request, err := builder.Build(req)
			if err != nil {
				return nil, err
			}
			switch api.Spec.ID {
			case "pre-cache-image-cleanup-policy.create", "pre-cache-image-cleanup-policy.update":
				if err := validateIdleTTL(request["IdleTTL"]); err != nil {
					return nil, err
				}
			}
			if api.Spec.ID == "pre-cache-image-cleanup-policy.update" && !hasUpdateField(request) {
				return nil, output.NewUsageError(
					"MISSING_REQUIRED_FLAG",
					"at least one of --name, --registry-host, --repo-pattern, --idle-ttl, or --enabled must be specified",
					"Provide at least one field to update, or include it in --request.",
				)
			}
			result, err := executor.Execute(ctx, request)
			if err != nil {
				return nil, err
			}
			response, err := apivalue.Decode(result.Data)
			if err != nil {
				return nil, err
			}
			switch api.Spec.ID {
			case "pre-cache-image-cleanup-policy.create", "pre-cache-image-cleanup-policy.update":
				policy := response.Object("CleanupPolicy")
				if policy == nil || policy.String("CleanupPolicyId") == "" {
					return nil, output.NewCLIError(&output.Failure{
						Code:    "INTERNAL_ERROR",
						Kind:    output.KindGenericError,
						Message: "cleanup policy response is missing CleanupPolicyId",
						Hint:    "Rerun with --debug. If the issue persists, inspect the control-plane response.",
					})
				}
				kind := "create"
				if api.Spec.ID == "pre-cache-image-cleanup-policy.update" {
					kind = "update"
				}
				result.Effects = append(result.Effects, output.Effect{Kind: kind, Resource: resource, Id: policy.String("CleanupPolicyId")})
				result.Text = func(w io.Writer) { renderPolicy(w, policy) }
			case "pre-cache-image-cleanup-policy.list":
				policies, err := response.ReadObjects("CleanupPolicySet")
				if err != nil {
					return nil, err
				}
				result.Text = func(w io.Writer) { renderPolicyList(w, policies, response) }
			case "pre-cache-image-cleanup-policy.delete":
				if response.String("RequestId") == "" {
					return nil, output.NewCLIError(&output.Failure{
						Code:    "INTERNAL_ERROR",
						Kind:    output.KindGenericError,
						Message: "cleanup policy delete response is missing RequestId",
						Hint:    "Rerun with --debug. If the issue persists, inspect the control-plane response.",
					})
				}
				id, _ := request["CleanupPolicyId"].(string)
				result.Effects = append(result.Effects, output.Effect{Kind: "delete", Resource: resource, Id: id})
				result.Text = func(w io.Writer) { fmt.Fprintf(w, "Pre-cache image cleanup policy deleted: %s\n", id) }
			}
			return result, nil
		})}, nil
	}
	return module
}

func validateIdleTTL(value any) error {
	if value == nil {
		return nil
	}
	ttl, ok := value.(string)
	if ok && idleTTLPattern.MatchString(ttl) {
		return nil
	}
	return output.NewUsageError(
		"INVALID_IDLE_TTL",
		fmt.Sprintf("IdleTTL %v must be a positive integer followed by one unit: m, h, or d", value),
		"Use a value such as --idle-ttl 30m, --idle-ttl 12h, or --idle-ttl 3d.",
	)
}

func hasUpdateField(request map[string]any) bool {
	for _, name := range []string{"Name", "RegistryHost", "RepoPattern", "IdleTTL", "Enabled"} {
		if value, ok := request[name]; ok && value != nil {
			return true
		}
	}
	return false
}

func renderPolicy(w io.Writer, policy apivalue.Object) {
	fields := []string{"CleanupPolicyId", "Name", "Enabled", "IdleTTL", "RegistryHost", "RepoPattern"}
	pairs := make([]textview.KeyValue, 0, len(fields))
	for _, field := range fields {
		if _, present := policy[field]; present {
			pairs = append(pairs, textview.KeyValue{Key: field, Value: policy.Display(field)})
		}
	}
	textview.PrintKV(w, pairs)
}

func renderPolicyList(w io.Writer, policies []apivalue.Object, response apivalue.Object) {
	if len(policies) == 0 {
		fmt.Fprintln(w, "No pre-cache image cleanup policies found")
	} else {
		fields := []string{"CleanupPolicyId", "Name", "Enabled", "IdleTTL"}
		headers := []string{"ID", "NAME", "ENABLED", "IDLE TTL"}
		rows := make([][]string, 0, len(policies))
		for _, policy := range policies {
			row := make([]string, 0, len(fields))
			for _, field := range fields {
				row = append(row, policy.Display(field))
			}
			rows = append(rows, row)
		}
		textview.PrintTable(w, headers, rows)
	}
	if total := response.Display("TotalCount"); total != "" {
		fmt.Fprintf(w, "Total: %s\n", total)
	}
}
