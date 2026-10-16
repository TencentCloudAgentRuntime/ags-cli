package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestContract_NoGlobalFlagShadows(t *testing.T) {
	if err := validateGlobalFlagShadows(contractRoot()); err != nil {
		t.Fatal(err)
	}
}

func TestContract_GlobalFlagShadowGuardRejectsNewShadows(t *testing.T) {
	root := contractRoot()
	for _, tc := range []struct {
		name, flag, shorthand string
		persistent            bool
	}{
		{name: "local token", flag: "token"},
		{name: "inherited token", flag: "token", persistent: true},
		{name: "local region", flag: "region"},
		{name: "inherited secret ID", flag: "secret-id", persistent: true},
		{name: "local output shorthand", flag: "business-output", shorthand: "o"},
		{name: "inherited output shorthand", flag: "business-output", shorthand: "o", persistent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &cobra.Command{Use: "global-shadow-probe"}
			if tc.persistent {
				probe.PersistentFlags().StringP(tc.flag, tc.shorthand, "", "")
				probe.AddCommand(&cobra.Command{Use: "read"})
			} else {
				probe.Flags().StringP(tc.flag, tc.shorthand, "", "")
			}
			root.AddCommand(probe)
			t.Cleanup(func() { root.RemoveCommand(probe) })
			if err := validateGlobalFlagShadows(root); err == nil {
				t.Fatalf("new --%s shadow escaped the real command-tree guard", tc.flag)
			}
		})
	}
}

func TestContract_FlagShadowGuardChecksAllAncestors(t *testing.T) {
	for _, shorthand := range []bool{false, true} {
		t.Run(fmt.Sprintf("shorthand=%t", shorthand), func(t *testing.T) {
			root := &cobra.Command{Use: "agr"}
			parent := &cobra.Command{Use: "parent"}
			parent.PersistentFlags().StringP("parent-setting", "p", "", "")
			child := &cobra.Command{Use: "child"}
			if shorthand {
				child.Flags().StringP("child-setting", "p", "", "")
			} else {
				child.Flags().String("parent-setting", "", "")
			}
			root.AddCommand(parent)
			parent.AddCommand(child)
			if err := validateGlobalFlagShadows(root); err == nil {
				t.Fatal("flag shadow of a non-root ancestor escaped the guard")
			}
		})
	}
}

func validateGlobalFlagShadows(root *cobra.Command) error {
	if got := globalFlagShadows(root); len(got) != 0 {
		return fmt.Errorf("local flags shadow global flags: %v", got)
	}
	return nil
}

func globalFlagShadows(root *cobra.Command) []string {
	var shadows []string
	var visit func(*cobra.Command, map[string]*pflag.Flag)
	visit = func(cmd *cobra.Command, inherited map[string]*pflag.Flag) {
		inheritedShort := map[string]*pflag.Flag{}
		for _, flag := range inherited {
			if flag.Shorthand != "" {
				inheritedShort[flag.Shorthand] = flag
			}
		}
		seen := map[*pflag.Flag]bool{}
		check := func(local *pflag.Flag) {
			if seen[local] {
				return
			}
			seen[local] = true
			id := strings.TrimPrefix(strings.ReplaceAll(cmd.CommandPath(), " ", "."), root.Name()+".")
			if parent := inherited[local.Name]; parent != nil && local != parent {
				shadows = append(shadows, id+" --"+local.Name)
			}
			if parent := inheritedShort[local.Shorthand]; parent != nil && local != parent {
				shadows = append(shadows, fmt.Sprintf("%s -%s (--%s shadows --%s)", id, local.Shorthand, local.Name, parent.Name))
			}
		}
		// Inspect declarations before merging inherited flags: pflag panics on
		// conflicting shorthands during the merge, hiding the command context.
		cmd.Flags().VisitAll(check)
		cmd.PersistentFlags().VisitAll(check)
		next := maps.Clone(inherited)
		cmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) { next[flag.Name] = flag })
		for _, child := range cmd.Commands() {
			visit(child, next)
		}
	}
	visit(root, map[string]*pflag.Flag{})
	slices.Sort(shadows)
	return shadows
}
