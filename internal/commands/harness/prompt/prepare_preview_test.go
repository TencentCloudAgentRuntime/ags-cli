//go:build preview

package prompt

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
)

type blockedPreparePlane struct {
	phase            string
	started, stopped chan struct{}
}

func (p *blockedPreparePlane) wait(ctx context.Context, phase string) error {
	if phase != p.phase {
		return nil
	}
	close(p.started)
	defer close(p.stopped)
	<-ctx.Done()
	return ctx.Err()
}

func (p *blockedPreparePlane) Call(ctx context.Context, action string, _ map[string]any) (any, error) {
	if err := p.wait(ctx, action); err != nil {
		return nil, err
	}
	return map[string]any{"Harness": map[string]any{"ResourceInfo": map[string]any{"DeploymentId": "dpl-test"}}}, nil
}

func (p *blockedPreparePlane) GetDeployment(ctx context.Context, _ string) (apivalue.Object, error) {
	return apivalue.Object{}, p.wait(ctx, "DescribeDeployment")
}

func (p *blockedPreparePlane) GetDeploymentToken(ctx context.Context, _ string) (apivalue.Object, error) {
	if err := p.wait(ctx, "AcquireDeploymentToken"); err != nil {
		return nil, err
	}
	return apivalue.Object{"Token": "dpt-test", "ExpiresAt": time.Now().Add(24 * time.Hour).Format(time.RFC3339)}, nil
}

func TestPromptTimeoutAndCancellationCoverPreparation(t *testing.T) {
	cfg := config.Get()
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Region, cfg.Domain = "ap-chongqing", "tencentags.com"
	cfg.Auth.SecretID, cfg.Auth.SecretKey = "fake-id", "fake-key"
	for _, phase := range []string{"DescribeHarness", "DescribeDeployment", "AcquireDeploymentToken"} {
		for _, mode := range []string{"timeout", "cancel"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				plane := &blockedPreparePlane{phase: phase, started: make(chan struct{}), stopped: make(chan struct{})}
				ios, _, _, _ := iostreams.Test()
				deps := command.Deps{IO: ios, ControlPlane: plane}
				client, err := harnesscli.NewClient(deps, "harness.prompt")
				if err != nil {
					t.Fatal(err)
				}
				timeout := "75ms"
				if mode == "cancel" {
					timeout = "5s"
				}
				req := command.Request{ArgValues: map[string]string{"harness-id": "hns-test", "text": "hello"}, Flags: map[string]command.FlagValue{"timeout": {String: timeout}}}
				done := make(chan error, 1)
				go func() { _, err := run(ctx, req, deps, client); done <- err }()
				select {
				case <-plane.started:
				case <-time.After(2 * time.Second):
					t.Fatal("preparation did not start")
				}
				want := error(context.DeadlineExceeded)
				if mode == "cancel" {
					want = context.Canceled
					cancel()
				}
				select {
				case err := <-done:
					if !errors.Is(err, want) {
						t.Fatalf("err=%v, want %v", err, want)
					}
					select {
					case <-plane.stopped:
					default:
						t.Fatal("prompt returned before the preparation request stopped")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("prompt did not honor timeout/cancellation during preparation")
				}
			})
		}
	}
}
