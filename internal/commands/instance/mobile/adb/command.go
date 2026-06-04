package adb

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/mobileadb"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/tunnelstore"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Store is the tunnel registry reader used to resolve an instance id to its
// local adb tunnel port.
type Store interface {
	Get(string) (tunnelstore.TunnelEntry, bool, error)
}

// RuntimeDeps contains store and adb execution hooks so tests can run without a
// real adb binary or tunnel registry.
type RuntimeDeps struct {
	NewStore        func() (Store, error)
	RequireADB      func() (string, error)
	RunADBBuffered  func(adbPath string, args ...string) (stdout string, stderr string, exitCode int, err error)
	RunADBStreaming func(adbPath string, args []string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error)
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:    "instance.mobile.adb",
		Path:  []string{"instance", "mobile", "adb"},
		Use:   "adb <instance-id> -- <adb-args...>",
		Short: "Execute adb command on mobile instance by ID",
		Long: `Execute an adb command targeting a specific mobile instance.

Use '--' to separate from adb arguments.

Examples:
  agr instance mobile adb ins-xxxx -- shell ls /sdcard
  agr instance mobile adb ins-xxxx -- install app.apk
  agr instance mobile adb ins-xxxx -- logcat`,
		Args: []command.ArgSpec{{Name: "args", Required: true, Repeatable: true}},
		Output: command.OutputSpec{
			DataType: "MobileADBResult",
		},
		SupportsJSON: true,
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{Path: []string{"instance"}, Use: "instance", Short: "Manage sandbox instances", Long: "Manage sandbox instances and related data-plane workflows.", Aliases: []string{"i"}},
				{Path: []string{"instance", "mobile"}, Use: "mobile", Short: "Mobile sandbox ADB commands", Long: `Manage ADB connections to mobile sandbox instances.

Examples:
  agr instance mobile connect <instance-id>
  agr instance mobile list
  agr instance mobile adb <instance-id> -- shell ls /sdcard
  agr instance mobile disconnect <instance-id>`},
			},
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			rt := runtimeDeps(deps.DataPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return runADB(ctx, req, deps, rt)
			})}, nil
		},
	}
}

func runtimeDeps(injected any) RuntimeDeps {
	rt, _ := injected.(RuntimeDeps)
	if rt.NewStore == nil {
		rt.NewStore = func() (Store, error) { return tunnelstore.NewStore() }
	}
	if rt.RequireADB == nil {
		rt.RequireADB = mobileadb.Require
	}
	if rt.RunADBBuffered == nil {
		rt.RunADBBuffered = mobileadb.RunBuffered
	}
	if rt.RunADBStreaming == nil {
		rt.RunADBStreaming = mobileadb.RunStreaming
	}
	return rt
}

func runADB(_ context.Context, req command.Request, deps command.Deps, rt RuntimeDeps) (*command.Result, error) {
	if req.DashPos != 1 || len(req.Args) <= 1 {
		return nil, output.NewUsageError("MISSING_SEPARATOR",
			"usage: agr instance mobile adb <instance-id> -- <adb-args...>\nUse '--' immediately after <instance-id> to separate the adb command.",
			"Use: agr instance mobile adb <instance-id> -- <adb-args...>")
	}

	instanceID := req.Args[0]
	adbArgs := req.Args[1:]

	adbPath, err := rt.RequireADB()
	if err != nil {
		return nil, err
	}

	store, err := rt.NewStore()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tunnel store: %w", err)
	}
	entry, ok, err := store.Get(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to read tunnel store: %w", err)
	}
	if !ok {
		return nil, noActiveTunnelError(instanceID, true)
	}

	adbAddr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
	fullArgs := append([]string{"-s", adbAddr}, adbArgs...)

	if cli.IsJSON() {
		stdout, stderr, exitCode, err := rt.RunADBBuffered(adbPath, fullArgs...)
		if err != nil {
			return nil, err
		}
		data := map[string]any{"Stdout": stdout, "Stderr": stderr, "ExitCode": exitCode}
		return &command.Result{Data: data, ExitCode: exitCode}, nil
	}

	exitCode, err := rt.RunADBStreaming(adbPath, fullArgs, deps.IO.In, deps.IO.Out, deps.IO.ErrOut)
	if err != nil {
		return nil, err
	}
	return &command.Result{StreamDone: true, ExitCode: exitCode}, nil
}

func noActiveTunnelError(instanceID string, includeHint bool) error {
	message := fmt.Sprintf("no active tunnel for %s", instanceID)
	hint := "Run 'agr instance mobile connect <instance-id>' to establish a local ADB tunnel first."
	if includeHint {
		message = fmt.Sprintf("no active tunnel for %s; run 'agr instance mobile connect %s' first", instanceID, instanceID)
	}
	return output.NewNotFoundError("NO_ACTIVE_TUNNEL", message, hint)
}
