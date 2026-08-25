package proxy

import (
	"context"
	"fmt"
	"net"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	dataplaneproxy "github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/proxy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

// ControlPlane supplies short-lived Deployment data-plane credentials.
type ControlPlane interface {
	GetDeploymentToken(context.Context, string) (*ags.AcquireDeploymentTokenResponseParams, error)
}

// Proxy is the local L7 proxy lifecycle managed by the command.
type Proxy interface {
	Start() (string, error)
	Stop()
}

// RuntimeDeps contains process and network seams used by focused tests.
type RuntimeDeps struct {
	NewProxy func(dataplaneproxy.Options) (Proxy, error)
	Wait     func(context.Context)
	Now      func() time.Time
}

// Module returns the Deployment local-debugging proxy command.
func Module() command.Module {
	spec := command.Spec{
		ID:    "deployment.proxy",
		Path:  []string{"deployment", "proxy"},
		Use:   "proxy <deployment-id> [local-port:]<remote-port>",
		Short: "Forward a Deployment HTTP port to a local address",
		Long: `Forward HTTP, SSE, and WebSocket traffic from a local address to a remotely managed Deployment.

This L7 proxy is recommended only for local debugging. It does not proxy raw TCP traffic.

Port syntax:
  <remote-port>                 Use the same local and remote port
  <local-port>:<remote-port>    Use a different local port`,
		Examples: []string{
			"Example - Forward the same local and remote port:\n  agr deployment proxy dpl-a1b2c3d4 8080",
			"Example - Avoid a local port conflict:\n  agr deployment proxy dpl-a1b2c3d4 3000:8080",
			"Example - Read an SSE endpoint through the proxy:\n  agr deployment proxy dpl-a1b2c3d4 3000:8080\n\n  # In another terminal:\n  curl -N http://127.0.0.1:3000/events",
			"Example - Log proxied requests without printing credentials:\n  agr deployment proxy dpl-a1b2c3d4 3000:8080 --verbose",
		},
		Args: []command.ArgSpec{
			{Name: "deployment-id", Required: true, Description: "Deployment ID."},
			{Name: "port", Required: true, Description: "[local-port:]remote-port mapping."},
		},
		Flags: []command.FlagSpec{
			{Name: "address", Usage: "Local address to bind to", Type: command.FlagString, Default: "127.0.0.1"},
			{Name: "verbose", Usage: "Enable secret-safe request logging", Type: command.FlagBool},
		},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{{
				Path: []string{"deployment"}, Use: "deployment", Short: "Manage deployments", Long: "Manage deployments and related data-plane workflows.",
			}},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			cp, ok := deps.ControlPlane.(ControlPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("deployment.proxy requires Deployment token support")
			}
			runtime := runtimeDeps(deps.DataPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return runProxy(ctx, req, deps, cp, runtime)
			})}, nil
		},
	}
}

func runtimeDeps(injected any) RuntimeDeps {
	runtime, _ := injected.(RuntimeDeps)
	if runtime.NewProxy == nil {
		runtime.NewProxy = func(options dataplaneproxy.Options) (Proxy, error) { return dataplaneproxy.New(options) }
	}
	if runtime.Wait == nil {
		runtime.Wait = waitForSignal
	}
	if runtime.Now == nil {
		runtime.Now = time.Now
	}
	return runtime
}

func runProxy(ctx context.Context, req command.Request, deps command.Deps, cp ControlPlane, runtime RuntimeDeps) (*command.Result, error) {
	deploymentID := positional(req, "deployment-id", 0)
	portSpec := positional(req, "port", 1)
	localPort, remotePort, err := parsePortSpec(portSpec)
	if err != nil {
		return nil, output.NewUsageError("INVALID_PORT", fmt.Sprintf("invalid port specification: %v", err), "Use [local-port:]remote-port with values between 1 and 65535.")
	}
	address := stringFlag(req, "address")
	if address == "" {
		address = "127.0.0.1"
	}
	if err := cli.ValidateListenAddress(address); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !isLoopbackAddress(address) {
		fmt.Fprintf(deps.IO.ErrOut, "Warning: binding to %s exposes the local debugging proxy to the network.\n", address)
	}

	tokenLifecycle, cancelTokens := context.WithCancel(ctx)
	defer cancelTokens()
	manager := newTokenManager(tokenLifecycle, func(tokenCtx context.Context) (*ags.AcquireDeploymentTokenResponseParams, error) {
		return cp.GetDeploymentToken(tokenCtx, deploymentID)
	}, runtime.Now)
	cfg := config.Get()
	domain := fmt.Sprintf("%s.agents.%s", cfg.Region, cfg.DataPlaneDomain())
	listenAddress := net.JoinHostPort(address, strconv.Itoa(localPort))
	proxy, err := runtime.NewProxy(dataplaneproxy.Options{
		InstanceID:      deploymentID,
		Domain:          domain,
		RemotePort:      remotePort,
		TokenProvider:   manager.Token,
		RewriteOrigin:   true,
		PreserveHeaders: true,
		ListenAddress:   listenAddress,
		Verbose:         boolFlag(req, "verbose"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment proxy: %w", err)
	}
	actualAddress, err := proxy.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start deployment proxy: %w", err)
	}

	fmt.Fprintln(deps.IO.Out, "Deployment proxy is recommended only for local debugging.")
	fmt.Fprintf(deps.IO.Out, "Forwarding from %s -> %d\n", actualAddress, remotePort)
	fmt.Fprintf(deps.IO.Out, "  Local:  http://%s\n", actualAddress)
	fmt.Fprintf(deps.IO.Out, "  Remote: https://%d-%s.%s\n", remotePort, deploymentID, domain)
	fmt.Fprintln(deps.IO.Out, "\nPress Ctrl+C to stop.")

	runtime.Wait(ctx)
	cancelTokens()
	fmt.Fprintln(deps.IO.Out, "\nStopping proxy...")
	proxy.Stop()
	return &command.Result{StreamDone: true}, nil
}

func parsePortSpec(spec string) (int, int, error) {
	parts := strings.Split(spec, ":")
	if len(parts) == 1 {
		port, err := validPort(parts[0], "port")
		return port, port, err
	}
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected [local-port:]remote-port")
	}
	local, err := validPort(parts[0], "local port")
	if err != nil {
		return 0, 0, err
	}
	remote, err := validPort(parts[1], "remote port")
	if err != nil {
		return 0, 0, err
	}
	return local, remote, nil
}

func validPort(value, label string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", label, value)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must be between 1 and 65535, got %d", label, port)
	}
	return port, nil
}

func waitForSignal(ctx context.Context) {
	waitCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-waitCtx.Done()
}

func positional(req command.Request, name string, index int) string {
	if value := req.ArgValues[name]; value != "" {
		return value
	}
	if index < len(req.Args) {
		return req.Args[index]
	}
	return ""
}

func stringFlag(req command.Request, name string) string {
	return req.Flags[name].String
}

func boolFlag(req command.Request, name string) bool {
	return req.Flags[name].Bool
}

func isLoopbackAddress(address string) bool {
	return address == "127.0.0.1" || address == "localhost" || address == "::1"
}
