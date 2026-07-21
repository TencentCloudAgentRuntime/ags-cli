package resourcewait

import (
	"context"
	"errors"
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
	if flag.Usage != "Wait until the resource leaves a transitional state" {
		t.Fatalf("Flag().Usage = %q", flag.Usage)
	}
	if !Requested(command.Request{Flags: map[string]command.FlagValue{
		"wait": {Name: "wait", Type: command.FlagBool, Bool: true},
	}}) {
		t.Fatal("Requested should report true for --wait")
	}
}

func TestWaitForInstancePollsUntilStableState(t *testing.T) {
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

func TestWaitForToolPollsUntilStableState(t *testing.T) {
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

func TestWaitForToolCanDelayFirstPoll(t *testing.T) {
	const interval = 20 * time.Millisecond
	startedAt := time.Now()
	status := "ACTIVE"
	_, err := WaitForTool(context.Background(), "tool-1", func(context.Context, string) (*ags.SandboxTool, error) {
		if elapsed := time.Since(startedAt); elapsed < interval {
			t.Fatalf("first poll started after %s, want at least %s", elapsed, interval)
		}
		return &ags.SandboxTool{Status: &status}, nil
	}, Options{
		Interval:             interval,
		Timeout:              100 * time.Millisecond,
		DelayBeforeFirstPoll: true,
	})
	if err != nil {
		t.Fatalf("WaitForTool returned error: %v", err)
	}
}

func TestWaitForInstanceReturnsEveryKnownStableState(t *testing.T) {
	statuses := []string{
		"RUNNING", "PAUSED", "STOPPED", "FAILED", "STARTING_FAILED",
		"STOPPING_FAILED", "PAUSE_FAILED", "RESUME_FAILED",
	}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			calls := 0
			got, err := WaitForInstance(context.Background(), "ins-1", func(context.Context, string) (*ags.SandboxInstance, error) {
				calls++
				return &ags.SandboxInstance{Status: &status}, nil
			}, testOptions())
			if err != nil {
				t.Fatalf("WaitForInstance returned error: %v", err)
			}
			if calls != 1 || got == nil || got.Status == nil || *got.Status != status {
				t.Fatalf("calls = %d, instance = %#v", calls, got)
			}
		})
	}
}

func TestWaitForToolReturnsEveryKnownStableState(t *testing.T) {
	statuses := []string{"ACTIVE", "FAILED", "ISOLATED"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			calls := 0
			got, err := WaitForTool(context.Background(), "tool-1", func(context.Context, string) (*ags.SandboxTool, error) {
				calls++
				return &ags.SandboxTool{Status: &status}, nil
			}, testOptions())
			if err != nil {
				t.Fatalf("WaitForTool returned error: %v", err)
			}
			if calls != 1 || got == nil || got.Status == nil || *got.Status != status {
				t.Fatalf("calls = %d, tool = %#v", calls, got)
			}
		})
	}
}

func TestWaitForInstanceRecognizesEveryTransitionalState(t *testing.T) {
	for _, transitional := range []string{"STARTING", "PAUSING", "STOPPING"} {
		t.Run(transitional, func(t *testing.T) {
			statuses := []string{transitional, "RUNNING"}
			calls := 0
			_, err := WaitForInstance(context.Background(), "ins-1", func(context.Context, string) (*ags.SandboxInstance, error) {
				status := statuses[calls]
				calls++
				return &ags.SandboxInstance{Status: &status}, nil
			}, testOptions())
			if err != nil || calls != 2 {
				t.Fatalf("calls = %d, error = %v", calls, err)
			}
		})
	}
}

func TestWaitForToolRecognizesEveryTransitionalState(t *testing.T) {
	for _, transitional := range []string{"CREATING", "DELETING"} {
		t.Run(transitional, func(t *testing.T) {
			statuses := []string{transitional, "ACTIVE"}
			calls := 0
			_, err := WaitForTool(context.Background(), "tool-1", func(context.Context, string) (*ags.SandboxTool, error) {
				status := statuses[calls]
				calls++
				return &ags.SandboxTool{Status: &status}, nil
			}, testOptions())
			if err != nil || calls != 2 {
				t.Fatalf("calls = %d, error = %v", calls, err)
			}
		})
	}
}

func TestWaitForInstanceRejectsUnknownState(t *testing.T) {
	status := "NEW_SERVER_STATE"
	_, err := WaitForInstance(context.Background(), "ins-1", func(context.Context, string) (*ags.SandboxInstance, error) {
		return &ags.SandboxInstance{Status: &status}, nil
	}, testOptions())
	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error = %T %v, want *output.CLIError", err, err)
	}
	if cliErr.Failure.Code != "WAIT_UNKNOWN_STATUS" || cliErr.Failure.Kind != output.KindGenericError || cliErr.Failure.Details["LastStatus"] != status {
		t.Fatalf("failure = %#v", cliErr.Failure)
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

func testOptions() Options {
	return Options{
		Interval: time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}
}
