//go:build preview

package toolcopy

import "testing"

// A forked or debugged tool carries the preview configuration fields verbatim:
// no member is dropped, no default is filled in, and an explicit false or an
// empty keepalive override survives the projection.
func TestRequestCopiesSandboxConfiguration(t *testing.T) {
	source := map[string]any{
		"ToolName": "source-tool",
		"ToolType": "code-interpreter",
		"KeepaliveConfiguration": map[string]any{
			"KeepaliveMode":    "REQUEST",
			"KeepaliveTimeout": "600s",
		},
		"AutoPause": false,
		"AutoResume": true,
		"LogConfiguration": map[string]any{
			"CLSConfig":     map[string]any{"TopicId": "topic-123"},
			"ContentFormat": "auto",
		},
	}
	request, err := Request(source)
	if err != nil {
		t.Fatal(err)
	}
	configuration, ok := request["KeepaliveConfiguration"].(map[string]any)
	if !ok {
		t.Fatalf("KeepaliveConfiguration = %#v", request["KeepaliveConfiguration"])
	}
	if configuration["KeepaliveMode"] != "REQUEST" || configuration["KeepaliveTimeout"] != "600s" {
		t.Fatalf("keepalive members were rewritten: %#v", configuration)
	}
	if pause, ok := request["AutoPause"].(bool); !ok || pause {
		t.Fatalf("AutoPause = %#v, want an explicit false", request["AutoPause"])
	}
	if resume, ok := request["AutoResume"].(bool); !ok || !resume {
		t.Fatalf("AutoResume = %#v, want true", request["AutoResume"])
	}
	logConfiguration, ok := request["LogConfiguration"].(map[string]any)
	if !ok {
		t.Fatalf("LogConfiguration = %#v", request["LogConfiguration"])
	}
	if logConfiguration["ContentFormat"] != "auto" {
		t.Fatalf("ContentFormat = %#v", logConfiguration["ContentFormat"])
	}
	topic, ok := logConfiguration["CLSConfig"].(map[string]any)
	if !ok || topic["TopicId"] != "topic-123" {
		t.Fatalf("CLSConfig = %#v", logConfiguration["CLSConfig"])
	}
	if _, ok := request["ToolName"]; ok {
		t.Fatalf("source tool name was copied: %#v", request["ToolName"])
	}
}

func TestRequestPreservesEmptyKeepaliveOverrideAndOmitsMissingMembers(t *testing.T) {
	request, err := Request(map[string]any{
		"ToolType":               "code-interpreter",
		"KeepaliveConfiguration": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration, ok := request["KeepaliveConfiguration"].(map[string]any)
	if !ok || len(configuration) != 0 {
		t.Fatalf("an explicit empty override must stay an empty object: %#v", request["KeepaliveConfiguration"])
	}
	for _, member := range []string{"AutoPause", "AutoResume", "LogConfiguration"} {
		if _, ok := request[member]; ok {
			t.Fatalf("unsent member %s was fabricated: %#v", member, request[member])
		}
	}
}
