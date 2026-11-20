//go:build preview

package harnesscli

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

type preparePlane struct {
	before     func(context.Context, string) error
	tokenCalls atomic.Int32
}

func (p *preparePlane) Call(ctx context.Context, action string, request map[string]any) (any, error) {
	if action != "DescribeHarness" || request["HarnessId"] != "hns-test" {
		return nil, fmt.Errorf("unexpected %s: %v", action, request)
	}
	if p.before != nil {
		if err := p.before(ctx, action); err != nil {
			return nil, err
		}
	}
	return map[string]any{"Harness": map[string]any{"ResourceInfo": map[string]any{"DeploymentId": "dpl-test"}}}, nil
}

func (p *preparePlane) GetDeployment(ctx context.Context, id string) (apivalue.Object, error) {
	if id != "dpl-test" {
		return nil, fmt.Errorf("unexpected Deployment %s", id)
	}
	if p.before != nil {
		if err := p.before(ctx, "DescribeDeployment"); err != nil {
			return nil, err
		}
	}
	return apivalue.Object{"Status": "ACTIVE", "AffinityConfiguration": map[string]any{"Mode": "EXCLUSIVE"}}, nil
}

func (p *preparePlane) GetDeploymentToken(ctx context.Context, id string) (apivalue.Object, error) {
	p.tokenCalls.Add(1)
	if id != "dpl-test" {
		return nil, fmt.Errorf("unexpected Deployment %s", id)
	}
	if p.before != nil {
		if err := p.before(ctx, "AcquireDeploymentToken"); err != nil {
			return nil, err
		}
	}
	return apivalue.Object{"Token": "dpt-prefetched", "ExpiresAt": time.Now().Add(24 * time.Hour).Format(time.RFC3339)}, nil
}

func prepareConfig(t *testing.T) {
	t.Helper()
	cfg := config.Get()
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Region, cfg.Domain = "ap-chongqing", "tencentags.com"
	cfg.Auth.SecretID, cfg.Auth.SecretKey = "fake-id", "fake-key"
}

func TestPrepareEndpointOverlapsReadsAndReusesToken(t *testing.T) {
	prepareConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started, release := make(chan string, 2), make(chan struct{})
	plane := &preparePlane{before: func(ctx context.Context, action string) error {
		if action == "DescribeHarness" {
			return nil
		}
		started <- action
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	deps := command.Deps{ControlPlane: plane}
	client, err := NewClient(deps, "harness.prompt")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		endpoint, target, err := PrepareEndpoint(ctx, deps, client, "hns-test", "aff-test")
		if err == nil && (target.DeploymentID != "dpl-test" || endpoint.AffinityID != "aff-test" || endpoint.AffinityHeader != defaultAffinityHeader) {
			err = fmt.Errorf("unexpected endpoint %+v target %+v", endpoint, target)
		}
		if err == nil {
			// Initialize, streams and subsequent requests all use this source.
			var requests sync.WaitGroup
			for range 8 {
				requests.Go(func() {
					token, tokenErr := endpoint.Token(ctx)
					if tokenErr != nil || token != "dpt-prefetched" {
						t.Errorf("cached token=%q err=%v", token, tokenErr)
					}
				})
			}
			requests.Wait()
		}
		done <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("Deployment read and token prefetch did not overlap")
		}
	}
	close(release)
	if err := <-done; err != nil || plane.tokenCalls.Load() != 1 {
		t.Fatalf("prepare err=%v token calls=%d", err, plane.tokenCalls.Load())
	}
}

func TestPrepareEndpointFailureAndCancellationJoinPeer(t *testing.T) {
	prepareConfig(t)
	for _, failedAction := range []string{"DescribeDeployment", "AcquireDeploymentToken", "cancel", "timeout"} {
		t.Run(failedAction, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failedAction == "timeout" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer deadlineCancel()
			}
			started, stopped := make(chan string, 2), make(chan struct{}, 2)
			peerStarted := make(chan struct{})
			failure := output.NewAuthError("PREPARE_DENIED", "permission denied", "Check the permission.")
			plane := &preparePlane{before: func(ctx context.Context, action string) error {
				if action == "DescribeHarness" {
					return nil
				}
				started <- action
				defer func() { stopped <- struct{}{} }()
				if action == failedAction {
					select {
					case <-peerStarted:
						return failure
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				if failedAction != "cancel" && failedAction != "timeout" {
					close(peerStarted)
				}
				<-ctx.Done()
				return ctx.Err()
			}}
			deps := command.Deps{ControlPlane: plane}
			client, err := NewClient(deps, "harness.prompt")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, _, err := PrepareEndpoint(ctx, deps, client, "hns-test", "")
				done <- err
			}()
			if failedAction == "cancel" {
				for range 2 {
					select {
					case <-started:
					case <-time.After(2 * time.Second):
						t.Fatal("preparation did not start both requests")
					}
				}
				cancel()
			}
			select {
			case err := <-done:
				want := error(failure)
				switch failedAction {
				case "cancel":
					want = context.Canceled
				case "timeout":
					want = context.DeadlineExceeded
				}
				if !errors.Is(err, want) || len(stopped) != 2 || plane.tokenCalls.Load() != 1 {
					t.Fatalf("err=%v stopped=%d tokens=%d", err, len(stopped), plane.tokenCalls.Load())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("preparation did not cancel and join its peer")
			}
		})
	}
}

func TestStdioEndpointKeepsLazyToken(t *testing.T) {
	prepareConfig(t)
	plane := &preparePlane{}
	deps := command.Deps{ControlPlane: plane}
	client, err := NewClient(deps, "harness.acp")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _, err := ResolveEndpoint(t.Context(), deps, client, "hns-test", "")
	if err != nil || plane.tokenCalls.Load() != 0 {
		t.Fatalf("stdio acquired a token before initialize: err=%v calls=%d", err, plane.tokenCalls.Load())
	}
	if token, err := endpoint.Token(t.Context()); err != nil || token != "dpt-prefetched" || plane.tokenCalls.Load() != 1 {
		t.Fatalf("stdio token=%q err=%v calls=%d", token, err, plane.tokenCalls.Load())
	}
}
