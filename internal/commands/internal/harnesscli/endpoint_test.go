//go:build preview

package harnesscli

import "testing"

func TestHarnessEntryURLs(t *testing.T) {
	target := Target{DeploymentID: "dpl-test", Domain: "ap-chongqing.agents.tencentags.com"}
	if got, want := target.WebURL(), "https://3080-dpl-test-webview.ap-chongqing.agents.tencentags.com/"; got != want {
		t.Errorf("DSH browser entry = %q, want %q", got, want)
	}
	if got, want := target.URL(), "https://8080-dpl-test.ap-chongqing.agents.tencentags.com/acp"; got != want {
		t.Errorf("ACP entry changed = %q, want %q", got, want)
	}
}
