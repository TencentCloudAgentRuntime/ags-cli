package sandboxconfig

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

func previewObject(t *testing.T, raw string) apivalue.Object {
	t.Helper()
	var object apivalue.Object
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func TestDetailsFollowsTheResponse(t *testing.T) {
	if apimeta.BuildChannel != apimeta.Preview {
		if Details(apivalue.Object{"AutoPause": false}, true) != nil {
			t.Fatal("stable build must not render keepalive details")
		}
		return
	}
	full := previewObject(t, `{
		"KeepaliveConfiguration":{"KeepaliveMode":"MIXED","KeepaliveTimeout":"600s"},
		"AutoPause":false,
		"AutoResume":true,
		"LogConfiguration":{"ContentFormat":"auto"}
	}`)
	got := Details(full, true)
	want := []Field{
		{Name: "KeepaliveMode", Value: "MIXED"},
		{Name: "KeepaliveTimeout", Value: "600s"},
		{Name: "AutoPause", Value: "false"},
		{Name: "AutoResume", Value: "true"},
		{Name: "LogConfiguration.ContentFormat", Value: "auto"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Details = %v, want %v", got, want)
	}
	if toolGot := Details(full, false); len(toolGot) != 4 {
		t.Fatalf("instance details must not include the tool log format: %v", toolGot)
	}

	partial := previewObject(t, `{"KeepaliveConfiguration":{"KeepaliveTimeout":"5m"},"AutoPause":false}`)
	got = Details(partial, false)
	want = []Field{
		{Name: "KeepaliveTimeout", Value: "5m"},
		{Name: "AutoPause", Value: "false"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Details = %v, want %v", got, want)
	}

	if got := Details(previewObject(t, `{}`), true); len(got) != 0 {
		t.Fatalf("missing configuration must render nothing: %v", got)
	}
	if got := Details(previewObject(t, `{"KeepaliveConfiguration":null,"AutoPause":null}`), false); len(got) != 0 {
		t.Fatalf("null configuration must render nothing: %v", got)
	}
}

func TestHeadersAndValuesStayAligned(t *testing.T) {
	if apimeta.BuildChannel != apimeta.Preview {
		if Headers(true) != nil || Values(apivalue.Object{}, true) != nil {
			t.Fatal("stable build must not add columns")
		}
		return
	}
	instanceHeaders := []string{"KEEPALIVE MODE", "KEEPALIVE TIMEOUT", "AUTO PAUSE", "AUTO RESUME"}
	if got := Headers(false); !slices.Equal(got, instanceHeaders) {
		t.Fatalf("instance headers = %v", got)
	}
	if got := Headers(true); !slices.Equal(got, append(slices.Clone(instanceHeaders), "LOG FORMAT")) {
		t.Fatalf("tool headers = %v", got)
	}

	object := previewObject(t, `{"KeepaliveConfiguration":{"KeepaliveMode":"NONE"},"AutoPause":false,"LogConfiguration":{"ContentFormat":"raw"}}`)
	got := Values(object, true)
	want := []string{"NONE", "-", "false", "-", "raw"}
	if !slices.Equal(got, want) {
		t.Fatalf("Values = %v, want %v", got, want)
	}
	if len(Values(previewObject(t, `{}`), false)) != len(instanceHeaders) {
		t.Fatal("a missing configuration must still render one cell per column")
	}
}
