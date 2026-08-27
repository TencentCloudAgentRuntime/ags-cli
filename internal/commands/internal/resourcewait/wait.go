// Package resourcewait provides polling for resource lifecycle states.
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

// Operation identifies the command whose outcome a waiter is evaluating.
type Operation string

const (
	OperationGet    Operation = "get"
	OperationCreate Operation = "create"
	OperationUpdate Operation = "update"
	OperationPause  Operation = "pause"
	OperationResume Operation = "resume"
	OperationDelete Operation = "delete"
	OperationFork   Operation = "fork"
)

// Decision describes how a policy interprets one observed status.
type Decision int

const (
	Continue Decision = iota
	Success
	Failed
	Preempted
)

// Observation captures the current status and whether operation progress has
// already been observed during this wait.
type Observation struct {
	Status             string
	InProgressObserved bool
}

// Policy interprets resource statuses for one operation.
type Policy struct {
	Operation         Operation
	Decide            func(Observation) Decision
	IsInProgress      func(string) bool
	NotFoundIsSuccess bool
	TimeoutHint       func(string) string
}

// Options controls one wait operation.
type Options struct {
	Interval   time.Duration
	Timeout    time.Duration
	IsNotFound func(error) bool
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
		Usage:    "Wait until the requested operation reaches a final outcome",
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
		options.IsNotFound = configured.IsNotFound
	}
	return options
}

// InstancePolicy returns the backend-aligned status policy for an Instance
// operation.
func InstancePolicy(operation Operation) Policy {
	policy := Policy{
		Operation: operation,
		Decide: func(observation Observation) Decision {
			return decideInstanceStatus(operation, normalizeStatus(observation.Status), observation.InProgressObserved)
		},
	}
	if operation == OperationPause {
		policy.IsInProgress = func(status string) bool {
			return normalizeStatus(status) == "PAUSING"
		}
	}
	return policy
}

// ToolPolicy returns the backend-aligned status policy for a Tool operation.
func ToolPolicy(operation Operation) Policy {
	return Policy{
		Operation: operation,
		Decide: func(observation Observation) Decision {
			return decideToolStatus(operation, normalizeStatus(observation.Status))
		},
		NotFoundIsSuccess: operation == OperationDelete,
	}
}

func deploymentDeletePolicy() Policy {
	return Policy{
		Operation: OperationDelete,
		Decide: func(observation Observation) Decision {
			if normalizeStatus(observation.Status) == "DELETE_FAILED" {
				return Failed
			}
			return Continue
		},
		NotFoundIsSuccess: true,
		TimeoutHint: func(deploymentID string) string {
			return fmt.Sprintf("Inspect the resource with 'agr deployment get %s'.", deploymentID)
		},
	}
}

// WaitForInstance waits for an Instance to reach any non-failure terminal
// state, which is the contract used by instance get --wait.
func WaitForInstance(
	ctx context.Context,
	instanceID string,
	get func(context.Context, string) (*ags.SandboxInstance, error),
	options Options,
) (*ags.SandboxInstance, error) {
	return WaitForInstanceWithPolicy(ctx, instanceID, get, InstancePolicy(OperationGet), options)
}

// WaitForInstanceWithPolicy waits for an Instance operation-specific outcome.
func WaitForInstanceWithPolicy(
	ctx context.Context,
	instanceID string,
	get func(context.Context, string) (*ags.SandboxInstance, error),
	policy Policy,
	options Options,
) (*ags.SandboxInstance, error) {
	return waitFor(ctx, "instance", instanceID, get, func(instance *ags.SandboxInstance) string {
		if instance == nil || instance.Status == nil {
			return ""
		}
		return *instance.Status
	}, nil, policy, options)
}

// WaitForTool waits for a Tool to reach any non-failure terminal state, which
// is the contract used by tool get --wait.
func WaitForTool(
	ctx context.Context,
	toolID string,
	get func(context.Context, string) (*ags.SandboxTool, error),
	options Options,
) (*ags.SandboxTool, error) {
	return WaitForToolWithPolicy(ctx, toolID, get, ToolPolicy(OperationGet), options)
}

// WaitForToolWithPolicy waits for a Tool operation-specific outcome.
func WaitForToolWithPolicy(
	ctx context.Context,
	toolID string,
	get func(context.Context, string) (*ags.SandboxTool, error),
	policy Policy,
	options Options,
) (*ags.SandboxTool, error) {
	return waitFor(ctx, "tool", toolID, get, func(tool *ags.SandboxTool) string {
		if tool == nil || tool.Status == nil {
			return ""
		}
		return *tool.Status
	}, nil, policy, options)
}

// WaitForDeploymentDeletion waits until a Deployment is absent or reports a
// terminal deletion failure.
func WaitForDeploymentDeletion(
	ctx context.Context,
	deploymentID string,
	get func(context.Context, string) (*ags.Deployment, error),
	options Options,
) (*ags.Deployment, error) {
	return waitFor(ctx, "deployment", deploymentID, get, func(deployment *ags.Deployment) string {
		if deployment == nil || deployment.Status == nil {
			return ""
		}
		return *deployment.Status
	}, func(deployment *ags.Deployment) string {
		if deployment == nil || deployment.StatusReason == nil {
			return ""
		}
		return *deployment.StatusReason
	}, deploymentDeletePolicy(), options)
}

func waitFor[T any](
	ctx context.Context,
	resourceType string,
	resourceID string,
	get func(context.Context, string) (T, error),
	statusOf func(T) string,
	statusReasonOf func(T) string,
	policy Policy,
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

	startedAt := time.Now()
	lastStatus := ""
	attempts := 0
	inProgressObserved := false
	for {
		attempts++
		resource, err := get(waitCtx, resourceID)
		if err != nil {
			if waitCtx.Err() != nil {
				return zero, waitContextError(ctx, resourceType, resourceID, policy, lastStatus, attempts, startedAt)
			}
			if policy.NotFoundIsSuccess && options.IsNotFound != nil && options.IsNotFound(err) {
				return zero, nil
			}
			return zero, annotateWaitError(err, resourceType, resourceID, policy.Operation, lastStatus, attempts, startedAt)
		}

		lastStatus = strings.TrimSpace(statusOf(resource))
		statusReason := ""
		if statusReasonOf != nil {
			statusReason = strings.TrimSpace(statusReasonOf(resource))
		}
		if policy.IsInProgress != nil && policy.IsInProgress(lastStatus) {
			inProgressObserved = true
		}
		switch policy.Decide(Observation{Status: lastStatus, InProgressObserved: inProgressObserved}) {
		case Success:
			return resource, nil
		case Failed:
			return zero, waitStatusError("WAIT_FAILED", output.KindGenericError, resourceType, resourceID, policy.Operation, lastStatus, statusReason, attempts, startedAt)
		case Preempted:
			return zero, waitStatusError("WAIT_PREEMPTED", output.KindConflict, resourceType, resourceID, policy.Operation, lastStatus, statusReason, attempts, startedAt)
		case Continue:
		}

		if err := waitForNextPoll(waitCtx, options.Interval); err != nil {
			return zero, waitContextError(ctx, resourceType, resourceID, policy, lastStatus, attempts, startedAt)
		}
	}
}

type annotatedWaitError struct {
	cause      error
	classified *output.CLIError
}

func (e *annotatedWaitError) Error() string { return e.cause.Error() }

func (e *annotatedWaitError) Unwrap() []error { return []error{e.classified, e.cause} }

func annotateWaitError(
	err error,
	resourceType string,
	resourceID string,
	operation Operation,
	lastStatus string,
	attempts int,
	startedAt time.Time,
) error {
	classified := output.ClassifyError(err)
	failure := *classified.Failure
	details := make(map[string]any, len(failure.Details)+6)
	for key, value := range failure.Details {
		details[key] = value
	}
	for key, value := range waitDetails(resourceType, resourceID, operation, lastStatus, attempts, startedAt) {
		if _, exists := details[key]; !exists {
			details[key] = value
		}
	}
	failure.Details = details

	return &annotatedWaitError{
		cause: err,
		classified: &output.CLIError{
			Failure:  &failure,
			ExitCode: classified.ExitCode,
		},
	}
}

func decideInstanceStatus(operation Operation, status string, inProgressObserved bool) Decision {
	switch operation {
	case OperationCreate:
		switch status {
		case "RUNNING":
			return Success
		case "FAILED", "STARTING_FAILED", "STOPPING_FAILED", "STOP_FAILED", "PAUSE_FAILED", "RESUME_FAILED":
			return Failed
		case "PAUSED", "STOPPED":
			return Preempted
		default:
			return Continue
		}
	case OperationUpdate:
		switch status {
		case "RUNNING", "PAUSED", "STOPPED":
			return Success
		case "FAILED", "STARTING_FAILED", "STOPPING_FAILED", "STOP_FAILED", "PAUSE_FAILED", "RESUME_FAILED":
			return Failed
		default:
			return Continue
		}
	case OperationPause:
		switch status {
		case "PAUSED":
			return Success
		case "RUNNING":
			if inProgressObserved {
				return Failed
			}
			return Continue
		case "FAILED", "STARTING_FAILED", "PAUSE_FAILED", "RESUME_FAILED":
			return Failed
		case "STOPPED", "STOPPING_FAILED", "STOP_FAILED":
			return Preempted
		default:
			return Continue
		}
	case OperationResume:
		switch status {
		case "RUNNING":
			return Success
		case "FAILED", "STARTING_FAILED", "PAUSE_FAILED", "RESUME_FAILED":
			return Failed
		case "STOPPED", "STOPPING_FAILED", "STOP_FAILED":
			return Preempted
		default:
			return Continue
		}
	case OperationDelete:
		switch status {
		case "STOPPED":
			return Success
		case "STOPPING_FAILED", "STOP_FAILED":
			return Failed
		default:
			return Continue
		}
	default:
		switch status {
		case "RUNNING", "PAUSED", "STOPPED":
			return Success
		case "FAILED", "STARTING_FAILED", "STOPPING_FAILED", "STOP_FAILED", "PAUSE_FAILED", "RESUME_FAILED":
			return Failed
		default:
			return Continue
		}
	}
}

func decideToolStatus(operation Operation, status string) Decision {
	switch operation {
	case OperationCreate, OperationFork:
		switch status {
		case "ACTIVE":
			return Success
		case "FAILED":
			return Failed
		case "ISOLATED":
			return Preempted
		default:
			return Continue
		}
	case OperationUpdate:
		switch status {
		case "ACTIVE", "ISOLATED":
			return Success
		case "FAILED":
			return Failed
		default:
			return Continue
		}
	case OperationDelete:
		return Continue
	default:
		switch status {
		case "ACTIVE", "ISOLATED":
			return Success
		case "FAILED":
			return Failed
		default:
			return Continue
		}
	}
}

func normalizeStatus(status string) string {
	return strings.ToUpper(strings.TrimSpace(status))
}

func waitStatusError(
	code string,
	kind string,
	resourceType string,
	resourceID string,
	operation Operation,
	status string,
	statusReason string,
	attempts int,
	startedAt time.Time,
) error {
	message := fmt.Sprintf("%s %s %s finished with status %q", resourceType, resourceID, operation, status)
	hint := fmt.Sprintf("Inspect the resource with 'agr %s get %s'.", resourceType, resourceID)
	if code == "WAIT_PREEMPTED" {
		message = fmt.Sprintf("%s %s %s was preempted; current status is %q", resourceType, resourceID, operation, status)
	}
	if statusReason != "" {
		message += ": " + statusReason
	}
	details := waitDetails(resourceType, resourceID, operation, status, attempts, startedAt)
	if statusReason != "" {
		details["StatusReason"] = statusReason
	}
	return output.NewCLIError(&output.Failure{
		Code:    code,
		Kind:    kind,
		Message: message,
		Hint:    hint,
		Details: details,
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

func waitContextError(
	parent context.Context,
	resourceType string,
	resourceID string,
	policy Policy,
	lastStatus string,
	attempts int,
	startedAt time.Time,
) error {
	if err := parent.Err(); err != nil {
		return err
	}
	hint := fmt.Sprintf("Run 'agr %s get %s --wait' to continue waiting.", resourceType, resourceID)
	if policy.TimeoutHint != nil {
		hint = policy.TimeoutHint(resourceID)
	}
	return output.NewCLIError(&output.Failure{
		Code:      "WAIT_TIMEOUT",
		Kind:      output.KindTimeout,
		Message:   fmt.Sprintf("timed out waiting for %s %s %s", resourceType, resourceID, policy.Operation),
		Hint:      hint,
		Retryable: true,
		Details:   waitDetails(resourceType, resourceID, policy.Operation, lastStatus, attempts, startedAt),
	})
}

func waitDetails(
	resourceType string,
	resourceID string,
	operation Operation,
	lastStatus string,
	attempts int,
	startedAt time.Time,
) map[string]any {
	return map[string]any{
		"ResourceType": resourceType,
		"ResourceId":   resourceID,
		"Operation":    string(operation),
		"LastStatus":   lastStatus,
		"Attempts":     attempts,
		"Elapsed":      time.Since(startedAt).Round(time.Millisecond).String(),
	}
}
