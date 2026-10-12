package session

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionTextPreservesIDsAndPagination(t *testing.T) {
	for _, tc := range []struct {
		id   string
		data any
		want []string
	}{
		{"session.space.create", struct {
			Space map[string]any `json:"SessionSpace"`
		}{map[string]any{"SpaceId": "space-test", "Default": false}}, []string{"SpaceId:", "space-test", "Default:", "false"}},
		{"session.get", map[string]any{"Session": map[string]any{"SessionId": "session-test", "EventCount": json.Number("9007199254740993"), "CreateTime": "2026-10-10T00:00:00Z", "UpdateTime": "2026-10-12T00:00:00Z", "Metadata": []any{map[string]any{"Name": "ags.tencentcloud.com/agent-id", "Value": "agent-test"}}}}, []string{"session-test", "9007199254740993", "CreateTime:", "2026-10-10T00:00:00Z", "UpdateTime:", "2026-10-12T00:00:00Z", "agent-test"}},
		{"session.event.list", map[string]any{"Events": []any{map[string]any{"EventId": "event-test", "Content": map[string]any{"Role": "user"}}}, "TotalCount": 2}, []string{"event-test", `{"Role":"user"}`, "Showing 1 of 2", "--offset", "--limit"}},
		{"session.space.list", map[string]any{"TotalCount": 0}, []string{"SpaceId", "Name"}},
		{"session.delete", map[string]any{"RequestId": "deleted"}, []string{"Deleted session."}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			render, err := sessionTextRenderer(tc.id, tc.data)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			render(&out)
			if strings.Contains(out.String(), "and its events") {
				t.Fatal("text output asserts undocumented event deletion")
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q: %s", want, out.String())
				}
			}
		})
	}
}
