//go:build preview

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

const testToken = "browser-secret/+?&= with spaces"

type webPlane struct {
	tokenCalls int
	beforeRead func(context.Context, string) error
}

func (p *webPlane) Call(ctx context.Context, action string, _ map[string]any) (any, error) {
	if p.beforeRead != nil {
		if err := p.beforeRead(ctx, action); err != nil {
			return nil, err
		}
	}
	switch action {
	case "DescribeHarness":
		return map[string]any{"Harness": map[string]any{"ResourceInfo": map[string]any{"DeploymentId": "dpl-test"}}}, nil
	case "DescribeHarnessRevisionList":
		return map[string]any{"HarnessRevisionSet": []any{map[string]any{"Revision": 3, "Status": "PUBLISHED", "Spec": map[string]any{"HarnessEngine": "dsh"}}}, "TotalCount": 1}, nil
	default:
		return nil, fmt.Errorf("unexpected action %s", action)
	}
}

func (p *webPlane) GetDeployment(ctx context.Context, _ string) (apivalue.Object, error) {
	if p.beforeRead != nil {
		if err := p.beforeRead(ctx, "DescribeDeployment"); err != nil {
			return nil, err
		}
	}
	return apivalue.Object{"Status": "ACTIVE"}, nil
}

func (p *webPlane) GetDeploymentToken(context.Context, string) (apivalue.Object, error) {
	p.tokenCalls++
	return apivalue.Object{"Token": testToken, "ExpiresAt": "2026-11-14T00:00:00Z"}, nil
}

func webConfig(t *testing.T) {
	t.Helper()
	cfg := config.Get()
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Region, cfg.Domain = "ap-chongqing", "tencentags.com"
	cfg.Auth.SecretID, cfg.Auth.SecretKey = "fake-id", "fake-key"
}

func TestWebBrowserAndOutput(t *testing.T) {
	webConfig(t)
	for _, noOpen := range []bool{false, true} {
		for _, showToken := range []bool{false, true} {
			t.Run(fmt.Sprintf("no-open=%t/show-token=%t", noOpen, showToken), func(t *testing.T) {
				plane := &webPlane{}
				client, err := harnesscli.NewClient(command.Deps{ControlPlane: plane}, "harness.web")
				if err != nil {
					t.Fatal(err)
				}
				req := command.Request{ArgValues: map[string]string{"harness-id": "hns-test"}, Flags: map[string]command.FlagValue{
					"no-open": {Bool: noOpen}, "show-token": {Bool: showToken}, "affinity-id": {String: "conversation /+&?=中文"},
				}}
				var opened []string
				result, err := run(t.Context(), client, plane, req, func(address string) error {
					opened = append(opened, address)
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				data := result.Data.(map[string]any)
				wantTokenCalls := 0
				if !noOpen || showToken {
					wantTokenCalls = 1
				}
				if plane.tokenCalls != wantTokenCalls || data["Opened"] != !noOpen {
					t.Fatalf("token calls=%d data=%+v", plane.tokenCalls, data)
				}
				if noOpen {
					if len(opened) != 0 {
						t.Fatalf("--no-open started browser: %v", opened)
					}
				} else {
					if len(opened) != 1 {
						t.Fatalf("browser launch count=%d", len(opened))
					}
					address, err := url.Parse(opened[0])
					if err != nil || address.Host != "3080-dpl-test-webview.ap-chongqing.agents.tencentags.com" || address.Path != "/" || address.Query().Get("token") != testToken || address.Query().Get("affinity_id") != "conversation /+&?=中文" {
						t.Fatalf("browser address=%v err=%v", address, err)
					}
				}
				var text bytes.Buffer
				result.Text(&text)
				encoded, err := json.Marshal(result.Data)
				if err != nil {
					t.Fatal(err)
				}
				for format, value := range map[string]string{"text": text.String(), "json": string(encoded)} {
					if showToken {
						if !strings.Contains(value, "AccessURL") || !strings.Contains(value, url.QueryEscape(testToken)) || !strings.Contains(value, "2026-11-14T00:00:00Z") {
							t.Errorf("%s explicit output missing login link/expiry: %s", format, value)
						}
					} else if strings.Contains(value, "AccessURL") || strings.Contains(value, testToken) || strings.Contains(value, url.QueryEscape(testToken)) || strings.Contains(value, "token=") {
						t.Errorf("%s default output leaked token: %s", format, value)
					}
				}
			})
		}
	}
}

func TestWebBrowserFailureHidesCredentialURL(t *testing.T) {
	webConfig(t)
	plane := &webPlane{}
	client, err := harnesscli.NewClient(command.Deps{ControlPlane: plane}, "harness.web")
	if err != nil {
		t.Fatal(err)
	}
	launches := 0
	result, err := run(t.Context(), client, plane, command.Request{ArgValues: map[string]string{"harness-id": "hns-test"}}, func(address string) error {
		launches++
		return fmt.Errorf("exec open %s: %s", address, testToken)
	})
	var failure *output.CLIError
	if result != nil || !errors.As(err, &failure) || failure.Failure.Code != "BROWSER_OPEN_FAILED" || failure.ExitCode == 0 || launches != 1 || plane.tokenCalls != 1 {
		t.Fatalf("result=%+v err=%v launches=%d tokens=%d", result, err, launches, plane.tokenCalls)
	}
	encoded, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), testToken) || strings.Contains(string(encoded), url.QueryEscape(testToken)) || strings.Contains(string(encoded), "token=") {
		t.Fatalf("browser error leaked credentials: %s", encoded)
	}
	if !strings.Contains(failure.Failure.Hint, "--no-open --show-token") {
		t.Fatalf("browser error missing headless recovery: %+v", failure.Failure)
	}
}

func TestWebReadsOverlap(t *testing.T) {
	webConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started, release := make(chan string, 2), make(chan struct{})
	plane := &webPlane{beforeRead: func(ctx context.Context, action string) error {
		if action == "DescribeDeployment" {
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
	client, err := harnesscli.NewClient(command.Deps{ControlPlane: plane}, "harness.web")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := run(ctx, client, plane, command.Request{ArgValues: map[string]string{"harness-id": "hns-test"}, Flags: map[string]command.FlagValue{"no-open": {Bool: true}}}, func(string) error {
			return errors.New("unexpected browser launch")
		})
		done <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("endpoint and revision requests did not overlap")
		}
	}
	close(release)
	if err := <-done; err != nil || plane.tokenCalls != 0 {
		t.Fatalf("web result: err=%v token calls=%d", err, plane.tokenCalls)
	}
}

func TestWebReadFailureAndCancellationJoinPeer(t *testing.T) {
	webConfig(t)
	for _, failedAction := range []string{"DescribeHarness", "DescribeHarnessRevisionList", "cancel"} {
		t.Run(failedAction, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			started, stopped := make(chan string, 2), make(chan struct{}, 2)
			peerStarted := make(chan struct{})
			failure := output.NewAuthError("READ_DENIED", "read denied", "Check the read permission.")
			plane := &webPlane{beforeRead: func(ctx context.Context, action string) error {
				started <- action
				defer func() { stopped <- struct{}{} }()
				if action == failedAction {
					select {
					case <-peerStarted:
					case <-ctx.Done():
						return ctx.Err()
					}
					return failure
				}
				if failedAction != "cancel" {
					close(peerStarted)
				}
				<-ctx.Done()
				return ctx.Err()
			}}
			client, err := harnesscli.NewClient(command.Deps{ControlPlane: plane}, "harness.web")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			launches := 0
			go func() {
				_, err := run(ctx, client, plane, command.Request{ArgValues: map[string]string{"harness-id": "hns-test"}}, func(string) error { launches++; return nil })
				done <- err
			}()
			if failedAction == "cancel" {
				for range 2 {
					select {
					case <-started:
					case <-ctx.Done():
						t.Fatal("reads did not both start")
					}
				}
				cancel()
			}
			select {
			case err := <-done:
				want := error(failure)
				if failedAction == "cancel" {
					want = context.Canceled
				}
				if !errors.Is(err, want) || len(stopped) != 2 || launches != 0 || plane.tokenCalls != 0 {
					t.Fatalf("err=%v stopped=%d launches=%d tokens=%d", err, len(stopped), launches, plane.tokenCalls)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("failed read did not cancel and join its peer")
			}
		})
	}
}
