package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestContract_GlobalFlagShadowsAreReviewed(t *testing.T) {
	if err := validateGlobalFlagShadows(contractRoot()); err != nil {
		t.Fatal(err)
	}
}

func TestContract_GlobalFlagShadowGuardRejectsNewShadows(t *testing.T) {
	root := contractRoot()
	for _, tc := range []struct {
		name, flag string
		persistent bool
	}{
		{name: "local region", flag: "region"},
		{name: "inherited secret ID", flag: "secret-id", persistent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &cobra.Command{Use: "global-shadow-probe"}
			if tc.persistent {
				probe.PersistentFlags().String(tc.flag, "", "")
				probe.AddCommand(&cobra.Command{Use: "read"})
			} else {
				probe.Flags().String(tc.flag, "", "")
			}
			root.AddCommand(probe)
			t.Cleanup(func() { root.RemoveCommand(probe) })
			if err := validateGlobalFlagShadows(root); err == nil {
				t.Fatalf("new --%s shadow escaped the real command-tree guard", tc.flag)
			}
		})
	}
}

func validateGlobalFlagShadows(root *cobra.Command) error {
	// These existing business JWT flags intentionally shadow the cloud STS flag.
	// Any additional shadow requires an explicit routing/authentication decision.
	want := []string{
		"credential.oauth2.acquire --token",
		"credential.secret.get --token",
	}
	got := globalFlagShadows(root)
	if !slices.Equal(got, want) {
		return fmt.Errorf("global flag shadows = %v; reviewed exceptions = %v", got, want)
	}
	return nil
}

func globalFlagShadows(root *cobra.Command) []string {
	var shadows []string
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		// Resolve inherited persistent flags exactly as Cobra does. Inheritance
		// of the root flag itself is not a shadow: compare the flag objects.
		cmd.InheritedFlags()
		root.PersistentFlags().VisitAll(func(global *pflag.Flag) {
			local := cmd.Flags().Lookup(global.Name)
			if local != nil && local != global {
				id := strings.TrimPrefix(strings.ReplaceAll(cmd.CommandPath(), " ", "."), root.Name()+".")
				shadows = append(shadows, id+" --"+global.Name)
			}
		})
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
	slices.Sort(shadows)
	return shadows
}
