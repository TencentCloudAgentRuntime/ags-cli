package proxy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	dataplaneproxy "github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/proxy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

type fakeControlPlane struct {
	tokenCalls      int
	deploymentCalls int
	deployment      *ags.Deployment
}

func (f *fakeControlPlane) GetDeployment(ctx context.Context, deploymentID string) (*ags.Deployment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.deploymentCalls++
	if deploymentID != "dpl-a1b2c3d4" {
		panic("unexpected deployment id")
	}
	if f.deployment != nil {
		return f.deployment, nil
	}
	return &ags.Deployment{}, nil
}

func (f *fakeControlPlane) GetDeploymentToken(ctx context.Context, deploymentID string) (*ags.AcquireDeploymentTokenResponseParams, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.tokenCalls++
	token, expires := "dpt_secret", "2026-08-26T08:00:00Z"
	if deploymentID != "dpl-a1b2c3d4" {
		panic("unexpected deployment id")
	}
	return &ags.AcquireDeploymentTokenResponseParams{Token: &token, ExpiresAt: &expires}, nil
}

func TestModuleIsTextOnlyLocalDebuggingWorkflow(t *testing.T) {
	spec := Module().Descriptor.Spec
	if spec.ID != "deployment.proxy" || spec.SupportsJSON || spec.SupportsNDJSON {
		t.Fatalf("spec = %#v", spec)
	}
	if !strings.Contains(strings.ToLower(spec.Long), "local debugging") {
		t.Fatalf("Long = %q, want local debugging recommendation", spec.Long)
	}
}

func TestRunProxyUsesLazyRefreshableTokenProvider(t *testing.T) {
	setupProxyConfig(t)
	cp := &fakeControlPlane{}
	fake := &fakeProxy{addr: "127.0.0.1:3000"}
	var options dataplaneproxy.Options
	var acquiredToken string
	var acquireErr error
	ios, _, stdout, _ := iostreams.Test()
	runtime, err := Module().Build(command.Deps{
		IO:           ios,
		ControlPlane: cp,
		DataPlane: RuntimeDeps{
			NewProxy: func(opts dataplaneproxy.Options) (Proxy, error) {
				if cp.tokenCalls != 0 {
					t.Fatalf("token acquired before proxy start: calls=%d", cp.tokenCalls)
				}
				options = opts
				return fake, nil
			},
			Wait: func(context.Context) {
				acquiredToken, acquireErr = options.TokenProvider(context.Background())
			},
			Now: func() time.Time { return time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC) },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		ArgValues: map[string]string{"deployment-id": "dpl-a1b2c3d4", "port": "3000:8080"},
		Flags: map[string]command.FlagValue{
			"address": {Name: "address", Type: command.FlagString, String: "127.0.0.1"},
			"verbose": {Name: "verbose", Type: command.FlagBool, Bool: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.StreamDone || !fake.started || !fake.stopped {
		t.Fatalf("result=%#v proxy=%#v", result, fake)
	}
	if cp.deploymentCalls != 1 {
		t.Fatalf("deployment calls = %d, want 1", cp.deploymentCalls)
	}
	if cp.tokenCalls != 1 || acquireErr != nil || acquiredToken != "dpt_secret" {
		t.Fatalf("lazy token = (%q, %v), calls=%d", acquiredToken, acquireErr, cp.tokenCalls)
	}
	if options.Token != "" || options.TokenProvider == nil || !options.RewriteOrigin || !options.PreserveHeaders {
		t.Fatalf("options = %#v", options)
	}
	if options.InstanceID != "dpl-a1b2c3d4" || options.RemotePort != 8080 || options.ListenAddress != "127.0.0.1:3000" {
		t.Fatalf("options = %#v", options)
	}
	if options.Domain != "ap-guangzhou.agents.tencentags.com" {
		t.Fatalf("Domain = %q, want Deployment authority domain", options.Domain)
	}
	if !strings.Contains(stdout.String(), "recommended only for local debugging") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "https://8080-dpl-a1b2c3d4.ap-guangzhou.agents.tencentags.com") {
		t.Fatalf("stdout remote authority = %q", stdout.String())
	}
}

func TestRunProxyConfiguresAndPrintsAffinitySession(t *testing.T) {
	setupProxyConfig(t)
	mode, headerName := "STRICT", "X-Workspace-Session"
	cp := &fakeControlPlane{deployment: &ags.Deployment{AffinityConfiguration: &ags.AffinityConfiguration{
		Mode:       &mode,
		HeaderName: &headerName,
	}}}
	fake := &fakeProxy{addr: "127.0.0.1:3000"}
	var options dataplaneproxy.Options
	ios, _, stdout, _ := iostreams.Test()
	runtime, err := Module().Build(command.Deps{
		IO:           ios,
		ControlPlane: cp,
		DataPlane: RuntimeDeps{
			NewProxy: func(opts dataplaneproxy.Options) (Proxy, error) {
				options = opts
				return fake, nil
			},
			Wait: func(context.Context) {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		ArgValues: map[string]string{"deployment-id": "dpl-a1b2c3d4", "port": "3000:8080"},
		Flags: map[string]command.FlagValue{
			"address":     {Name: "address", Type: command.FlagString, String: "127.0.0.1"},
			"affinity-id": {Name: "affinity-id", Type: command.FlagString, String: "session-from-flag"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.Affinity == nil || options.Affinity.HeaderName != headerName || options.Affinity.InitialID != "session-from-flag" {
		t.Fatalf("affinity options = %#v", options.Affinity)
	}
	options.Affinity.OnIDChange("session-from-response")
	for _, want := range []string{"Affinity ID: session-from-flag", "Affinity ID: session-from-response"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunProxyUsesDefaultAffinityHeader(t *testing.T) {
	setupProxyConfig(t)
	mode := "BEST_EFFORT"
	cp := &fakeControlPlane{deployment: &ags.Deployment{AffinityConfiguration: &ags.AffinityConfiguration{Mode: &mode}}}
	var options dataplaneproxy.Options
	runtime, err := Module().Build(command.Deps{
		ControlPlane: cp,
		DataPlane: RuntimeDeps{
			NewProxy: func(opts dataplaneproxy.Options) (Proxy, error) {
				options = opts
				return &fakeProxy{addr: "127.0.0.1:3000"}, nil
			},
			Wait: func(context.Context) {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		ArgValues: map[string]string{"deployment-id": "dpl-a1b2c3d4", "port": "3000"},
		Flags:     map[string]command.FlagValue{"address": {Name: "address", Type: command.FlagString, String: "127.0.0.1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.Affinity == nil || options.Affinity.HeaderName != "X-Tencent-Agr-Affinity-Id" {
		t.Fatalf("affinity options = %#v", options.Affinity)
	}
}

func TestRunProxyRejectsAffinityIDWhenDeploymentAffinityIsDisabled(t *testing.T) {
	setupProxyConfig(t)
	runtime, err := Module().Build(command.Deps{
		ControlPlane: &fakeControlPlane{},
		DataPlane: RuntimeDeps{
			NewProxy: func(dataplaneproxy.Options) (Proxy, error) {
				t.Fatal("proxy must not be created")
				return nil, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		ArgValues: map[string]string{"deployment-id": "dpl-a1b2c3d4", "port": "3000"},
		Flags: map[string]command.FlagValue{
			"address":     {Name: "address", Type: command.FlagString, String: "127.0.0.1"},
			"affinity-id": {Name: "affinity-id", Type: command.FlagString, String: "unused"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not enable affinity") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunProxyRetainsNonLoopbackWarning(t *testing.T) {
	setupProxyConfig(t)
	ios, _, _, stderr := iostreams.Test()
	runtime, err := Module().Build(command.Deps{
		IO:           ios,
		ControlPlane: &fakeControlPlane{},
		DataPlane: RuntimeDeps{
			NewProxy: func(dataplaneproxy.Options) (Proxy, error) { return &fakeProxy{addr: "0.0.0.0:3000"}, nil },
			Wait:     func(context.Context) {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{
		ArgValues: map[string]string{"deployment-id": "dpl-a1b2c3d4", "port": "3000"},
		Flags:     map[string]command.FlagValue{"address": {Name: "address", Type: command.FlagString, String: "0.0.0.0"}},
	})
	if err != nil || !strings.Contains(stderr.String(), "exposes the local debugging proxy") {
		t.Fatalf("error=%v stderr=%q", err, stderr.String())
	}
}

func TestParsePortSpec(t *testing.T) {
	for _, tc := range []struct {
		spec          string
		local, remote int
	}{
		{"8080", 8080, 8080},
		{"3000:8080", 3000, 8080},
		{"1:65535", 1, 65535},
	} {
		local, remote, err := parsePortSpec(tc.spec)
		if err != nil || local != tc.local || remote != tc.remote {
			t.Fatalf("parsePortSpec(%q) = (%d, %d, %v)", tc.spec, local, remote, err)
		}
	}
	for _, spec := range []string{"", "0", "65536", "a", "1:0", "1:65536", "1:2:3"} {
		if _, _, err := parsePortSpec(spec); err == nil {
			t.Fatalf("parsePortSpec(%q) unexpectedly succeeded", spec)
		}
	}
}

type fakeProxy struct {
	addr             string
	started, stopped bool
}

func (f *fakeProxy) Start() (string, error) { f.started = true; return f.addr, nil }
func (f *fakeProxy) Stop()                  { f.stopped = true }

func setupProxyConfig(t *testing.T) {
	t.Helper()
	if err := config.Init(); err != nil {
		t.Fatal(err)
	}
	config.SetSecretID("AKIDfake")
	config.SetSecretKey("fake")
	config.SetRegion("ap-guangzhou")
	config.SetDomain("tencentags.com")
}
