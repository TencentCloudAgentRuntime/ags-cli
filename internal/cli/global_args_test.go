package cli

import "testing"

func TestRawGlobalArgs(t *testing.T) {
	for _, tc := range []struct {
		name, wantToken, wantRegion, wantID string
		args                                []string
		wantDebug                           bool
	}{
		{name: "workload token", args: []string{"resource", "read", "--workload-identity-token", "business", "--output", "json"}},
		{name: "both tokens", wantToken: "cloud", args: []string{"--token=cloud", "resource", "read", "--workload-identity-token=business", "--output", "json"}},
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
			oldToken, oldRegion, oldID, oldOutput := tokenFlag, region, secretID, outputFmt
			oldDebug, oldExplicit := debugFlag, debugFlagExplicit
			t.Cleanup(func() {
				tokenFlag, region, secretID, outputFmt = oldToken, oldRegion, oldID, oldOutput
				debugFlag, debugFlagExplicit = oldDebug, oldExplicit
			})
			tokenFlag, region, secretID, outputFmt = "", "", "", "text"
			debugFlag, debugFlagExplicit = false, false
			applyRawGlobalArgs(tc.args)
			if tokenFlag != tc.wantToken || region != tc.wantRegion || secretID != tc.wantID || debugFlag != tc.wantDebug || outputFmt != "json" {
				t.Fatalf("global state: token=%q region=%q ID=%q debug=%v output=%q", tokenFlag, region, secretID, debugFlag, outputFmt)
			}
		})
	}
}
