package cli

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
		{"session.get", map[string]any{"Session": map[string]any{"SessionId": "session-test", "EventCount": json.Number("9007199254740993")}}, []string{"session-test", "9007199254740993"}},
		{"session.event.list", map[string]any{"Events": []any{map[string]any{"EventId": "event-test", "Content": map[string]any{"Role": "user"}}}, "TotalCount": 2}, []string{"event-test", `{"Role":"user"}`, "Showing 1 of 2", "--offset", "--limit"}},
		{"session.space.list", map[string]any{"TotalCount": 0}, []string{"SpaceId", "Name"}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			render, err := sessionTextRenderer(tc.id, tc.data)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			render(&out)
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q: %s", want, out.String())
				}
			}
		})
	}
}
