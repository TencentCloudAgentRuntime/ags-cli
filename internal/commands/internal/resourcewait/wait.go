// Package resourcewait provides polling for Instance and Tool lifecycle states.
package resourcewait

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

const (
	DefaultInterval = 5 * time.Second
	DefaultTimeout  = 10 * time.Minute

	// OptionsKey allows focused tests to replace the default timing through
	// command.Deps.Values without exposing timing flags to CLI users.
	OptionsKey = "resourcewait.options"
)

var (
	instanceTransitionalStatuses = statusSet("STARTING", "PAUSING", "STOPPING")
	instanceStableStatuses       = statusSet(
		"RUNNING", "PAUSED", "STOPPED", "FAILED", "STARTING_FAILED",
		"STOPPING_FAILED", "PAUSE_FAILED", "RESUME_FAILED",
	)
	toolTransitionalStatuses = statusSet("CREATING", "DELETING")
	toolStableStatuses       = statusSet("ACTIVE", "FAILED", "ISOLATED")
)

// Options controls one wait operation.
type Options struct {
	Interval             time.Duration
	Timeout              time.Duration
	DelayBeforeFirstPoll bool
}

// InstanceGetter is the control-plane capability needed by Instance waiters.
type InstanceGetter interface {
	GetInstance(context.Context, string) (*ags.SandboxInstance, error)
}

// ToolGetter is the control-plane capability needed by Tool waiters.
type ToolGetter interface {
	GetTool(context.Context, string) (*ags.SandboxTool, error)
}

// Flag returns the shared workflow flag used by supported commands.
func Flag() command.FlagSpec {
	return command.FlagSpec{
		Name:     "wait",
		Usage:    "Wait until the resource leaves a transitional state",
		Type:     command.FlagBool,
		Workflow: true,
	}
}

// Requested reports whether --wait was enabled for the request.
func Requested(req command.Request) bool {
	value, ok := req.Flags["wait"]
	return ok && value.Bool
}

// PreserveMutationMetadata carries side effects and warnings from the one-time
// mutation onto the final resource result returned after waiting.
func PreserveMutationMetadata(finalResult, mutationResult *command.Result) *command.Result {
	if finalResult == nil || mutationResult == nil {
		return finalResult
	}
	finalResult.Warnings = append([]string(nil), mutationResult.Warnings...)
	finalResult.Effects = append([]output.Effect(nil), mutationResult.Effects...)
	if mutationResult.MetaExtra != nil {
		finalResult.MetaExtra = make(map[string]any, len(mutationResult.MetaExtra))
		for key, value := range mutationResult.MetaExtra {
			finalResult.MetaExtra[key] = value
		}
	}
	return finalResult
}

// OptionsFromDeps returns production timing defaults, with a test-only
// dependency override when supplied by a command module test.
func OptionsFromDeps(deps command.Deps) Options {
	options := Options{Interval: DefaultInterval, Timeout: DefaultTimeout}
	if configured, ok := deps.Values[OptionsKey].(Options); ok {
		if configured.Interval > 0 {
			options.Interval = configured.Interval
		}
		if configured.Timeout > 0 {
			options.Timeout = configured.Timeout
		}
	}
	return options
}

// WaitForInstance polls GetInstance until the instance leaves a transitional
// state or the operation times out.
func WaitForInstance(
	ctx context.Context,
	instanceID string,
	get func(context.Context, string) (*ags.SandboxInstance, error),
	options Options,
) (*ags.SandboxInstance, error) {
	return waitFor(ctx, "instance", instanceID, get, func(instance *ags.SandboxInstance) string {
		if instance == nil || instance.Status == nil {
			return ""
		}
		return *instance.Status
	}, instanceTransitionalStatuses, instanceStableStatuses, options)
}

// WaitForTool polls GetTool until the tool leaves a transitional state or the
// operation times out.
func WaitForTool(
	ctx context.Context,
	toolID string,
	get func(context.Context, string) (*ags.SandboxTool, error),
	options Options,
) (*ags.SandboxTool, error) {
	return waitFor(ctx, "tool", toolID, get, func(tool *ags.SandboxTool) string {
		if tool == nil || tool.Status == nil {
			return ""
		}
		return *tool.Status
	}, toolTransitionalStatuses, toolStableStatuses, options)
}

func waitFor[T any](
	ctx context.Context,
	resourceType string,
	resourceID string,
	get func(context.Context, string) (T, error),
	statusOf func(T) string,
	transitionalStatuses map[string]struct{},
	stableStatuses map[string]struct{},
	options Options,
) (T, error) {
	var zero T
	if options.Interval <= 0 {
		options.Interval = DefaultInterval
	}
	if options.Timeout <= 0 {
		options.Timeout = DefaultTimeout
	}

	waitCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	lastStatus := ""
	if options.DelayBeforeFirstPoll {
		if err := waitForNextPoll(waitCtx, options.Interval); err != nil {
			return zero, waitContextError(ctx, resourceType, resourceID, lastStatus)
		}
	}
	for {
		resource, err := get(waitCtx, resourceID)
		if err != nil {
			if waitCtx.Err() != nil {
				return zero, waitContextError(ctx, resourceType, resourceID, lastStatus)
			}
			return zero, err
		}

		lastStatus = strings.TrimSpace(statusOf(resource))
		normalizedStatus := strings.ToUpper(lastStatus)
		if _, ok := stableStatuses[normalizedStatus]; ok {
			return resource, nil
		}
		if _, ok := transitionalStatuses[normalizedStatus]; !ok {
			return zero, unknownStatusError(resourceType, resourceID, lastStatus)
		}

		if err := waitForNextPoll(waitCtx, options.Interval); err != nil {
			return zero, waitContextError(ctx, resourceType, resourceID, lastStatus)
		}
	}
}

func statusSet(statuses ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		set[status] = struct{}{}
	}
	return set
}

func unknownStatusError(resourceType, resourceID, status string) error {
	return output.NewCLIError(&output.Failure{
		Code:    "WAIT_UNKNOWN_STATUS",
		Kind:    output.KindGenericError,
		Message: fmt.Sprintf("%s %s has unknown status %q", resourceType, resourceID, status),
		Hint:    "Update agr if the service introduced a new resource status.",
		Details: map[string]any{"ResourceType": resourceType, "ResourceId": resourceID, "LastStatus": status},
	})
}

func waitForNextPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitContextError(parent context.Context, resourceType, resourceID, lastStatus string) error {
	if err := parent.Err(); err != nil {
		return err
	}
	return output.NewCLIError(&output.Failure{
		Code:      "WAIT_TIMEOUT",
		Kind:      output.KindTimeout,
		Message:   fmt.Sprintf("timed out waiting for %s %s", resourceType, resourceID),
		Hint:      fmt.Sprintf("Run 'agr %s get %s --wait' to continue waiting.", resourceType, resourceID),
		Retryable: true,
		Details:   map[string]any{"ResourceType": resourceType, "ResourceId": resourceID, "LastStatus": lastStatus},
	})
}
