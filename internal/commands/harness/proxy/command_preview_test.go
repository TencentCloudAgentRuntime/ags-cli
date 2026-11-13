//go:build preview

package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	deploymentproxy "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/deployment/proxy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	dataplaneproxy "github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/proxy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

type fakePlane struct{ calls int }

func (p *fakePlane) Call(context.Context, string, map[string]any) (any, error) {
	p.calls++
	return map[string]any{"Harness": map[string]any{"ResourceInfo": map[string]any{"DeploymentId": "dpl-test"}}}, nil
}

func (p *fakePlane) GetDeployment(context.Context, string) (apivalue.Object, error) {
	p.calls++
	return apivalue.Object{}, nil
}

func (*fakePlane) GetDeploymentToken(context.Context, string) (apivalue.Object, error) {
	panic("the fake proxy never requests a token")
}

type fakeProxy struct{ started, stopped bool }

func (p *fakeProxy) Start() (string, error) {
	p.started = true
	return "127.0.0.1:1234", nil
}

func (p *fakeProxy) Stop() { p.stopped = true }

func TestProxyOutputFormats(t *testing.T) {
	cfg := config.Get()
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Region, cfg.Domain = "ap-guangzhou", "tencentags.com"
	cfg.Auth.SecretID, cfg.Auth.SecretKey = "fake-id", "fake-key"
	for _, mode := range []string{"json", "text", "ndjson"} {
		t.Run(mode, func(t *testing.T) {
			flags := cli.RootCmd().PersistentFlags()
			flag := flags.Lookup("output")
			previous, changed := flag.Value.String(), flag.Changed
			if err := flags.Set("output", mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = flags.Set("output", previous); flag.Changed = changed })
			plane, proxy := &fakePlane{}, &fakeProxy{}
			created := false
			ios, _, stdout, stderr := iostreams.Test()
			runtime, err := Module().Build(command.Deps{IO: ios, ControlPlane: plane, DataPlane: Runtime{
				NewProxy: func(dataplaneproxy.Options) (deploymentproxy.Proxy, error) { created = true; return proxy, nil },
				Wait:     func(context.Context) {},
			}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := runtime.Handler.Run(t.Context(), command.Request{ArgValues: map[string]string{"harness-id": "hns-test"}})
			if mode == "json" {
				failure := output.ClassifyError(err)
				if result != nil || failure == nil || failure.Failure.Code != "UNSUPPORTED_OUTPUT" || failure.ExitCode != output.ExitUsage || !strings.Contains(failure.Failure.Hint, "-o ndjson") {
					t.Fatalf("result = %+v, error = %v, stdout = %q", result, err, stdout)
				}
				if plane.calls != 0 || created || stdout.Len() != 0 {
					t.Fatalf("rejected JSON touched cloud or proxy: calls=%d created=%v stdout=%q", plane.calls, created, stdout)
				}
				return
			}
			if err != nil || result == nil || !result.StreamDone || !proxy.started || !proxy.stopped {
				t.Fatalf("result=%+v error=%v proxy=%+v", result, err, proxy)
			}
			if mode == "text" {
				if stdout.String() != "ACP endpoint: http://127.0.0.1:1234/acp\n" {
					t.Fatalf("stdout = %q", stdout)
				}
				return
			}
			var types []string
			decoder := json.NewDecoder(stdout)
			for {
				var event output.NDJSONEvent
				if err := decoder.Decode(&event); errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if event.Command != "harness.proxy" || event.Data.(map[string]any)["AcpURL"] != "http://127.0.0.1:1234/acp" {
					t.Fatalf("event = %+v", event)
				}
				types = append(types, event.Type)
			}
			if !reflect.DeepEqual(types, []string{"ready", "completed"}) || stderr.Len() != 0 {
				t.Fatalf("events = %v, stderr = %q", types, stderr)
			}
		})
	}
}
