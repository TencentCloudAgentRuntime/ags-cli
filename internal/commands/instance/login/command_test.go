package login

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestModuleLogsIntoRunningInstance(t *testing.T) {
	setupConfig(t)
	status := "RUNNING"
	authMode := "TOKEN"
	cp := &fakeControlPlane{instance: &ags.SandboxInstance{Status: &status, AuthMode: &authMode}}
	session := &fakeSession{}
	runtime, err := Module().Build(command.Deps{
		ControlPlane: cp,
		DataPlane: RuntimeDeps{
			RequireTTY:  func() error { return nil },
			Interactive: func() bool { return true },
			GetToken:    func(context.Context, string) (string, error) { return "token", nil },
			NewSession: func(accessToken, domain string) Session {
				session.token, session.domain = accessToken, domain
				return session
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{
		Args:      []string{"ins-1"},
		ArgValues: map[string]string{"instance-id": "ins-1"},
		Flags:     map[string]command.FlagValue{"user": {Name: "user", Type: command.FlagString, String: "root"}},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.StreamDone || cp.instanceID != "ins-1" || session.instanceID != "ins-1" || session.user != "root" || session.token != "token" {
		t.Fatalf("result=%#v cp=%#v session=%#v", result, cp, session)
	}
}

func TestModuleSkipsTokenForAuthNone(t *testing.T) {
	setupConfig(t)
	status := "RUNNING"
	authMode := "NONE"
	tokenCalls := 0
	runtime, err := Module().Build(command.Deps{
		ControlPlane: &fakeControlPlane{instance: &ags.SandboxInstance{Status: &status, AuthMode: &authMode}},
		DataPlane: RuntimeDeps{
			RequireTTY:  func() error { return nil },
			Interactive: func() bool { return true },
			GetToken: func(context.Context, string) (string, error) {
				tokenCalls++
				return "token", nil
			},
			NewSession: func(string, string) Session { return &fakeSession{} },
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Args: []string{"ins-1"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if tokenCalls != 0 {
		t.Fatalf("tokenCalls=%d, want 0", tokenCalls)
	}
}

func TestModuleRejectsStoppedInstance(t *testing.T) {
	setupConfig(t)
	status := "STOPPED"
	runtime, err := Module().Build(command.Deps{
		ControlPlane: &fakeControlPlane{instance: &ags.SandboxInstance{Status: &status}},
		DataPlane: RuntimeDeps{
			RequireTTY:  func() error { return nil },
			Interactive: func() bool { return true },
			NewSession:  func(string, string) Session { return &fakeSession{} },
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Args: []string{"ins-1"}})
	if err == nil || !strings.Contains(err.Error(), "is stopped") {
		t.Fatalf("error=%v, want stopped error", err)
	}
}

func TestModuleClassifiesDataPlaneConnectError(t *testing.T) {
	setupConfig(t)
	status := "RUNNING"
	authMode := "TOKEN"
	runtime, err := Module().Build(command.Deps{
		ControlPlane: &fakeControlPlane{instance: &ags.SandboxInstance{Status: &status, AuthMode: &authMode}},
		DataPlane: RuntimeDeps{
			RequireTTY:  func() error { return nil },
			Interactive: func() bool { return true },
			GetToken:    func(context.Context, string) (string, error) { return "token", nil },
			NewSession: func(string, string) Session {
				return &fakeSession{err: connect.NewError(connect.CodeInternal, errors.New("envd closed PTY stream"))}
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	_, err = runtime.Handler.Run(context.Background(), command.Request{Args: []string{"ins-1"}})
	cliErr, ok := err.(*output.CLIError)
	if !ok {
		t.Fatalf("error=%T %v, want CLIError", err, err)
	}
	if cliErr.Failure.Code != "DATA_PLANE_INTERNAL" ||
		!strings.Contains(cliErr.Failure.Message, "envd closed PTY stream") ||
		!strings.Contains(cliErr.Failure.Hint, "envd data-plane") {
		t.Fatalf("failure=%#v", cliErr.Failure)
	}
}

func TestModuleClassifiesNonZeroPTYExit(t *testing.T) {
	setupConfig(t)
	status := "RUNNING"
	authMode := "TOKEN"
	runtime, err := Module().Build(command.Deps{
		ControlPlane: &fakeControlPlane{instance: &ags.SandboxInstance{Status: &status, AuthMode: &authMode}},
		DataPlane: RuntimeDeps{
			RequireTTY:  func() error { return nil },
			Interactive: func() bool { return true },
			GetToken:    func(context.Context, string) (string, error) { return "token", nil },
			NewSession: func(string, string) Session {
				return &fakeSession{exitCode: 130}
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Args: []string{"ins-1"}})
	if err != nil {
		t.Fatalf("Run returned error: %v, want nil", err)
	}
	if result == nil || !result.StreamDone || result.ExitCode != 130 {
		t.Fatalf("result=%#v, want StreamDone with ExitCode=130", result)
	}
}

func TestModulePropagatesCleanExit(t *testing.T) {
	setupConfig(t)
	status := "RUNNING"
	authMode := "TOKEN"
	runtime, err := Module().Build(command.Deps{
		ControlPlane: &fakeControlPlane{instance: &ags.SandboxInstance{Status: &status, AuthMode: &authMode}},
		DataPlane: RuntimeDeps{
			RequireTTY:  func() error { return nil },
			Interactive: func() bool { return true },
			GetToken:    func(context.Context, string) (string, error) { return "token", nil },
			NewSession:  func(string, string) Session { return &fakeSession{} },
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	result, err := runtime.Handler.Run(context.Background(), command.Request{Args: []string{"ins-1"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result == nil || !result.StreamDone || result.ExitCode != 0 {
		t.Fatalf("result=%#v, want StreamDone with ExitCode=0", result)
	}
}

func TestModuleRequiresControlPlane(t *testing.T) {
	_, err := Module().Build(command.Deps{})
	if err == nil || !strings.Contains(err.Error(), "ControlPlane") {
		t.Fatalf("error=%v, want missing control plane", err)
	}
}

type fakeControlPlane struct {
	instanceID string
	instance   *ags.SandboxInstance
}

func (f *fakeControlPlane) GetInstance(_ context.Context, instanceID string) (*ags.SandboxInstance, error) {
	f.instanceID = instanceID
	return f.instance, nil
}

type fakeSession struct {
	token      string
	domain     string
	instanceID string
	user       string
	exitCode   int
	err        error
}

func (f *fakeSession) Connect(_ context.Context, instanceID, user string) (int, error) {
	f.instanceID = instanceID
	f.user = user
	return f.exitCode, f.err
}

func setupConfig(t *testing.T) {
	t.Helper()
	if err := config.Init(); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	config.SetSecretID("AKIDfake")
	config.SetSecretKey("fakeSecretKey")
	config.SetRegion("ap-guangzhou")
}
