package resourcewait

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestDefaults(t *testing.T) {
	if DefaultInterval != 5*time.Second {
		t.Fatalf("DefaultInterval = %s, want 5s", DefaultInterval)
	}
	if DefaultTimeout != 10*time.Minute {
		t.Fatalf("DefaultTimeout = %s, want 10m", DefaultTimeout)
	}
}

func TestWaitFlagAndRequested(t *testing.T) {
	flag := Flag()
	if flag.Name != "wait" || flag.Type != command.FlagBool || !flag.Workflow {
		t.Fatalf("Flag() = %#v", flag)
	}
	if flag.Usage != "Wait until the requested operation reaches a final outcome" {
		t.Fatalf("Flag().Usage = %q", flag.Usage)
	}
	if !Requested(command.Request{Flags: map[string]command.FlagValue{
		"wait": {Name: "wait", Type: command.FlagBool, Bool: true},
	}}) {
		t.Fatal("Requested should report true for --wait")
	}
}

func TestWaitForInstanceGetPollsUntilNonFailureTerminalState(t *testing.T) {
	statuses := []string{"STARTING", "RUNNING"}
	calls := 0
	got, err := WaitForInstance(context.Background(), "ins-1", func(context.Context, string) (*ags.SandboxInstance, error) {
		status := statuses[calls]
		calls++
		return &ags.SandboxInstance{Status: &status}, nil
	}, testOptions())
	if err != nil {
		t.Fatalf("WaitForInstance returned error: %v", err)
	}
	if calls != 2 || got == nil || got.Status == nil || *got.Status != "RUNNING" {
		t.Fatalf("calls = %d, instance = %#v", calls, got)
	}
}

func TestWaitForToolGetPollsUntilNonFailureTerminalState(t *testing.T) {
	statuses := []string{"CREATING", "ACTIVE"}
	calls := 0
	got, err := WaitForTool(context.Background(), "tool-1", func(context.Context, string) (*ags.SandboxTool, error) {
		status := statuses[calls]
		calls++
		return &ags.SandboxTool{Status: &status}, nil
	}, testOptions())
	if err != nil {
		t.Fatalf("WaitForTool returned error: %v", err)
	}
	if calls != 2 || got == nil || got.Status == nil || *got.Status != "ACTIVE" {
		t.Fatalf("calls = %d, tool = %#v", calls, got)
	}
}

func TestWaitPollsImmediately(t *testing.T) {
	status := "ACTIVE"
	_, err := WaitForTool(context.Background(), "tool-1", func(context.Context, string) (*ags.SandboxTool, error) {
		return &ags.SandboxTool{Status: &status}, nil
	}, Options{
		Interval: 50 * time.Millisecond,
		Timeout:  10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("WaitForTool returned error before its immediate first poll: %v", err)
	}
}

func TestWaitForInstanceGetReturnsFailureStateAsError(t *testing.T) {
	for _, status := range []string{
		"FAILED", "STARTING_FAILED", "STOPPING_FAILED", "STOP_FAILED",
		"PAUSE_FAILED", "RESUME_FAILED",
	} {
		t.Run(status, func(t *testing.T) {
			_, err := WaitForInstance(context.Background(), "ins-1", instanceStatusGetter(status), testOptions())
			assertWaitError(t, err, "WAIT_FAILED", status, OperationGet)
		})
	}
}

func TestWaitForToolGetReturnsFailedAsError(t *testing.T) {
	_, err := WaitForTool(context.Background(), "tool-1", toolStatusGetter("FAILED"), testOptions())
	assertWaitError(t, err, "WAIT_FAILED", "FAILED", OperationGet)
}

func TestWaitForInstancePauseUsesOperationOutcome(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		statuses := []string{"RUNNING", "PAUSING", "PAUSED"}
		calls := 0
		got, err := WaitForInstanceWithPolicy(
			context.Background(),
			"ins-1",
			func(context.Context, string) (*ags.SandboxInstance, error) {
				status := statuses[calls]
				calls++
				return &ags.SandboxInstance{Status: &status}, nil
			},
			InstancePolicy(OperationPause),
			testOptions(),
		)
		if err != nil || calls != 3 || got == nil || got.Status == nil || *got.Status != "PAUSED" {
			t.Fatalf("calls = %d, instance = %#v, error = %v", calls, got, err)
		}
	})

	t.Run("backend rollback to running", func(t *testing.T) {
		statuses := []string{"PAUSING", "RUNNING"}
		calls := 0
		_, err := WaitForInstanceWithPolicy(
			context.Background(),
			"ins-1",
			func(context.Context, string) (*ags.SandboxInstance, error) {
				status := statuses[calls]
				calls++
				return &ags.SandboxInstance{Status: &status}, nil
			},
			InstancePolicy(OperationPause),
			testOptions(),
		)
		assertWaitError(t, err, "WAIT_FAILED", "RUNNING", OperationPause)
		if calls != 2 {
			t.Fatalf("calls = %d, want 2", calls)
		}
	})

	t.Run("running without observed progress times out", func(t *testing.T) {
		_, err := WaitForInstanceWithPolicy(
			context.Background(),
			"ins-1",
			instanceStatusGetter("RUNNING"),
			InstancePolicy(OperationPause),
			testOptions(),
		)
		assertWaitError(t, err, "WAIT_TIMEOUT", "RUNNING", OperationPause)
	})

	t.Run("preempted by stop", func(t *testing.T) {
		_, err := WaitForInstanceWithPolicy(
			context.Background(),
			"ins-1",
			instanceStatusGetter("STOPPED"),
			InstancePolicy(OperationPause),
			testOptions(),
		)
		assertWaitError(t, err, "WAIT_PREEMPTED", "STOPPED", OperationPause)
	})
}

func TestWaitForInstanceResumeUsesOperationOutcome(t *testing.T) {
	_, err := WaitForInstanceWithPolicy(
		context.Background(),
		"ins-1",
		instanceStatusGetter("RESUME_FAILED"),
		InstancePolicy(OperationResume),
		testOptions(),
	)
	assertWaitError(t, err, "WAIT_FAILED", "RESUME_FAILED", OperationResume)
}

func TestWaitForInstanceDeleteAcceptsBothStopFailureNames(t *testing.T) {
	for _, status := range []string{"STOPPING_FAILED", "STOP_FAILED"} {
		t.Run(status, func(t *testing.T) {
			_, err := WaitForInstanceWithPolicy(
				context.Background(),
				"ins-1",
				instanceStatusGetter(status),
				InstancePolicy(OperationDelete),
				testOptions(),
			)
			assertWaitError(t, err, "WAIT_FAILED", status, OperationDelete)
		})
	}
}

func TestWaitForToolCreateTreatsIsolatedAsUnavailable(t *testing.T) {
	_, err := WaitForToolWithPolicy(
		context.Background(),
		"tool-1",
		toolStatusGetter("ISOLATED"),
		ToolPolicy(OperationCreate),
		testOptions(),
	)
	assertWaitError(t, err, "WAIT_PREEMPTED", "ISOLATED", OperationCreate)
}

func TestUnknownStatusKeepsPolling(t *testing.T) {
	statuses := []string{"NEW_SERVER_STATE", "RUNNING"}
	calls := 0
	got, err := WaitForInstanceWithPolicy(
		context.Background(),
		"ins-1",
		func(context.Context, string) (*ags.SandboxInstance, error) {
			status := statuses[calls]
			calls++
			return &ags.SandboxInstance{Status: &status}, nil
		},
		InstancePolicy(OperationCreate),
		testOptions(),
	)
	if err != nil || calls != 2 || got == nil || got.Status == nil || *got.Status != "RUNNING" {
		t.Fatalf("calls = %d, instance = %#v, error = %v", calls, got, err)
	}
}

func TestUnknownStatusTimesOutWithDiagnostics(t *testing.T) {
	status := "NEW_SERVER_STATE"
	_, err := WaitForInstanceWithPolicy(
		context.Background(),
		"ins-1",
		instanceStatusGetter(status),
		InstancePolicy(OperationCreate),
		Options{Interval: time.Millisecond, Timeout: 5 * time.Millisecond},
	)
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error = %T %v, want *output.CLIError", err, err)
	}
	if cliErr.Failure.Code != "WAIT_TIMEOUT" || cliErr.Failure.Kind != output.KindTimeout {
		t.Fatalf("failure = %#v", cliErr.Failure)
	}
	details := cliErr.Failure.Details
	if details["LastStatus"] != status || details["Operation"] != string(OperationCreate) {
		t.Fatalf("details = %#v", details)
	}
	attempts, ok := details["Attempts"].(int)
	if !ok || attempts < 2 {
		t.Fatalf("Attempts = %#v, want at least 2", details["Attempts"])
	}
	if _, ok := details["Elapsed"]; !ok {
		t.Fatalf("details missing Elapsed: %#v", details)
	}
}

func TestWaitForToolDeleteTreatsOnlyNotFoundAsSuccess(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		options := testOptions()
		options.IsNotFound = isNotFoundError
		got, err := WaitForToolWithPolicy(
			context.Background(),
			"tool-1",
			func(context.Context, string) (*ags.SandboxTool, error) {
				return nil, output.NewNotFoundError("TOOL_NOT_FOUND", "missing", "hint")
			},
			ToolPolicy(OperationDelete),
			options,
		)
		if err != nil || got != nil {
			t.Fatalf("tool = %#v, error = %v", got, err)
		}
	})

	t.Run("other error", func(t *testing.T) {
		options := testOptions()
		options.IsNotFound = isNotFoundError
		want := errors.New("network down")
		_, err := WaitForToolWithPolicy(
			context.Background(),
			"tool-1",
			func(context.Context, string) (*ags.SandboxTool, error) {
				return nil, want
			},
			ToolPolicy(OperationDelete),
			options,
		)
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
		}
		var cliErr *output.CLIError
		if !errors.As(err, &cliErr) || cliErr.Failure.Details["ResourceId"] != "tool-1" {
			t.Fatalf("error = %T %v, failure = %#v", err, err, cliErr)
		}
	})

	t.Run("CLI error details", func(t *testing.T) {
		options := testOptions()
		options.IsNotFound = isNotFoundError
		want := output.NewCLIError(&output.Failure{
			Code:      "GET_FAILED",
			Kind:      output.KindNetwork,
			Message:   "network down",
			Hint:      "retry",
			Retryable: true,
			Details:   map[string]any{"RequestId": "req-1"},
		})
		_, err := WaitForToolWithPolicy(
			context.Background(),
			"tool-1",
			func(context.Context, string) (*ags.SandboxTool, error) {
				return nil, want
			},
			ToolPolicy(OperationDelete),
			options,
		)
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
		}
		var cliErr *output.CLIError
		if !errors.As(err, &cliErr) {
			t.Fatalf("error = %T %v, want *output.CLIError", err, err)
		}
		if cliErr.Failure.Code != "GET_FAILED" || cliErr.Failure.Kind != output.KindNetwork || !cliErr.Failure.Retryable {
			t.Fatalf("failure = %#v", cliErr.Failure)
		}
		details := cliErr.Failure.Details
		if details["RequestId"] != "req-1" || details["ResourceType"] != "tool" || details["ResourceId"] != "tool-1" {
			t.Fatalf("details = %#v", details)
		}
		if details["Operation"] != string(OperationDelete) || details["LastStatus"] != "" || details["Attempts"] != 1 {
			t.Fatalf("wait details = %#v", details)
		}
	})
}

func TestWaitForDeploymentDeleteTreatsNotFoundAsSuccess(t *testing.T) {
	status := "DELETING"
	calls := 0
	options := testOptions()
	options.IsNotFound = isNotFoundError
	got, err := WaitForDeploymentDeletion(
		context.Background(),
		"dpl-1",
		func(context.Context, string) (*ags.Deployment, error) {
			calls++
			if calls == 1 {
				return &ags.Deployment{Status: &status}, nil
			}
			return nil, output.NewNotFoundError("DEPLOYMENT_NOT_FOUND", "missing", "hint")
		},
		options,
	)
	if err != nil || got != nil || calls != 2 {
		t.Fatalf("deployment = %#v, calls = %d, error = %v", got, calls, err)
	}
}

func TestWaitForDeploymentDeleteIncludesFailureReason(t *testing.T) {
	status := "DELETE_FAILED"
	reason := "ProviderError: sandbox cleanup failed"
	_, err := WaitForDeploymentDeletion(
		context.Background(),
		"dpl-1",
		func(context.Context, string) (*ags.Deployment, error) {
			return &ags.Deployment{Status: &status, StatusReason: &reason}, nil
		},
		testOptions(),
	)
	assertWaitError(t, err, "WAIT_FAILED", status, OperationDelete)
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error = %T %v, want *output.CLIError", err, err)
	}
	if !strings.Contains(cliErr.Failure.Message, reason) || cliErr.Failure.Details["StatusReason"] != reason {
		t.Fatalf("failure = %#v, want status reason", cliErr.Failure)
	}
}

func TestWaitForDeploymentDeleteTimeoutSuggestsAValidCommand(t *testing.T) {
	status := "DELETING"
	_, err := WaitForDeploymentDeletion(
		context.Background(),
		"dpl-1",
		func(context.Context, string) (*ags.Deployment, error) {
			return &ags.Deployment{Status: &status}, nil
		},
		Options{Interval: time.Millisecond, Timeout: 5 * time.Millisecond},
	)
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error = %T %v, want *output.CLIError", err, err)
	}
	if got, want := cliErr.Failure.Hint, "Inspect the resource with 'agr deployment get dpl-1'."; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
}

func TestWaitStopsWhenParentContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	status := "STARTING"
	_, err := WaitForInstance(ctx, "ins-1", func(context.Context, string) (*ags.SandboxInstance, error) {
		cancel()
		return &ags.SandboxInstance{Status: &status}, nil
	}, testOptions())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func instanceStatusGetter(status string) func(context.Context, string) (*ags.SandboxInstance, error) {
	return func(context.Context, string) (*ags.SandboxInstance, error) {
		return &ags.SandboxInstance{Status: &status}, nil
	}
}

func toolStatusGetter(status string) func(context.Context, string) (*ags.SandboxTool, error) {
	return func(context.Context, string) (*ags.SandboxTool, error) {
		return &ags.SandboxTool{Status: &status}, nil
	}
}

func assertWaitError(t *testing.T, err error, code, status string, operation Operation) {
	t.Helper()
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error = %T %v, want *output.CLIError", err, err)
	}
	if cliErr.Failure.Code != code || cliErr.Failure.Details["LastStatus"] != status {
		t.Fatalf("failure = %#v", cliErr.Failure)
	}
	if cliErr.Failure.Details["Operation"] != string(operation) {
		t.Fatalf("operation details = %#v", cliErr.Failure.Details)
	}
}

func isNotFoundError(err error) bool {
	var cliErr *output.CLIError
	return errors.As(err, &cliErr) && cliErr.Failure != nil && cliErr.Failure.Kind == output.KindNotFound
}

func testOptions() Options {
	return Options{
		Interval: time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}
}
