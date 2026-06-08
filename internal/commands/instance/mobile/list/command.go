package list

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/mobileadb"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/dataplane/tunnelstore"
)

// Store is the tunnel registry reader used to list active mobile connections.
type Store interface {
	List() (map[string]tunnelstore.TunnelEntry, error)
	Cleanup(sandboxID string) error
}

// RuntimeDeps contains the tunnel store factory so tests can provide in-memory
// tunnel state.
type RuntimeDeps struct {
	NewStore   func() (Store, error)
	RequireADB func() (string, error)
	RunADB     func(adbPath string, args ...string) error
}

// Module returns this package's command module.
func Module() command.Module {
	spec := command.Spec{
		ID:           "instance.mobile.list",
		Path:         []string{"instance", "mobile", "list"},
		Use:          "list",
		Short:        "List active mobile instance connections",
		SupportsJSON: true,
		Flags:        []command.FlagSpec{{Name: "prune", Usage: "Remove unreachable connections (kill tunnel, adb disconnect)", Type: command.FlagBool}},
		Output:       command.OutputSpec{DataType: "MobileConnectionList"},
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
				prune := false
				if flag, ok := req.Flags["prune"]; ok {
					prune = flag.Bool
				}
				return runList(ctx, deps, rt, prune)
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
	if rt.RunADB == nil {
		rt.RunADB = mobileadb.Run
	}
	return rt
}

// entryStatus returns the display status for a tunnel entry.
// Empty Status field (from older tunnel daemons) defaults to "connected".
func entryStatus(entry tunnelstore.TunnelEntry) string {
	if entry.Status == "" {
		return "connected"
	}
	return entry.Status
}

func runList(_ context.Context, deps command.Deps, rt RuntimeDeps, prune bool) (*command.Result, error) {
	store, err := rt.NewStore()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tunnel store: %w", err)
	}
	entries, err := store.List()
	if err != nil {
		var recovered *tunnelstore.CorruptStoreRecoveredError
		if errors.As(err, &recovered) {
			data := map[string]any{"Items": []map[string]any{}, "Total": 0}
			return &command.Result{
				Data:     data,
				Warnings: []string{recovered.Error()},
				Text: func(w io.Writer) {
					fmt.Fprintln(deps.IO.ErrOut, "No active connections")
					fmt.Fprintf(deps.IO.ErrOut, "Warning: %s\n", recovered.Error())
				},
			}, nil
		}
		return nil, fmt.Errorf("failed to list tunnels: %w", err)
	}

	// --prune: clean up unreachable entries
	if prune {
		pruneUnreachable(deps, rt, store, entries)
		// Re-read entries after pruning
		entries, err = store.List()
		if err != nil {
			return nil, fmt.Errorf("failed to re-read tunnel store after prune: %w", err)
		}
	}

	items := make([]map[string]any, 0, len(entries))
	for id, entry := range entries {
		addr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
		status := entryStatus(entry)
		items = append(items, map[string]any{
			"InstanceId": id,
			"AdbAddress": addr,
			"Port":       entry.Port,
			"Pid":        entry.PID,
			"CreatedAt":  entry.CreatedAt.Format(time.RFC3339),
			"Status":     status,
		})
	}
	data := map[string]any{"Items": items, "Total": len(items)}
	return &command.Result{Data: data, Text: func(w io.Writer) {
		if len(entries) == 0 {
			fmt.Fprintln(deps.IO.ErrOut, "No active connections.")
			fmt.Fprintln(deps.IO.ErrOut, "Use 'agr instance mobile connect <instance-id>' to connect.")
			return
		}
		headers := []string{"INSTANCE", "ADB ADDRESS", "STATUS"}
		rows := make([][]string, 0, len(entries))
		for id, entry := range entries {
			addr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
			rows = append(rows, []string{id, addr, entryStatus(entry)})
		}
		printTable(w, headers, rows)
	}}, nil
}

// pruneUnreachable removes entries marked as "unreachable" by killing the tunnel
// process, disconnecting ADB, and removing the store entry.
func pruneUnreachable(deps command.Deps, rt RuntimeDeps, store Store, entries map[string]tunnelstore.TunnelEntry) {
	adbPath, _ := rt.RequireADB()
	for id, entry := range entries {
		if entryStatus(entry) != "unreachable" {
			continue
		}
		// Disconnect ADB first (best-effort)
		if adbPath != "" {
			addr := fmt.Sprintf("127.0.0.1:%d", entry.Port)
			_ = rt.RunADB(adbPath, "disconnect", addr)
		}
		// Kill tunnel process and remove store entry
		if err := store.Cleanup(id); err != nil {
			fmt.Fprintf(deps.IO.ErrOut, "Warning: failed to cleanup %s: %v\n", id, err)
		} else {
			fmt.Fprintf(deps.IO.ErrOut, "Pruned unreachable tunnel: %s\n", id)
		}
	}
}

func printTable(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}
