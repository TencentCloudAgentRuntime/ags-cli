package login

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/pty"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// ControlPlane supplies the instance lookup needed before opening an
// interactive login session.
type ControlPlane interface {
	GetInstance(ctx context.Context, instanceID string) (*ags.SandboxInstance, error)
}

// Session is the interactive PTY connection opened against a sandbox instance.
//
// Connect blocks until the remote shell exits. Its first return value is the
// remote shell's exit code (0 for a clean `exit`); its second return value is
// non-nil only when the data-plane session itself fails (transport, auth,
// envd RPC) and never just because the remote shell returned non-zero.
type Session interface {
	Connect(ctx context.Context, instanceID, user string) (int, error)
}

// RuntimeDeps contains data-plane and terminal dependencies that tests can
// replace without invoking a real PTY or access-token flow.
type RuntimeDeps struct {
	RequireTTY  func() error
	Interactive func() bool
	GetToken    func(ctx context.Context, instanceID string) (string, error)
	NewSession  func(accessToken, domain string) Session
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:    "instance.login",
		Path:  []string{"instance", "login"},
		Use:   "login <instance-id>",
		Short: "Login to instance via terminal",
		Long: `Login to a sandbox instance interactively using a native PTY session.

Connects a terminal session directly in your current console.

Examples:
  agr instance login ins-xxxx
  agr instance login ins-xxxx --user root`,
		Args:  []command.ArgSpec{{Name: "instance-id", Required: true}},
		Flags: []command.FlagSpec{{Name: "user", Usage: "User to run terminal as (default: \"user\")", Type: command.FlagString}},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{{
				Path:    []string{"instance"},
				Use:     "instance",
				Short:   "Manage sandbox instances",
				Long:    "Manage sandbox instances and related data-plane workflows.",
				Aliases: []string{"i"},
			}},
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp, ok := deps.ControlPlane.(ControlPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("instance.login requires command.Deps.ControlPlane implementing instance/login.ControlPlane")
			}
			rt := runtimeDeps(deps.DataPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return runLogin(ctx, req, cp, rt)
			})}, nil
		},
	}
}

func runtimeDeps(injected any) RuntimeDeps {
	rt, _ := injected.(RuntimeDeps)
	if rt.RequireTTY == nil {
		rt.RequireTTY = cli.RequireTTY
	}
	if rt.Interactive == nil {
		rt.Interactive = func() bool { return !cli.NonInteractive() }
	}
	if rt.GetToken == nil {
		rt.GetToken = cli.GetCachedTokenOrAcquire
	}
	if rt.NewSession == nil {
		rt.NewSession = func(accessToken, domain string) Session {
			return pty.NewSession(accessToken, domain)
		}
	}
	return rt
}

func runLogin(ctx context.Context, req command.Request, cp ControlPlane, rt RuntimeDeps) (*command.Result, error) {
	if err := rt.RequireTTY(); err != nil {
		return nil, err
	}
	if !rt.Interactive() {
		return nil, exitError(2, fmt.Errorf("instance login requires interactive mode"))
	}

	instanceID := req.ArgValues["instance-id"]
	if instanceID == "" && len(req.Args) > 0 {
		instanceID = req.Args[0]
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	instance, err := cp.GetInstance(ctx, instanceID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("instance %s not found. Please check the instance ID and try again", instanceID)
		}
		if strings.Contains(err.Error(), "permission") || strings.Contains(err.Error(), "access") {
			return nil, fmt.Errorf("access denied to instance %s. Please check your permissions", instanceID)
		}
		return nil, fmt.Errorf("failed to get instance %s: %w", instanceID, err)
	}
	if err := validateRunning(instanceID, instance); err != nil {
		return nil, err
	}

	var accessToken string
	if !isAuthModeNone(instance.AuthMode) {
		accessToken, err = rt.GetToken(ctx, instanceID)
		if err != nil {
			return nil, fmt.Errorf("failed to get access token: %w", err)
		}
	}
	cfg := config.Get()
	session := rt.NewSession(accessToken, cfg.DataPlaneRegionDomain())
	exitCode, err := session.Connect(ctx, instanceID, resolveUser(stringFlag(req, "user")))
	if err != nil {
		return nil, classifySessionError(err)
	}
	// A clean remote shell exit (typed `exit`, possibly inheriting 130 from a
	// Ctrl-C'd command) is not a CLI error: propagate the exit code as the
	// process exit status without rendering an error envelope, mirroring how
	// `ssh` reports the remote shell's status.
	return &command.Result{StreamDone: true, ExitCode: exitCode}, nil
}

func classifySessionError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		code := strings.ToUpper(strings.ReplaceAll(connectErr.Code().String(), " ", "_"))
		return output.NewCLIError(&output.Failure{
			Code:    "DATA_PLANE_" + code,
			Kind:    output.KindGenericError,
			Message: "data-plane PTY session failed: " + connectErr.Error(),
			Hint:    "This error came from the envd data-plane session. Rerun with --debug and share stderr diagnostics if it persists.",
		})
	}

	return output.NewCLIError(&output.Failure{
		Code:    "DATA_PLANE_SESSION_ERROR",
		Kind:    output.KindGenericError,
		Message: "data-plane PTY session failed: " + msg,
		Hint:    "Rerun with --debug and share stderr diagnostics if the problem persists.",
	})
}

func validateRunning(instanceID string, instance *ags.SandboxInstance) error {
	status := strings.ToUpper(derefString(instance.Status))
	if status == "RUNNING" {
		return nil
	}
	switch status {
	case "CREATING", "STARTING":
		return fmt.Errorf("instance %s is still being created. Please wait for it to finish and try again", instanceID)
	case "STOPPED", "STOPPING":
		return fmt.Errorf("instance %s is stopped. Please start it first using 'agr instance create' or contact support", instanceID)
	case "ERROR", "FAILED":
		return fmt.Errorf("instance %s is in error state. Please contact support or create a new instance", instanceID)
	default:
		return fmt.Errorf("instance %s is not running (status: %s). Please wait for it to be ready", instanceID, derefString(instance.Status))
	}
}

func exitError(code int, err error) error {
	msg := "command failed"
	if err != nil {
		msg = err.Error()
	}
	return &output.CLIError{
		Failure:  &output.Failure{Code: "CLI_ERROR", Kind: kindFromExitCode(code), Message: msg, Hint: "Run 'agr doctor' to diagnose configuration and environment issues."},
		ExitCode: code,
	}
}

func kindFromExitCode(code int) string {
	switch code {
	case output.ExitUsage:
		return output.KindUsage
	case output.ExitAuthOrPermission:
		return output.KindAuthOrPermission
	case output.ExitRemoteExecFailed:
		return output.KindRemoteExecFailed
	default:
		return output.KindGenericError
	}
}

func stringFlag(req command.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}

func resolveUser(flagValue string) string {
	return cli.ResolveUser(flagValue)
}

func isAuthModeNone(authMode *string) bool {
	return cli.IsAuthModeNone(authMode)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
