package proxy

import (
	"context"
	"fmt"
	"net"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	dataplaneproxy "github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/proxy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Proxy is the local port-forwarding process managed by the command.
type Proxy interface {
	Start() (string, error)
	Stop()
}

// RuntimeDeps contains token, proxy construction, and wait hooks that tests can
// replace without opening real network listeners.
type RuntimeDeps struct {
	AcquireToken func(ctx context.Context, instanceID string) (string, error)
	NewProxy     func(dataplaneproxy.Options) (Proxy, error)
	Wait         func(context.Context)
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:    "instance.proxy",
		Path:  []string{"instance", "proxy"},
		Use:   "proxy <instance-id> [local_port:]<remote_port>",
		Short: "Forward an instance port to localhost",
		Long: `Forward a remote instance port to a local address, similar to kubectl port-forward.

Port Syntax:
  <remote_port>                Forward remote port to the same local port
  <local_port>:<remote_port>   Forward remote port to a specific local port

Examples:
  agr instance proxy ins-xxxx 8080
  agr instance proxy ins-xxxx 3000:8080
  agr instance proxy ins-xxxx 3000:8080 --address 0.0.0.0`,
		Args: []command.ArgSpec{
			{Name: "instance-id", Required: true},
			{Name: "port", Required: true},
		},
		Flags: []command.FlagSpec{
			{Name: "address", Usage: "Local address to bind to", Type: command.FlagString, Default: "127.0.0.1"},
			{Name: "verbose", Usage: "Enable verbose request logging", Type: command.FlagBool},
		},
	}
	return command.Module{
		Descriptor: command.Descriptor{
			Spec: spec,
			Groups: []command.GroupSpec{
				{
					Path:    []string{"instance"},
					Use:     "instance",
					Short:   "Manage sandbox instances",
					Long:    "Manage sandbox instances and related data-plane workflows.",
					Aliases: []string{"i"},
				},
			},
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			rt := runtimeDeps(deps.DataPlane)
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					return runProxy(ctx, req, deps, rt)
				}),
			}, nil
		},
	}
}

func runtimeDeps(injected any) RuntimeDeps {
	rt, _ := injected.(RuntimeDeps)
	if rt.AcquireToken == nil {
		rt.AcquireToken = cli.AcquireInstanceToken
	}
	if rt.NewProxy == nil {
		rt.NewProxy = func(opts dataplaneproxy.Options) (Proxy, error) {
			return dataplaneproxy.New(opts)
		}
	}
	if rt.Wait == nil {
		rt.Wait = waitForSignal
	}
	return rt
}

func runProxy(ctx context.Context, req command.Request, deps command.Deps, rt RuntimeDeps) (*command.Result, error) {
	instanceID := req.ArgValues["instance-id"]
	portSpec := req.ArgValues["port"]
	if instanceID == "" && len(req.Args) > 0 {
		instanceID = req.Args[0]
	}
	if portSpec == "" && len(req.Args) > 1 {
		portSpec = req.Args[1]
	}

	localPort, remotePort, err := parsePortSpec(portSpec)
	if err != nil {
		return nil, output.NewUsageError("INVALID_PORT", fmt.Sprintf("invalid port specification: %v", err), "Use [local_port:]remote_port with values between 1 and 65535.")
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
	if address != "127.0.0.1" && address != "localhost" && address != "::1" {
		fmt.Fprintf(deps.IO.ErrOut, "Warning: binding to %s exposes the proxy (and the sandbox access token) to the network.\n", address)
	}
	token, err := rt.AcquireToken(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire access token: %w", err)
	}
	cfg := config.Get()
	domain := cfg.DataPlaneRegionDomain()
	listenAddr := net.JoinHostPort(address, strconv.Itoa(localPort))

	proxy, err := rt.NewProxy(dataplaneproxy.Options{
		InstanceID:    instanceID,
		Domain:        domain,
		RemotePort:    remotePort,
		Token:         token,
		ListenAddress: listenAddr,
		Insecure:      false,
		Verbose:       boolFlag(req, "verbose"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy: %w", err)
	}
	addr, err := proxy.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start proxy: %w", err)
	}

	fmt.Fprintf(deps.IO.Out, "Forwarding from %s -> %d\n", addr, remotePort)
	fmt.Fprintf(deps.IO.Out, "  Local:  http://%s\n", addr)
	fmt.Fprintf(deps.IO.Out, "  Remote: https://%d-%s.%s\n", remotePort, instanceID, domain)
	fmt.Fprintln(deps.IO.Out, "\nPress Ctrl+C to stop.")

	rt.Wait(ctx)

	fmt.Fprintln(deps.IO.Out, "\nStopping proxy...")
	proxy.Stop()
	return &command.Result{StreamDone: true}, nil
}

func parsePortSpec(spec string) (int, int, error) {
	parts := strings.SplitN(spec, ":", 2)

	if len(parts) == 1 {
		port, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid port: %q", parts[0])
		}
		if port <= 0 || port > 65535 {
			return 0, 0, fmt.Errorf("port must be between 1 and 65535, got %d", port)
		}
		return port, port, nil
	}

	localPort, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid local port: %q", parts[0])
	}
	if localPort <= 0 || localPort > 65535 {
		return 0, 0, fmt.Errorf("local port must be between 1 and 65535, got %d", localPort)
	}

	remotePort, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid remote port: %q", parts[1])
	}
	if remotePort <= 0 || remotePort > 65535 {
		return 0, 0, fmt.Errorf("remote port must be between 1 and 65535, got %d", remotePort)
	}

	return localPort, remotePort, nil
}

func waitForSignal(ctx context.Context) {
	waitCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-waitCtx.Done()
}

func stringFlag(req command.Request, name string) string {
	flag, ok := req.Flags[name]
	if !ok {
		return ""
	}
	return flag.String
}

func boolFlag(req command.Request, name string) bool {
	flag, ok := req.Flags[name]
	return ok && flag.Bool
}
