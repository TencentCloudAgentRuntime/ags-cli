//go:build preview

// Package proxy forwards a local address to the ACP entry of a harness.
package proxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	deploymentproxy "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/deployment/proxy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	dataplaneproxy "github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/proxy"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Runtime holds process seams replaced in tests.
type Runtime struct {
	NewProxy func(dataplaneproxy.Options) (deploymentproxy.Proxy, error)
	Wait     func(context.Context)
}

// Module returns the harness local proxy workflow.
func Module() command.Module {
	spec := command.Spec{
		ID:    "harness.proxy",
		Path:  []string{"harness", "proxy"},
		Use:   "proxy <harness-id>",
		Short: "Serve the harness ACP endpoint on a local address for testing",
		Long: "Start a local HTTP proxy to the harness Deployment. The proxy adds and refreshes the access token and keeps the affinity value, so browsers, IDE plugins and ACP SDKs can connect to http://127.0.0.1:<port>/acp without credentials.\n\n" +
			"The proxy runs until interrupted. With -o ndjson it prints one 'ready' event with the local URL, then stays running; agents can start it in the background and read the first line. Use --port 0 to pick a free port.\n\n" +
			"Intended for local testing. To connect a client directly, use 'agr harness endpoint'. Available in preview builds.",
		Examples: []string{
			"Example - Serve on port 8080:\n  agr harness proxy hns-a1b2c3d4",
			"Example - Pick a free port and read the URL from the first NDJSON line:\n  agr harness proxy hns-a1b2c3d4 --port 0 -o ndjson",
			"Example - Resume the instance of an earlier session:\n  agr harness proxy hns-a1b2c3d4 --affinity-id <affinity-id>",
		},
		SupportsJSON:   true,
		SupportsNDJSON: true,
		Args:           []command.ArgSpec{{Name: "harness-id", Required: true, Description: "Harness ID, such as hns-a1b2c3d4."}},
		Flags: []command.FlagSpec{
			{Name: "port", Usage: "Local port; 0 picks a free port", Type: command.FlagInt, Default: harnesscli.ACPPort, Workflow: true},
			harnesscli.StringFlag("address", "Local address to bind (default 127.0.0.1)"),
			harnesscli.StringFlag("affinity-id", "Initial affinity value, to reach the instance of an earlier session"),
			harnesscli.BoolFlag("verbose", "Log request paths; queries and tokens are omitted"),
		},
		Output: command.OutputSpec{DataType: "HarnessProxyEvent", Description: "Streaming ready and stopped events with the local ACP URL.", Effects: []string{"read:harness", "read:deployment-token"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{Spec: spec, Groups: harnesscli.GroupsOf(get.APIDescriptor(), 1), Source: command.SourceWorkflow},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			client, err := harnesscli.NewClient(deps, spec.ID)
			if err != nil {
				return command.Runtime{}, err
			}
			plane, ok := deps.ControlPlane.(deploymentproxy.ControlPlane)
			if !ok {
				return command.Runtime{}, fmt.Errorf("%s requires Deployment control-plane support", spec.ID)
			}
			runtime := runtimeOf(deps.DataPlane)
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return run(ctx, req, deps, client, plane, runtime)
			})}, nil
		},
	}
}

func runtimeOf(injected any) Runtime {
	runtime, _ := injected.(Runtime)
	if runtime.NewProxy == nil {
		runtime.NewProxy = func(options dataplaneproxy.Options) (deploymentproxy.Proxy, error) {
			return dataplaneproxy.New(options)
		}
	}
	if runtime.Wait == nil {
		runtime.Wait = func(ctx context.Context) {
			waitCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			<-waitCtx.Done()
		}
	}
	return runtime
}

func run(ctx context.Context, req command.Request, deps command.Deps, client harnesscli.Client, plane deploymentproxy.ControlPlane, runtime Runtime) (*command.Result, error) {
	port := req.Flags["port"].Int
	if !req.Flags["port"].Changed {
		port = harnesscli.ACPPort
	}
	if port < 0 || port > 65535 {
		return nil, output.NewUsageError("INVALID_PORT", fmt.Sprintf("--port must be between 0 and 65535, got %d", port), "Use --port 0 to pick a free port.")
	}
	address := harnesscli.String(req, "address")
	if address == "" {
		address = "127.0.0.1"
	}
	if err := cli.ValidateListenAddress(address); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	target, err := harnesscli.ResolveTarget(ctx, client, plane, req.ArgValues["harness-id"])
	if err != nil {
		return nil, err
	}
	affinityID := harnesscli.String(req, "affinity-id")
	if affinityID != "" && target.AffinityHeader == "" {
		return nil, output.NewUsageError("AFFINITY_DISABLED", fmt.Sprintf("the Deployment of %s does not enable affinity", target.HarnessID), "Remove --affinity-id.")
	}
	if address != "127.0.0.1" && address != "localhost" && address != "::1" {
		fmt.Fprintf(deps.IO.ErrOut, "Warning: binding to %s exposes an authenticated harness endpoint to the network.\n", address)
	}
	lifecycle, cancel := context.WithCancel(ctx)
	defer cancel()
	ndjson := cli.IsNDJSON()
	events := output.NewNDJSONWriter(deps.IO.Out, "harness.proxy")
	var affinity *dataplaneproxy.AffinityOptions
	if target.AffinityHeader != "" {
		affinity = &dataplaneproxy.AffinityOptions{HeaderName: target.AffinityHeader, InitialID: affinityID, OnIDChange: func(id string) {
			if ndjson {
				_ = events.WriteEvent("affinity", map[string]any{"AffinityId": id})
				return
			}
			fmt.Fprintf(deps.IO.ErrOut, "Affinity ID: %s\n", id)
		}}
	}
	proxy, err := runtime.NewProxy(dataplaneproxy.Options{
		Logger:          log.New(cli.DiagnosticWriter(deps.IO.ErrOut), "", log.LstdFlags),
		InstanceID:      target.DeploymentID,
		Domain:          target.Domain,
		RemotePort:      harnesscli.ACPPort,
		TokenProvider:   deploymentproxy.NewTokenSource(lifecycle, plane, target.DeploymentID, nil),
		RewriteOrigin:   true,
		PreserveHeaders: true,
		Affinity:        affinity,
		ListenAddress:   net.JoinHostPort(address, strconv.Itoa(port)),
		Verbose:         harnesscli.Bool(req, "verbose"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create harness proxy: %w", err)
	}
	actual, err := proxy.Start()
	if err != nil {
		return nil, output.NewUsageError("PROXY_BIND_FAILED", fmt.Sprintf("cannot listen on %s: %v", net.JoinHostPort(address, strconv.Itoa(port)), err), "Choose another port with --port, or --port 0 for a free one.")
	}
	localURL := "http://" + actual + harnesscli.ACPPath
	ready := map[string]any{"AcpURL": localURL, "RemoteAcpURL": target.URL(), "HarnessId": target.HarnessID, "DeploymentId": target.DeploymentID, "AffinityHeader": target.AffinityHeader}
	if ndjson {
		_ = events.WriteEvent("ready", ready)
	} else {
		fmt.Fprintf(deps.IO.Out, "ACP endpoint: %s\n", localURL)
		fmt.Fprintf(deps.IO.ErrOut, "Forwarding to %s (Deployment %s). Local testing only.\nPress Ctrl+C to stop.\n", target.URL(), target.DeploymentID)
	}
	runtime.Wait(ctx)
	cancel()
	proxy.Stop()
	if ndjson {
		_ = events.WriteCompleted(map[string]any{"AcpURL": localURL})
	} else {
		fmt.Fprintln(deps.IO.ErrOut, "Proxy stopped.")
	}
	return &command.Result{StreamDone: true}, nil
}
