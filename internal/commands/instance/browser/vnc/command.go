package vnc

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// RuntimeDeps contains the token acquisition hook needed to construct browser
// access URLs.
type RuntimeDeps struct {
	AcquireToken func(ctx context.Context, instanceID string) (string, error)
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:    "instance.browser.vnc",
		Path:  []string{"instance", "browser", "vnc"},
		Use:   "vnc <instance-id>",
		Short: "Show VNC URL for browser instance",
		Long: `Show the VNC URL for accessing a browser sandbox instance.

Examples:
  agr instance browser vnc ins-xxxx
  agr instance browser vnc ins-xxxx --port 9000`,
		Args: []command.ArgSpec{
			{Name: "instance-id", Required: true},
		},
		Flags: []command.FlagSpec{
			{Name: "port", Shorthand: "p", Usage: "VNC service port", Type: command.FlagInt, Default: 9000},
		},
		SupportsJSON: true,
		Output:       command.OutputSpec{DataType: "BrowserURLs"},
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
				{Path: []string{"instance", "browser"}, Use: "browser", Short: "Browser sandbox commands"},
			},
			Source: "workflow",
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			rt := runtimeDeps(deps.DataPlane)
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					return runVNC(ctx, req, deps, rt)
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
	return rt
}

func runVNC(ctx context.Context, req command.Request, deps command.Deps, rt RuntimeDeps) (*command.Result, error) {
	instanceID := req.ArgValues["instance-id"]
	if instanceID == "" && len(req.Args) > 0 {
		instanceID = req.Args[0]
	}
	port := intFlag(req, "port")
	if port == 0 {
		port = 9000
	}
	if port <= 0 || port > 65535 {
		return nil, output.NewUsageError("INVALID_PORT", fmt.Sprintf("invalid port: %d", port), "Provide a port between 1 and 65535.")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	accessToken, err := rt.AcquireToken(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire access token: %w", err)
	}

	cfg := config.Get()
	vncURL := buildVNCURL(instanceID, cfg.Region, cfg.DataPlaneDomain(), accessToken, port)
	cdpURL := buildCDPURL(instanceID, cfg.Region, cfg.DataPlaneDomain(), accessToken, port)
	data := map[string]any{
		"InstanceId": instanceID,
		"VncUrl":     vncURL,
		"CdpUrl":     cdpURL,
	}
	return &command.Result{Data: data, Text: func(w io.Writer) {
		printKV(w, []keyValue{
			{key: "Instance ID", value: instanceID},
			{key: "VNC URL", value: vncURL},
			{key: "CDP URL", value: cdpURL},
		})
	}}, nil
}

func buildVNCURL(instanceID, region, domain, accessToken string, port int) string {
	host := fmt.Sprintf("%d-%s.%s.%s", port, instanceID, region, domain)
	return fmt.Sprintf("https://%s/novnc/vnc_lite.html?&path=websockify?access_token=%s", host, accessToken)
}

func buildCDPURL(instanceID, region, domain, accessToken string, port int) string {
	host := fmt.Sprintf("%d-%s.%s.%s", port, instanceID, region, domain)
	return fmt.Sprintf("https://%s/cdp?access_token=%s", host, accessToken)
}

type keyValue struct {
	key   string
	value string
}

func printKV(w io.Writer, pairs []keyValue) {
	maxLen := 0
	for _, kv := range pairs {
		if len(kv.key) > maxLen {
			maxLen = len(kv.key)
		}
	}
	for _, kv := range pairs {
		fmt.Fprintf(w, "%-*s  %s\n", maxLen, kv.key+":", kv.value)
	}
}

func intFlag(req command.Request, name string) int {
	flag, ok := req.Flags[name]
	if !ok {
		return 0
	}
	return flag.Int
}
