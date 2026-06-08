package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/adbtunnel"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/tunnelstore"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Tunnel is the foreground or daemon ADB tunnel process managed by this command.
type Tunnel interface {
	Start() (string, error)
	Probe() error
	Stop()
}

// RuntimeDeps contains token, config, tunnel construction, and signal-wait hooks
// that tests can replace without opening a real tunnel.
type RuntimeDeps struct {
	AcquireToken   func(ctx context.Context, instanceID string) (string, error)
	ValidateConfig func() error
	NewTunnel      func(adbtunnel.TunnelOptions) (Tunnel, error)
	Wait           func(context.Context)
	StopTimeout    time.Duration
}

type readyMessage struct {
	Status  string `json:"status"`
	Port    int    `json:"port,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Message string `json:"message,omitempty"`
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:     "instance.mobile.tunnel",
		Path:   []string{"instance", "mobile", "tunnel"},
		Use:    "tunnel <instance-id>",
		Short:  "Run ADB tunnel in foreground (used internally by connect)",
		Hidden: true,
		Args:   []command.ArgSpec{{Name: "instance-id", Required: true}},
		Flags:  []command.FlagSpec{{Name: "daemon", Usage: "Run in daemon mode (used by connect)", Type: command.FlagBool}, {Name: "port", Usage: "Local port to listen on (0 = auto-assign)", Type: command.FlagInt, Default: 0}},
		Output: command.OutputSpec{DataType: "MobileTunnel"},
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
				return runTunnel(ctx, req, deps, rt)
			})}, nil
		},
	}
}

func runtimeDeps(injected any) RuntimeDeps {
	rt, _ := injected.(RuntimeDeps)
	if rt.AcquireToken == nil {
		rt.AcquireToken = cli.AcquireInstanceToken
	}
	if rt.ValidateConfig == nil {
		rt.ValidateConfig = config.Validate
	}
	if rt.NewTunnel == nil {
		rt.NewTunnel = func(opts adbtunnel.TunnelOptions) (Tunnel, error) {
			return adbtunnel.New(opts)
		}
	}
	if rt.Wait == nil {
		rt.Wait = waitForSignal
	}
	if rt.StopTimeout == 0 {
		rt.StopTimeout = 5 * time.Second
	}
	return rt
}

func runTunnel(ctx context.Context, req command.Request, deps command.Deps, rt RuntimeDeps) (*command.Result, error) {
	instanceID := req.ArgValues["instance-id"]
	if instanceID == "" && len(req.Args) > 0 {
		instanceID = req.Args[0]
	}
	daemon := boolFlag(req, "daemon")
	port := intFlag(req, "port")
	if port < 0 || port > 65535 {
		return nil, exitError(output.ExitUsage, fmt.Errorf("--port must be between 0 and 65535"))
	}
	if err := rt.ValidateConfig(); err != nil {
		return nil, exitError(output.ExitGenericError, err)
	}

	cfg := config.Get()
	listenAddr := "127.0.0.1:0"
	if port > 0 {
		listenAddr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	tunnel, err := rt.NewTunnel(adbtunnel.TunnelOptions{
		InstanceID: instanceID,
		Domain:     cfg.DataPlaneRegionDomain(),
		TokenProvider: func() (string, error) {
			return rt.AcquireToken(ctx, instanceID)
		},
		ListenAddress: listenAddr,
		Insecure:      false,
		OnStateChange: makeStateChangeHandler(instanceID),
	})
	if err != nil {
		writeReadyError(deps.IO.Out, daemon, fmt.Sprintf("failed to create tunnel: %v", err))
		return nil, exitError(output.ExitGenericError, fmt.Errorf("failed to create tunnel: %w", err))
	}

	addr, err := tunnel.Start()
	if err != nil {
		writeReadyError(deps.IO.Out, daemon, fmt.Sprintf("failed to start tunnel: %v", err))
		return nil, exitError(output.ExitGenericError, fmt.Errorf("failed to start tunnel: %w", err))
	}

	if err := tunnel.Probe(); err != nil {
		tunnel.Stop()
		writeReadyError(deps.IO.Out, daemon, err.Error())
		return nil, exitError(output.ExitUsage, fmt.Errorf("upstream probe failed: %w", err))
	}

	_, portStr, _ := strings.Cut(addr, ":")
	if daemon {
		msg := readyMessage{Status: "ready", Port: mustAtoi(portStr), PID: os.Getpid()}
		if err := json.NewEncoder(deps.IO.Out).Encode(msg); err != nil {
			tunnel.Stop()
			return nil, exitError(output.ExitGenericError, fmt.Errorf("failed to write ready message: %w", err))
		}
	} else {
		fmt.Fprintf(deps.IO.Out, "[Ready] ADB Tunnel established at %s\n", addr)
		fmt.Fprintln(deps.IO.Out, "[Ready] Press Ctrl+C to disconnect.")
	}

	rt.Wait(ctx)

	if !daemon {
		fmt.Fprintln(deps.IO.Out, "\n[INFO] Shutting down ADB tunnel...")
	}

	shutdownDone := make(chan struct{})
	go func() {
		tunnel.Stop()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(rt.StopTimeout):
		if !daemon {
			fmt.Fprintln(deps.IO.Out, "[WARN] Graceful shutdown timed out. Forcing exit.")
		}
	}

	return &command.Result{StreamDone: true}, nil
}

func writeReadyError(w io.Writer, daemon bool, message string) {
	if !daemon {
		return
	}
	_ = json.NewEncoder(w).Encode(readyMessage{Status: "error", Message: message})
}

func waitForSignal(ctx context.Context) {
	waitCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-waitCtx.Done()
}

func boolFlag(req command.Request, name string) bool {
	flag, ok := req.Flags[name]
	return ok && flag.Bool
}

func intFlag(req command.Request, name string) int {
	flag, ok := req.Flags[name]
	if !ok {
		return 0
	}
	return flag.Int
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
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

// makeStateChangeHandler returns an OnStateChange callback that updates the
// tunnel's status in the persistent tunnel store. This allows `mobile list`
// to display the real health state without probing each tunnel.
func makeStateChangeHandler(instanceID string) func(adbtunnel.TunnelState) {
	return func(state adbtunnel.TunnelState) {
		store, err := tunnelstore.NewStore()
		if err != nil {
			log.Printf("[WARN] Failed to open tunnel store for status update: %v", err)
			return
		}
		var status string
		var degradedAt *time.Time
		switch state {
		case adbtunnel.StateDegraded:
			status = "unreachable"
			now := time.Now()
			degradedAt = &now
		default:
			status = "connected"
		}
		if err := store.UpdateStatus(instanceID, status, degradedAt); err != nil {
			log.Printf("[WARN] Failed to update tunnel status in store: %v", err)
		}
	}
}
