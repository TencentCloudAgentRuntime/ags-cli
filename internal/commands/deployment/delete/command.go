package delete

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

const (
	defaultWaitTimeout = 10 * time.Minute
	defaultWaitPoll    = time.Second
	waitIntervalKey    = "deployment.delete.waitInterval"
)

// ControlPlane supplies the generated delete call and status polling seam.
type ControlPlane interface {
	apicli.ControlPlane
	GetDeployment(context.Context, string) (*ags.Deployment, error)
	IsDeploymentNotFound(error) bool
}

// Module returns the generated delete operation with its asynchronous wait workflow.
func Module() command.Module {
	api := APIDescriptor()
	generatedSpec := api.CommandSpec()
	spec := generatedSpec
	spec.Flags = append(spec.Flags,
		command.FlagSpec{Name: "wait", Usage: "Wait until deletion completes or fails", Type: command.FlagBool, Default: true, Workflow: true},
		command.FlagSpec{Name: "timeout", Usage: "Maximum deletion wait (0 means no timeout)", Type: command.FlagString, Default: "10m", Workflow: true},
	)
	spec.Output = command.OutputSpec{
		DataType:    "DeleteDeploymentResponse",
		Description: "Deployment delete response.",
		Effects:     []string{"delete:deployment"},
	}

	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Generated: &command.Descriptor{
				Spec: generatedSpec, Groups: api.Groups, API: api, Source: command.SourceAPICli,
			},
			Groups: api.Groups, API: api, Source: command.SourceMixedAPI,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			cp, ok := deps.ControlPlane.(ControlPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("deployment.delete requires Deployment control-plane support")
			}
			builder := apicli.NewRequestBuilder(api)
			executor := apicli.NewExecutor(api, cp)
			interval := defaultWaitPoll
			if configured, ok := deps.Values[waitIntervalKey].(time.Duration); ok && configured > 0 {
				interval = configured
			}
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				wait := waitEnabled(req)
				var timeout time.Duration
				if wait {
					var err error
					timeout, err = parseTimeout(timeoutValue(req))
					if err != nil {
						return nil, output.NewUsageError("INVALID_TIMEOUT", err.Error(), "Use a Go duration such as 30s or 10m; use 0 to wait indefinitely.")
					}
				}
				apiRequest, err := builder.Build(req)
				if err != nil {
					return nil, err
				}
				deploymentID, _ := apiRequest["DeploymentId"].(string)
				if strings.TrimSpace(deploymentID) == "" {
					return nil, output.NewUsageError("MISSING_REQUIRED_ARG", "missing deployment id", "Provide <deployment-id>.")
				}
				result, err := executor.Execute(ctx, apiRequest)
				if err != nil {
					return nil, err
				}
				result.Effects = append(result.Effects, output.Effect{Kind: "delete", Resource: "deployment", Id: deploymentID})
				if !wait {
					result.Text = func(w io.Writer) { fmt.Fprintf(w, "Deployment deletion requested: %s\n", deploymentID) }
					return result, nil
				}
				if err := waitForDeletion(ctx, cp, deploymentID, interval, timeout); err != nil {
					return nil, err
				}
				result.Text = func(w io.Writer) { fmt.Fprintf(w, "Deployment deleted: %s\n", deploymentID) }
				return result, nil
			})}, nil
		},
	}
}

func waitForDeletion(ctx context.Context, cp ControlPlane, deploymentID string, interval, timeout time.Duration) error {
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	for attempts := 1; ; attempts++ {
		deployment, err := cp.GetDeployment(waitCtx, deploymentID)
		if err != nil {
			if cp.IsDeploymentNotFound(err) {
				return nil
			}
			if !isRetryable(err) {
				return err
			}
		} else if deployment != nil && strings.EqualFold(strings.TrimSpace(derefString(deployment.Status)), "DELETE_FAILED") {
			reason := strings.TrimSpace(derefString(deployment.StatusReason))
			if reason == "" {
				reason = "the control plane reported DELETE_FAILED"
			}
			return output.NewCLIError(&output.Failure{
				Code:    "DELETE_FAILED",
				Kind:    output.KindGenericError,
				Message: fmt.Sprintf("deployment deletion failed: %s", reason),
				Hint:    "Fix the reported cause and retry the delete command.",
				Details: map[string]any{"DeploymentId": deploymentID, "Status": "DELETE_FAILED", "StatusReason": reason, "Attempts": attempts},
			})
		}

		timer := time.NewTimer(interval)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return output.NewCLIError(&output.Failure{
				Code:      "WAIT_TIMEOUT",
				Kind:      output.KindTimeout,
				Message:   fmt.Sprintf("timed out waiting for deployment %s to be deleted", deploymentID),
				Hint:      "Rerun the delete command to continue waiting, or inspect the Deployment status.",
				Retryable: true,
				Details:   map[string]any{"DeploymentId": deploymentID, "Attempts": attempts},
			})
		case <-timer.C:
		}
	}
}

func waitEnabled(req command.Request) bool {
	value, ok := req.Flags["wait"]
	if !ok {
		return true
	}
	return value.Bool
}

func timeoutValue(req command.Request) string {
	if value, ok := req.Flags["timeout"]; ok && strings.TrimSpace(value.String) != "" {
		return value.String
	}
	return defaultWaitTimeout.String()
}

func parseTimeout(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("invalid --timeout %q: %w", value, err)
	}
	if duration < 0 {
		return 0, fmt.Errorf("--timeout must be non-negative")
	}
	return duration, nil
}

func isRetryable(err error) bool {
	classified := output.ClassifyError(err)
	return classified != nil && classified.Failure != nil && classified.Failure.Retryable
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
