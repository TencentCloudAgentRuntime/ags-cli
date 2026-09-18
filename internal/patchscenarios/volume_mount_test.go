package patchscenarios

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

func TestMountStartInstanceTracksWaitFailureForCleanup(t *testing.T) {
	for _, tc := range []struct {
		name         string
		omitID       bool
		cancelCreate bool
	}{
		{name: "failure details"},
		{name: "tool discovery fallback", omitID: true},
		{name: "cancelled create cleanup discovery", cancelCreate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testMountWaitFailureCleanup(t, tc.omitID, tc.cancelCreate)
		})
	}
}

func testMountWaitFailureCleanup(t *testing.T, omitID, cancelCreate bool) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "calls.log")
	binary := filepath.Join(t.TempDir(), "fake-agr")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$CALL_LOG"
case "$*" in
  "instance create "*)
	if [ "$CANCEL_CREATE" = "1" ]; then
	  : > "$CALL_LOG.created"
	  exec sleep 30
	elif [ "$OMIT_RESOURCE_ID" = "1" ]; then
      printf '%s\n' '{"Status":"failed","Failure":{"Code":"ResourceNotReady","Kind":"runtime","Message":"wait timed out"}}'
    else
      printf '%s\n' '{"Status":"failed","Failure":{"Code":"ResourceNotReady","Kind":"runtime","Message":"wait timed out","Details":{"ResourceId":"instance-wait-failed"}}}'
    fi
    exit 1
    ;;
  "instance list "*)
    if [ -f "$CALL_LOG.deleted" ]; then
      printf '%s\n' '{"Status":"succeeded","Data":{"Items":[{"InstanceId":"instance-wait-failed","Status":"STOPPED"}]}}'
    else
      printf '%s\n' '{"Status":"succeeded","Data":{"Items":[{"InstanceId":"instance-wait-failed","Status":"RUNNING"}]}}'
    fi
    ;;
  "instance delete "*)
    : > "$CALL_LOG.deleted"
    printf '%s\n' '{"Status":"succeeded","Data":{"InstanceId":"instance-wait-failed","Status":"STOPPED"}}'
    ;;
  *)
    printf '%s\n' '{"Status":"failed","Failure":{"Code":"UnexpectedCall","Kind":"internal","Message":"unexpected fake CLI call"}}'
    exit 1
    ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	registry := patchtest.Registry{
		"wait-failure-cleanup": {
			Assertions: []string{"wait.failed"},
			Run: func(s *patchtest.Session) error {
				owned := &volumeMountOwned{}
				s.Cleanup(func(ctx context.Context) error { return owned.cleanup(s, ctx) })
				createCtx := s.Context
				var cancelResult <-chan error
				if cancelCreate {
					var cancel context.CancelFunc
					createCtx, cancel = context.WithCancel(s.Context)
					defer cancel()
					cancelResult = cancelWhenFileExists(s.Context, logPath+".created", cancel)
				}
				_, err := mountStartInstance(s, createCtx, owned, "tool-1", "reuse-1")
				if cancelResult != nil {
					if cancelErr := <-cancelResult; cancelErr != nil {
						return cancelErr
					}
				}
				return s.Assert("wait.failed", err != nil)
			},
		},
	}
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: "wait-failure-cleanup", Assertions: []string{"wait.failed"}}}}
	env := []string{"CALL_LOG=" + logPath}
	if omitID {
		env = append(env, "OMIT_RESOURCE_ID=1")
	}
	if cancelCreate {
		env = append(env, "CANCEL_CREATE=1")
	}
	results, err := patchtest.Run(t.Context(), plan, registry, binary, env)
	if err != nil {
		t.Fatalf("run: %v; results=%+v", err, results)
	}
	if cancelCreate {
		if _, err := os.Stat(logPath + ".created"); err != nil {
			t.Fatalf("fake remote creation was not reached: %v", err)
		}
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "instance delete instance-wait-failed --wait") {
		t.Fatalf("cleanup did not delete wait-failed instance:\n%s", log)
	}
	wantListCalls := 2
	if omitID || cancelCreate {
		wantListCalls++
	}
	if got := strings.Count(string(log), "instance list "); got != wantListCalls {
		t.Fatalf("instance list calls=%d, want %d:\n%s", got, wantListCalls, log)
	}
}

func cancelWhenFileExists(ctx context.Context, path string, cancel context.CancelFunc) <-chan error {
	result := make(chan error, 1)
	go func() {
		waitCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(path); err == nil {
				cancel()
				result <- nil
				return
			}
			select {
			case <-waitCtx.Done():
				cancel()
				result <- waitCtx.Err()
				return
			case <-ticker.C:
			}
		}
	}()
	return result
}
