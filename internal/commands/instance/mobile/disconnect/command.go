package disconnect

import (
	"context"
	"fmt"
	"io"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/mobileadb"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/tunnelstore"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

// Store is the tunnel registry dependency used to find and clean up mobile
// connections.
type Store interface {
	Get(string) (tunnelstore.TunnelEntry, bool, error)
	List() (map[string]tunnelstore.TunnelEntry, error)
	Cleanup(string) error
	CleanupAll() error
}

// RuntimeDeps contains store and adb hooks that tests can replace without
// touching the local adb daemon or tunnel registry.
type RuntimeDeps struct {
	NewStore   func() (Store, error)
	RequireADB func() (string, error)
	RunADB     func(adbPath string, args ...string) error
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:    "instance.mobile.disconnect",
		Path:  []string{"instance", "mobile", "disconnect"},
		Use:   "disconnect [instance-id]",
		Short: "Disconnect from mobile instance",
		Args:  []command.ArgSpec{{Name: "instance-id"}},
		Flags: []command.FlagSpec{{Name: "all", Usage: "Disconnect all active connections", Type: command.FlagBool}},
		Output: command.OutputSpec{
			DataType: "MobileDisconnectResult",
		},
		SupportsJSON: true,
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
				{Path: []string{"instance", "mobile"}, Use: "mobile", Short: "Mobile sandbox ADB commands", Long: `Manage ADB connections to mobile sandbox instances.

Examples:
  agr instance mobile connect <instance-id>
  agr instance mobile list
  agr instance mobile adb <instance-id> -- shell ls /sdcard
  agr instance mobile disconnect <instance-id>`},
			},
			Source: command.SourceWorkflow,
		},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			rt := runtimeDeps(deps.DataPlane)
			return command.Runtime{
				Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
					return runDisconnect(ctx, req, deps, rt)
				}),
			}, nil
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
	if rt.RunADB == nil {
		rt.RunADB = mobileadb.Run
	}
	return rt
}

func runDisconnect(_ context.Context, req command.Request, deps command.Deps, rt RuntimeDeps) (*command.Result, error) {
	all := boolFlag(req, "all")
	if all && len(req.Args) > 0 {
		return nil, fmt.Errorf("--all cannot be used with an instance-id")
	}
	store, err := rt.NewStore()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tunnel store: %w", err)
	}
	if all {
		return disconnectAll(store, deps, rt)
	}
	instanceID := req.ArgValues["instance-id"]
	if instanceID == "" && len(req.Args) > 0 {
		instanceID = req.Args[0]
	}
	if instanceID == "" {
		return nil, fmt.Errorf("must specify instance-id or use --all")
	}
	entry, ok, err := store.Get(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to read tunnel store: %w", err)
	}
	if !ok {
		return nil, noActiveTunnelError(instanceID, false)
	}
	if adbPath, err := rt.RequireADB(); err == nil {
		adbAddr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
		_ = rt.RunADB(adbPath, "disconnect", adbAddr)
	}
	if err := store.Cleanup(instanceID); err != nil {
		return nil, fmt.Errorf("failed to cleanup tunnel: %w", err)
	}
	data := map[string]any{"InstanceId": instanceID}
	return &command.Result{Data: data, Text: func(w io.Writer) {
		fmt.Fprintf(deps.IO.ErrOut, "disconnected from %s\n", instanceID)
	}}, nil
}

func disconnectAll(store Store, deps command.Deps, rt RuntimeDeps) (*command.Result, error) {
	entries, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list tunnels: %w", err)
	}
	if len(entries) == 0 {
		data := map[string]any{"Disconnected": []string{}, "Count": 0}
		return &command.Result{Data: data, Text: func(w io.Writer) {
			fmt.Fprintln(deps.IO.ErrOut, "no active connections")
		}}, nil
	}
	adbPath, _ := rt.RequireADB()
	var disconnected []string
	for id, entry := range entries {
		if adbPath != "" {
			adbAddr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
			_ = rt.RunADB(adbPath, "disconnect", adbAddr)
		}
		disconnected = append(disconnected, id)
	}
	if err := store.CleanupAll(); err != nil {
		return nil, fmt.Errorf("failed to cleanup tunnels: %w", err)
	}
	data := map[string]any{"Disconnected": disconnected, "Count": len(disconnected)}
	return &command.Result{Data: data, Text: func(w io.Writer) {
		for _, id := range disconnected {
			fmt.Fprintf(deps.IO.ErrOut, "disconnected from %s\n", id)
		}
	}}, nil
}

func noActiveTunnelError(instanceID string, includeHint bool) error {
	message := fmt.Sprintf("no active tunnel for %s", instanceID)
	hint := "Run 'agr instance mobile connect <instance-id>' to establish a local ADB tunnel first."
	if includeHint {
		message = fmt.Sprintf("no active tunnel for %s; run 'agr instance mobile connect %s' first", instanceID, instanceID)
	}
	return output.NewNotFoundError("NO_ACTIVE_TUNNEL", message, hint)
}

func boolFlag(req command.Request, name string) bool {
	flag, ok := req.Flags[name]
	return ok && flag.Bool
}
