package patchscenarios

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// The same registered scenario runs against the real candidate CLI here and
// against the service in the strict patch runner. The local fixture is not live evidence.
func TestSessionLifecycleCandidate(t *testing.T) {
	root := filepath.Clean("../..")
	binary := filepath.Join(t.TempDir(), "agr-preview")
	build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-tags=preview", "-o", binary, "./cmd/agr")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, fault := range []string{"", "ignore-space-id", "ignore-name", "ignore-name-like", "ignore-description-like", "ignore-title", "ignore-title-like", "drop-update", "drop-event", "retain-resource", "ignore-session-filters", "ignore-session-offset", "ignore-session-limit", "ignore-space-offset", "ignore-space-limit", "ignore-event-author", "ignore-event-time", "ignore-event-offset", "ignore-event-limit", "drop-inline-data"} {
		t.Run("fault="+fault, func(t *testing.T) {
			fixture := &sessionFixture{t: t, fault: fault}
			server := httptest.NewTLSServer(fixture)
			defer server.Close()
			env := []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "TENCENTCLOUD_TOKEN=fake-session", "AGR_REGION=ap-guangzhou", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_INSECURE_SKIP_VERIFY=1"}
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: "session.lifecycle", Assertions: sessionAssertions}}}
			results, err := patchtest.Run(t.Context(), plan, Registry, binary, env)
			if (err != nil) != (fault != "") {
				t.Fatalf("fault %q: err=%v results=%+v", fault, err, results)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fault != "retain-resource" && (fixture.space != nil || fixture.session != nil || fixture.pageSession != nil || fixture.pageSpace != nil) {
				t.Fatal("owned resources leaked")
			}
			if fault == "" && (len(results) != 1 || results[0].Cleanup != "pass" || results[0].Calls < 20) {
				t.Fatalf("insufficient execution: %+v", results)
			}
		})
	}
	for _, fault := range []string{"", "zero-get-count", "zero-both-counts"} {
		t.Run("event-count="+fault, func(t *testing.T) {
			fixture := &sessionFixture{t: t, fault: fault}
			server := httptest.NewTLSServer(fixture)
			defer server.Close()
			env := []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "TENCENTCLOUD_TOKEN=fake-session", "AGR_REGION=ap-guangzhou", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_INSECURE_SKIP_VERIFY=1"}
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: "session.event-count", Assertions: []string{"event-count.consistent"}}}}
			results, err := patchtest.Run(t.Context(), plan, Registry, binary, env)
			if (err != nil) != (fault != "") {
				t.Fatalf("fault %q: err=%v results=%+v", fault, err, results)
			}
			if len(results) != 1 || results[0].Cleanup != "pass" {
				t.Fatalf("cleanup: %+v", results)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.space != nil || fixture.session != nil {
				t.Fatal("count fixture leaked")
			}
		})
	}
	t.Run("channel-isolation", func(t *testing.T) {
		stable := filepath.Join(t.TempDir(), "agr-stable")
		build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", stable, "./cmd/agr")
		build.Dir = root
		build.Env = append(os.Environ(), "GOFLAGS=")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build stable: %v\n%s", err, out)
		}
		for _, group := range []string{"session", "session-space"} {
			if out, err := exec.CommandContext(t.Context(), stable, group, "--help").CombinedOutput(); err == nil {
				t.Fatalf("stable exposes %s: %s", group, out)
			}
			if out, err := exec.CommandContext(t.Context(), binary, group, "--help").CombinedOutput(); err != nil {
				t.Fatalf("preview omits %s: %v %s", group, err, out)
			}
		}
		if out, err := exec.CommandContext(t.Context(), binary, "schema", "session.update-title", "-o", "json").CombinedOutput(); err == nil {
			t.Fatalf("unsupported dedicated title command: %s", out)
		}
		for _, args := range [][]string{{"session", "get", "--help"}, {"schema", "session.get", "-o", "json"}} {
			out, err := exec.CommandContext(t.Context(), binary, args...).CombinedOutput()
			if err != nil {
				t.Fatalf("get contract: %v %s", err, out)
			}
			for _, unsupported := range []string{"num-recent-events", "after-timestamp", "NumRecentEvents", "AfterTimestamp"} {
				if strings.Contains(string(out), unsupported) {
					t.Fatalf("unsupported get parameter %s exposed: %s", unsupported, out)
				}
			}
		}
		if out, err := exec.CommandContext(t.Context(), binary, "session", "create", "--help").CombinedOutput(); err != nil || strings.Contains(string(out), "--agent-id") {
			t.Fatalf("disabled AgentId exposed: %v %s", err, out)
		}
	})
	t.Run("update-title-flags", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			flags    []string
			title    string
			present  bool
			metadata bool
		}{
			{"omitted", []string{"--metadata", "[]"}, "", false, true},
			{"empty", []string{"--title", ""}, "", true, false},
			{"empty-with-metadata", []string{"--title", "", "--metadata", "[]"}, "", true, true},
			{"nonempty", []string{"--title", "updated"}, "updated", true, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				requests := make(chan map[string]any, 1)
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request map[string]any
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if r.Header.Get("X-TC-Action") != "ModifySession" {
						t.Error("unexpected action")
					}
					requests <- request
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"Response":{"RequestId":"fixture","Session":{}}}`))
				}))
				defer server.Close()
				args := append([]string{"session", "update", "--space-id", "space-fixture", "--user-id", "user-fixture", "--session-id", "session-fixture", "-o", "json"}, tc.flags...)
				cmd := exec.CommandContext(t.Context(), binary, args...)
				cmd.Env = []string{"HOME=" + t.TempDir(), "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_INSECURE_SKIP_VERIFY=1"}
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("update: %v %s", err, out)
				}
				select {
				case request := <-requests:
					value, present := request["Title"]
					if present != tc.present || (present && value != tc.title) {
						t.Fatalf("Title = %#v, present=%v; want %q, present=%v", value, present, tc.title, tc.present)
					}
					if tc.metadata {
						items, ok := request["Metadata"].([]any)
						if !ok || len(items) != 0 {
							t.Fatalf("Metadata = %#v, want []", request["Metadata"])
						}
					}
				default:
					t.Fatal("no request reached server")
				}
			})
		}
	})
	t.Run("event-input-transports", func(t *testing.T) {
		fixture := &sessionFixture{t: t, session: map[string]any{}}
		server := httptest.NewTLSServer(fixture)
		defer server.Close()
		payload := `{"EventId":"input-fixture","Author":"user","Content":{"Role":"user","Parts":[{"Text":"hello"}]}}`
		path := filepath.Join(t.TempDir(), "event.json")
		if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			input, stdin string
			valid        bool
		}{{payload, "", true}, {"@" + path, "", true}, {"-", payload, true}, {"@" + path + "-missing", "", false}, {"-", "", false}, {"{", "", false}, {`{"Content":{"Unknown":true}}`, "", false}} {
			fixture.mu.Lock()
			before := fixture.calls
			fixture.mu.Unlock()
			cmd := exec.CommandContext(t.Context(), binary, "session", "event", "append", "--space-id", "space-fixture", "--user-id", "user-fixture", "--session-id", "session-fixture", "--event", tc.input, "-o", "json")
			cmd.Stdin = strings.NewReader(tc.stdin)
			cmd.Env = []string{"HOME=" + t.TempDir(), "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "TENCENTCLOUD_TOKEN=fake-session", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_INSECURE_SKIP_VERIFY=1"}
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.valid {
				t.Fatalf("input %q: %v %s", tc.input, err, out)
			}
			fixture.mu.Lock()
			calls, event := fixture.calls, maps.Clone(fixture.event)
			fixture.mu.Unlock()
			if !tc.valid && calls != before {
				t.Fatal("invalid input reached server")
			}
			if tc.valid {
				var want map[string]any
				if err := json.Unmarshal([]byte(payload), &want); err != nil {
					t.Fatal(err)
				}
				delete(event, "Timestamp")
				if !reflect.DeepEqual(event, want) {
					t.Fatalf("input changed: %v", event)
				}
			}
		}
	})
}

type sessionFixture struct {
	t                     *testing.T
	mu                    sync.Mutex
	fault                 string
	calls                 int
	space, session, event map[string]any
	events                []any
	pageSession           map[string]any
	pageSpace             map[string]any
}

func (f *sessionFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Token") != "fake-session" {
		f.t.Error("missing signed temporary credentials")
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Error(err)
		return
	}
	response := map[string]any{"RequestId": "fixture"}
	fail := func() {
		response["Error"] = map[string]any{"Code": "ResourceNotFound.SessionNotExist", "Message": "missing fixture"}
	}
	switch action := r.Header.Get("X-TC-Action"); action {
	case "CreateSessionSpace":
		created := maps.Clone(req)
		created["SpaceId"], created["Status"], created["Default"] = "space-fixture", "ACTIVE", false
		if f.space == nil {
			f.space = created
		} else {
			created["SpaceId"] = "space-page"
			f.pageSpace = created
		}
		response["SessionSpace"] = created
	case "DescribeSessionSpace":
		if f.space != nil && req["SpaceId"] == f.space["SpaceId"] {
			response["SessionSpace"] = f.space
		} else if f.pageSpace != nil && req["SpaceId"] == f.pageSpace["SpaceId"] {
			response["SessionSpace"] = f.pageSpace
		} else {
			fail()
		}
	case "ModifySessionSpace":
		if f.space == nil {
			fail()
		} else {
			maps.Copy(f.space, req)
			response["SessionSpace"] = f.space
		}
	case "DescribeSessionSpaces":
		// Existing account resources must not invalidate pagination assertions.
		spaces := []any{map[string]any{"SpaceId": "preexisting-space", "Name": "preexisting"}}
		if f.space != nil {
			spaces = append(spaces, f.space)
		}
		if f.pageSpace != nil {
			spaces = append(spaces, f.pageSpace)
		}
		if filters, ok := req["Filters"].([]any); ok {
			var matched []any
			for _, item := range spaces {
				if fixtureFilters(sessionObject(item), filters, f.fault) {
					matched = append(matched, item)
				}
			}
			spaces = matched
		}
		response["TotalCount"] = len(spaces)
		if offset, ok := req["Offset"].(float64); ok && f.fault != "ignore-space-offset" {
			spaces = spaces[min(int(offset), len(spaces)):]
		}
		if limit, ok := req["Limit"].(float64); ok && limit > 0 && f.fault != "ignore-space-limit" {
			spaces = spaces[:min(int(limit), len(spaces))]
		}
		response["SessionSpaces"] = spaces
	case "DeleteSessionSpace":
		if f.pageSpace != nil && req["SpaceId"] == f.pageSpace["SpaceId"] {
			f.pageSpace = nil
			break
		}
		if f.fault != "retain-resource" {
			f.space = nil
		}
	case "CreateSession":
		created := maps.Clone(req)
		created["EventCount"] = 0
		if f.session == nil {
			f.session = created
		} else {
			f.pageSession = created
		}
		response["Session"] = created
	case "DescribeSession":
		if f.session != nil && req["SessionId"] == f.session["SessionId"] {
			response["Session"] = f.session
		} else if f.pageSession != nil && req["SessionId"] == f.pageSession["SessionId"] {
			response["Session"] = f.pageSession
		} else {
			fail()
		}
	case "ModifySession":
		if f.fault != "drop-update" {
			maps.Copy(f.session, req)
		}
		response["Session"] = f.session
	case "DescribeSessions":
		var sessions []any
		match := f.session != nil
		if f.fault != "ignore-session-filters" {
			for _, pair := range [][2]string{{"UserIds", "UserId"}, {"SessionIds", "SessionId"}} {
				if ids, ok := req[pair[0]].([]any); ok && len(ids) > 0 && ids[0] != f.session[pair[1]] {
					match = false
				}
			}
			if filters, ok := req["Filters"].([]any); ok && !fixtureFilters(f.session, filters, f.fault) {
				match = false
			}
		}
		if match {
			sessions = []any{f.session}
			if f.pageSession != nil {
				sessions = append(sessions, f.pageSession)
			}
		}
		response["TotalCount"] = len(sessions)
		if req["Offset"] == float64(1) && f.fault != "ignore-session-offset" {
			sessions = sessions[min(1, len(sessions)):]
		}
		if limit, ok := req["Limit"].(float64); ok && limit > 0 && f.fault != "ignore-session-limit" {
			sessions = sessions[:min(int(limit), len(sessions))]
		}
		response["Sessions"] = sessions
	case "AppendEvent":
		f.event = maps.Clone(sessionObject(req["Event"]))
		f.event["Timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
		// Model wire normalization instead of echoing the request verbatim.
		if parts, ok := sessionObject(f.event["Content"])["Parts"].([]any); ok {
			for _, raw := range parts {
				part := sessionObject(raw)
				if part["Thought"] == false {
					delete(part, "Thought")
				}
				for _, field := range []string{"FunctionCall", "FunctionResponse"} {
					if text, ok := part[field].(string); ok {
						var value any
						if err := json.Unmarshal([]byte(text), &value); err != nil {
							f.t.Error(err)
						}
						encoded, _ := json.MarshalIndent(value, "", "  ")
						part[field] = string(encoded)
					}
				}
				if f.fault == "drop-inline-data" {
					delete(part, "InlineData")
				}
			}
		}
		f.events = append(f.events, f.event)
		response["Event"] = f.event
		if f.fault == "drop-event" {
			delete(f.event, "Content")
		}
		if delta := sessionObject(f.event["Actions"])["StateDelta"]; delta != nil {
			f.session["State"] = map[string]any{"CustomState": delta}
		}
		f.session["EventCount"] = len(f.events)
	case "DescribeEvents":
		var events []any
		for _, raw := range f.events {
			event := sessionObject(raw)
			if author, ok := req["Author"].(string); ok && author != event["Author"] && f.fault != "ignore-event-author" {
				continue
			}
			if after, ok := req["AfterTimestamp"].(string); ok && f.fault != "ignore-event-time" {
				cutoff, _ := time.Parse(time.RFC3339Nano, after)
				stamp, _ := time.Parse(time.RFC3339Nano, sessionString(event, "Timestamp"))
				if !stamp.After(cutoff) {
					continue
				}
			}
			events = append(events, event)
		}
		response["TotalCount"] = len(events)
		if offset, ok := req["Offset"].(float64); ok && f.fault != "ignore-event-offset" {
			events = events[min(int(offset), len(events)):]
		}
		if limit, ok := req["Limit"].(float64); ok && limit > 0 && f.fault != "ignore-event-limit" {
			events = events[:min(int(limit), len(events))]
		}
		response["Events"] = events
	case "DeleteSession":
		if f.pageSession != nil && req["SessionId"] == f.pageSession["SessionId"] {
			f.pageSession = nil
			break
		}
		f.session = nil
		f.event = nil
		f.events = nil
	default:
		f.t.Errorf("unexpected action %s", action)
		fail()
	}
	if f.fault == "zero-get-count" || f.fault == "zero-both-counts" {
		if r.Header.Get("X-TC-Action") == "DescribeSession" && response["Session"] != nil {
			row := maps.Clone(sessionObject(response["Session"]))
			row["EventCount"] = 0
			response["Session"] = row
		}
		if f.fault == "zero-both-counts" && r.Header.Get("X-TC-Action") == "DescribeSessions" {
			if rows, ok := response["Sessions"].([]any); ok {
				for i, raw := range rows {
					row := maps.Clone(sessionObject(raw))
					row["EventCount"] = 0
					rows[i] = row
				}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"Response": response}); err != nil {
		f.t.Error(err)
	}
}

func TestSessionJSONEquals(t *testing.T) {
	if !sessionJSONEquals(`{"b":2,"a":1}`, `{"a":1,"b":2}`) || sessionJSONEquals(`{"a":2}`, `{"a":1}`) {
		t.Fatal("JSON readback comparison")
	}
}

func TestSessionEventValueEqual(t *testing.T) {
	for _, tc := range []struct {
		field            string
		actual, expected any
		want             bool
	}{
		{"Thought", nil, false, true},
		{"Thought", nil, true, false},
		{"FunctionCall", `{"Args":{}, "Name":"probe"}`, `{"Name":"probe","Args":{}}`, true},
		{"FunctionResponse", `{"Response":{"ok":false}}`, `{"Response":{"ok":true}}`, false},
		{"InlineData", nil, map[string]any{"Data": "aGVsbG8="}, false},
	} {
		if got := sessionEventValueEqual(tc.field, tc.actual, tc.expected); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.field, got, tc.want)
		}
	}
}

func fixtureFilters(object map[string]any, filters []any, fault string) bool {
	for _, raw := range filters {
		filter := sessionObject(raw)
		name := sessionString(filter, "Name")
		if fault == "ignore-"+name {
			continue
		}
		values, _ := filter["Values"].([]any)
		matched := false
		for _, rawValue := range values {
			value, _ := rawValue.(string)
			switch name {
			case "space-id":
				matched = matched || sessionString(object, "SpaceId") == value
			case "name":
				matched = matched || sessionString(object, "Name") == value
			case "name-like":
				matched = matched || strings.Contains(sessionString(object, "Name"), value)
			case "description-like":
				matched = matched || strings.Contains(sessionString(object, "Description"), value)
			case "title":
				matched = matched || sessionString(object, "Title") == value
			case "title-like":
				matched = matched || strings.Contains(sessionString(object, "Title"), value)
			default:
				matched = matched || sessionContains(object["Metadata"], "Value", value)
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
