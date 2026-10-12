package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRawGlobalArgsRespectFlagOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, shadow, wantToken, wantRegion, wantID string
		args                                        []string
		localBool, parentFlag, wantDebug            bool
	}{
		{name: "local token", shadow: "token", args: []string{"resource", "read", "--token", "business", "--output", "json"}},
		{name: "inline local token", shadow: "token", args: []string{"resource", "read", "--token=business", "--output", "json"}},
		{name: "local token before command", shadow: "token", args: []string{"--token", "business", "resource", "read", "--output", "json"}},
		{name: "inline local token before command", shadow: "token", args: []string{"--token=business", "resource", "read", "--output", "json"}},
		{name: "consume local value", shadow: "token", args: []string{"resource", "read", "--token", "--debug", "--output", "json"}},
		{name: "local region", shadow: "region", args: []string{"resource", "read", "--region", "business", "--output", "json"}},
		{name: "local secret ID", shadow: "secret-id", args: []string{"resource", "read", "--secret-id=business", "--output", "json"}},
		{name: "parent persistent shadow", shadow: "token", parentFlag: true, args: []string{"resource", "read", "--token", "business", "--output", "json"}},
		{name: "local bool does not consume next flag", shadow: "debug", localBool: true, args: []string{"resource", "read", "--debug", "--output", "json"}},
		{name: "inline local bool", shadow: "debug", localBool: true, args: []string{"resource", "read", "--debug=true", "--output", "json"}},
		{name: "global token", wantToken: "cloud", args: []string{"resource", "read", "--token", "cloud", "--output", "json"}},
		{name: "global token before command", wantToken: "cloud", args: []string{"--token=cloud", "resource", "read", "--output", "json"}},
		{name: "global region", wantRegion: "cloud", args: []string{"resource", "read", "--region=cloud", "--output", "json"}},
		{name: "global secret ID", wantID: "cloud", args: []string{"resource", "read", "--secret-id", "cloud", "--output", "json"}},
		{name: "global bool", wantDebug: true, args: []string{"resource", "read", "--debug", "--output", "json"}},
		{name: "unknown command fallback", wantToken: "cloud", args: []string{"unknown", "--token", "cloud", "--output", "json"}},
		{name: "root command", wantToken: "cloud", args: []string{"--token=cloud", "--output", "json"}},
		{name: "terminator", args: []string{"resource", "read", "--output", "json", "--", "--token", "literal", "--debug"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousRoot := rootCmd
			oldToken, oldRegion, oldID, oldOutput := tokenFlag, region, secretID, outputFmt
			oldDebug, oldExplicit := debugFlag, debugFlagExplicit
			t.Cleanup(func() {
				rootCmd = previousRoot
				tokenFlag, region, secretID, outputFmt = oldToken, oldRegion, oldID, oldOutput
				debugFlag, debugFlagExplicit = oldDebug, oldExplicit
			})
			rootCmd = &cobra.Command{Use: "agr"}
			rootCmd.PersistentFlags().StringVar(&tokenFlag, "token", "", "")
			rootCmd.PersistentFlags().StringVar(&region, "region", "", "")
			rootCmd.PersistentFlags().StringVar(&secretID, "secret-id", "", "")
			rootCmd.PersistentFlags().StringVar(&outputFmt, "output", "text", "")
			rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "")
			debugFlagExplicit = false
			group := &cobra.Command{Use: "resource"}
			leaf := &cobra.Command{Use: "read", Args: cobra.ArbitraryArgs}
			rootCmd.AddCommand(group)
			group.AddCommand(leaf)
			flags := leaf.Flags()
			if tc.parentFlag {
				flags = group.PersistentFlags()
			}
			if tc.shadow != "" {
				if tc.localBool {
					flags.Bool(tc.shadow, false, "")
				} else {
					flags.String(tc.shadow, "", "")
				}
			}
			applyRawGlobalArgs(tc.args)
			if tokenFlag != tc.wantToken || region != tc.wantRegion || secretID != tc.wantID || debugFlag != tc.wantDebug || outputFmt != "json" {
				t.Fatalf("global state: token=%q region=%q ID=%q debug=%v output=%q", tokenFlag, region, secretID, debugFlag, outputFmt)
			}
		})
	}
}
