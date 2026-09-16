package patchscenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

var sessionAssertions = []string{
	"space.create", "space.get", "space.list", "space.update", "space.delete",
	"session.create", "session.get", "session.list", "session.update", "session.delete",
	"event.append", "event.list", "event.filters", "event.pagination", "session.filters", "session.pagination", "space.pagination", "input.validation",
}

type sessionEnvelope struct {
	Status  string
	Data    map[string]any
	Failure *struct{ Code, Kind string }
}

func sessionCall(s *patchtest.Session, ctx context.Context, command string, request map[string]any) (sessionEnvelope, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return sessionEnvelope{}, err
	}
	args := append(strings.Split(command, "."), "--request", string(payload), "-o", "json")
	raw, callErr := s.CLI(ctx, args...)
	var out sessionEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return out, fmt.Errorf("%s: invalid CLI envelope", command)
	}
	if callErr != nil || out.Status != "succeeded" || out.Failure != nil {
		return out, fmt.Errorf("%s: CLI call failed", command)
	}
	return out, nil
}

func sessionObject(value any) map[string]any { object, _ := value.(map[string]any); return object }
func sessionString(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func sessionContains(value any, key, want string) bool {
	items, _ := value.([]any)
	for _, item := range items {
		if sessionString(sessionObject(item), key) == want {
			return true
		}
	}
	return false
}

func runSessionLifecycle(s *patchtest.Session) error {
	ctx := s.Context
	for _, invalid := range []struct {
		command string
		request map[string]any
	}{
		{"session-space.get", map[string]any{}},
		{"session-space.list", map[string]any{"Filters": []any{}}},
		{"session.event.append", map[string]any{"SpaceId": "space-validation", "UserId": "user-validation", "SessionId": "session-validation", "Event": map[string]any{"Timestamp": "2026-09-01T00:00:00Z"}}},
		{"session.list", map[string]any{"SpaceId": "space-validation", "Limit": "invalid"}},
		{"session.get", map[string]any{"SpaceId": "space-validation", "UserId": "user-validation", "SessionId": "session-validation", "NumRecentEvents": 1}},
		{"session.get", map[string]any{"SpaceId": "space-validation", "UserId": "user-validation", "SessionId": "session-validation", "AfterTimestamp": "2026-09-01T00:00:00Z"}},
		{"session.get", map[string]any{"SpaceId": "space-validation", "UserId": "user-validation", "SessionId": "session-validation", "AgentId": "disabled"}},
	} {
		result, err := sessionCall(s, ctx, invalid.command, invalid.request)
		if err := s.Assert("input.validation", err != nil && result.Failure != nil && result.Failure.Kind == "usage"); err != nil {
			return err
		}
	}
	name := fmt.Sprintf("agr-session-e2e-%d", time.Now().UnixNano())
	// Only ordinary session state is used: user:-scoped state outlives a session
	// and has no public cleanup API.
	space, err := sessionCall(s, ctx, "session-space.create", map[string]any{"Name": name, "Description": "CLI preview lifecycle", "Tags": []any{map[string]any{"Key": "agr-e2e", "Value": "session"}}})
	spaceID := sessionString(sessionObject(space.Data["SessionSpace"]), "SpaceId")
	spaceDeleted := false
	if spaceID != "" {
		s.Cleanup(func(ctx context.Context) error {
			if !spaceDeleted {
				if _, err := sessionCall(s, ctx, "session-space.delete", map[string]any{"SpaceId": spaceID}); err != nil {
					return err
				}
			}
			if err := sessionAbsent(s, ctx, "session-space.get", map[string]any{"SpaceId": spaceID}); err != nil {
				return err
			}
			return nil
		})
	}
	if err != nil {
		return err
	}
	if err := s.Assert("space.create", spaceID != "" && sessionString(sessionObject(space.Data["SessionSpace"]), "Name") == name); err != nil {
		return err
	}
	get, err := sessionCall(s, ctx, "session-space.get", map[string]any{"SpaceId": spaceID})
	if err != nil {
		return err
	}
	if err := s.Assert("space.get", sessionString(sessionObject(get.Data["SessionSpace"]), "SpaceId") == spaceID); err != nil {
		return err
	}
	_, err = sessionCall(s, ctx, "session-space.update", map[string]any{"SpaceId": spaceID, "Name": name + "-updated", "Description": "updated"})
	if err != nil {
		return err
	}
	get, err = sessionCall(s, ctx, "session-space.get", map[string]any{"SpaceId": spaceID})
	if err != nil {
		return err
	}
	if err := s.Assert("space.update", sessionString(sessionObject(get.Data["SessionSpace"]), "Name") == name+"-updated" && sessionString(sessionObject(get.Data["SessionSpace"]), "Description") == "updated"); err != nil {
		return err
	}
	var list sessionEnvelope

	other, err := sessionCall(s, ctx, "session-space.create", map[string]any{"Name": name + "-page"})
	otherID := sessionString(sessionObject(other.Data["SessionSpace"]), "SpaceId")
	otherDeleted := false
	if otherID != "" {
		s.Cleanup(func(ctx context.Context) error {
			if !otherDeleted {
				if _, err := sessionCall(s, ctx, "session-space.delete", map[string]any{"SpaceId": otherID}); err != nil {
					return err
				}
			}
			return sessionAbsent(s, ctx, "session-space.get", map[string]any{"SpaceId": otherID})
		})
	}
	if err != nil {
		return err
	}
	if err := s.Assert("space.pagination", otherID != "" && otherID != spaceID); err != nil {
		return err
	}
	spaceIDs := map[string]bool{}
	var page sessionEnvelope
	var items []any
	total := int64(-1)
	for offset := 0; total < 0 || int64(offset) < total; offset++ {
		page, err = sessionCall(s, ctx, "session-space.list", map[string]any{"Offset": offset, "Limit": 1})
		if err != nil {
			return err
		}
		count, ok := page.Data["TotalCount"].(json.Number)
		if !ok {
			return errors.New("space list missing TotalCount")
		}
		currentTotal, parseErr := count.Int64()
		if total < 0 {
			total = currentTotal
		}
		items, _ = page.Data["SessionSpaces"].([]any)
		if err := s.Assert("space.pagination", parseErr == nil && total >= 2 && currentTotal == total && len(items) == 1); err != nil {
			return err
		}
		id := sessionString(sessionObject(items[0]), "SpaceId")
		if err := s.Assert("space.pagination", id != "" && !spaceIDs[id]); err != nil {
			return err
		}
		spaceIDs[id] = true
	}
	if err := s.Assert("space.list", spaceIDs[spaceID] && spaceIDs[otherID]); err != nil {
		return err
	}

	if _, err := sessionCall(s, ctx, "session-space.delete", map[string]any{"SpaceId": otherID}); err != nil {
		return err
	}
	otherDeleted = true
	if err := sessionAbsent(s, ctx, "session-space.get", map[string]any{"SpaceId": otherID}); err != nil {
		return err
	}

	key := map[string]any{"SpaceId": spaceID, "UserId": "agr-e2e-user", "SessionId": name}
	sessionDeleted := false
	s.Cleanup(func(ctx context.Context) error {
		if !sessionDeleted {
			if _, err := sessionCall(s, ctx, "session.delete", key); err != nil {
				return err
			}
		}
		return sessionAbsent(s, ctx, "session.get", key)
	})
	request := maps.Clone(key)
	request["Title"] = "original"
	request["State"] = map[string]any{"CustomState": `{"counter":1}`}
	request["Metadata"] = []any{map[string]any{"Name": "env", "Value": "test"}}
	created, err := sessionCall(s, ctx, "session.create", request)
	if err != nil {
		return err
	}
	if err := s.Assert("session.create", sessionString(sessionObject(created.Data["Session"]), "SessionId") == name); err != nil {
		return err
	}
	get, err = sessionCall(s, ctx, "session.get", key)
	if err != nil {
		return err
	}
	current := sessionObject(get.Data["Session"])
	if err := s.Assert("session.get", sessionString(current, "Title") == "original" && sessionString(current, "UserId") == key["UserId"] && sessionString(current, "SpaceId") == spaceID && sessionJSONEquals(sessionObject(current["State"])["CustomState"], `{"counter":1}`) && sessionContains(current["Metadata"], "Value", "test")); err != nil {
		return err
	}
	request = maps.Clone(key)
	request["Title"] = "updated"
	request["Metadata"] = []any{map[string]any{"Name": "env", "Value": "verified"}}
	if _, err := sessionCall(s, ctx, "session.update", request); err != nil {
		return err
	}
	get, err = sessionCall(s, ctx, "session.get", key)
	if err != nil {
		return err
	}
	current = sessionObject(get.Data["Session"])
	if err := s.Assert("session.update", sessionString(current, "Title") == "updated" && sessionContains(current["Metadata"], "Value", "verified")); err != nil {
		return err
	}
	list, err = sessionCall(s, ctx, "session.list", map[string]any{"SpaceId": spaceID, "UserIds": []string{"agr-e2e-user"}, "SessionIds": []string{name}, "Offset": 0, "Limit": 10, "Filters": []any{map[string]any{"Name": "metadata:env", "Values": []string{"verified"}}}})
	if err != nil {
		return err
	}
	if err := s.Assert("session.list", sessionContains(list.Data["Sessions"], "SessionId", name) && list.Data["TotalCount"] == json.Number("1")); err != nil {
		return err
	}

	for _, filter := range []map[string]any{
		{"UserIds": []string{"absent-user"}},
		{"SessionIds": []string{"absent-session"}},
		{"Filters": []any{map[string]any{"Name": "metadata:env", "Values": []string{"absent"}}}},
	} {
		filter["SpaceId"] = spaceID
		result, err := sessionCall(s, ctx, "session.list", filter)
		if err != nil {
			return err
		}
		items, _ := result.Data["Sessions"].([]any)
		if err := s.Assert("session.filters", len(items) == 0 && result.Data["TotalCount"] == json.Number("0")); err != nil {
			return err
		}
	}
	pageKey := maps.Clone(key)
	pageKey["SessionId"] = name + "-page"
	pageDeleted := false
	s.Cleanup(func(ctx context.Context) error {
		if !pageDeleted {
			if _, err := sessionCall(s, ctx, "session.delete", pageKey); err != nil {
				return err
			}
		}
		return sessionAbsent(s, ctx, "session.get", pageKey)
	})
	if _, err := sessionCall(s, ctx, "session.create", pageKey); err != nil {
		return err
	}
	pageIDs := map[string]bool{}
	for offset := range 2 {
		page, err = sessionCall(s, ctx, "session.list", map[string]any{"SpaceId": spaceID, "SessionIds": []string{name, name + "-page"}, "Offset": offset, "Limit": 1})
		if err != nil {
			return err
		}
		items, _ = page.Data["Sessions"].([]any)
		if err := s.Assert("session.pagination", len(items) == 1 && page.Data["TotalCount"] == json.Number("2")); err != nil {
			return err
		}
		id := sessionString(sessionObject(items[0]), "SessionId")
		if err := s.Assert("session.pagination", !pageIDs[id] && (id == name || id == name+"-page")); err != nil {
			return err
		}
		pageIDs[id] = true
	}
	if _, err := sessionCall(s, ctx, "session.delete", pageKey); err != nil {
		return err
	}
	pageDeleted = true
	if err := sessionAbsent(s, ctx, "session.get", pageKey); err != nil {
		return err
	}

	event := map[string]any{"EventId": name + "-event", "InvocationId": "invocation-e2e", "Author": "user", "Content": map[string]any{"Role": "user", "Parts": []any{map[string]any{"Text": "hello", "Thought": false, "FunctionCall": `{"Name":"probe","Args":{"query":"hello"}}`, "FunctionResponse": `{"Name":"probe","Response":{"ok":true}}`, "InlineData": map[string]any{"MimeType": "text/plain", "Data": "aGVsbG8="}}}}, "Actions": map[string]any{"StateDelta": `{"counter":2}`}, "Metadata": `{"source":"cli"}`, "Extensions": `{"trace":"e2e"}`, "ErrorCode": "", "ErrorMessage": ""}
	request = maps.Clone(key)
	request["Event"] = event
	appended, err := sessionCall(s, ctx, "session.event.append", request)
	if err != nil {
		return err
	}
	if err := s.Assert("event.append", sessionString(sessionObject(appended.Data["Event"]), "EventId") == event["EventId"]); err != nil {
		return err
	}
	firstTime, err := time.Parse(time.RFC3339Nano, sessionString(sessionObject(appended.Data["Event"]), "Timestamp"))
	if err := s.Assert("event.append", err == nil && !firstTime.IsZero()); err != nil {
		return err
	}
	event["Timestamp"] = firstTime.Format(time.RFC3339Nano)
	second := maps.Clone(key)
	second["Event"] = map[string]any{"EventId": name + "-second", "InvocationId": "invocation-second", "Author": "model", "Content": map[string]any{"Role": "model", "Parts": []any{map[string]any{"Text": "reply"}}}}
	secondResult, err := sessionCall(s, ctx, "session.event.append", second)
	if err != nil {
		return err
	}
	secondTime, err := time.Parse(time.RFC3339Nano, sessionString(sessionObject(secondResult.Data["Event"]), "Timestamp"))
	if err := s.Assert("event.append", err == nil && !secondTime.IsZero()); err != nil {
		return err
	}
	latest := firstTime
	if secondTime.After(latest) {
		latest = secondTime
	}
	for _, filter := range []map[string]any{{"Author": "absent"}, {"AfterTimestamp": latest.Add(time.Second).Format(time.RFC3339Nano)}} {
		query := maps.Clone(key)
		maps.Copy(query, filter)
		result, err := sessionCall(s, ctx, "session.event.list", query)
		if err != nil {
			return err
		}
		items, _ := result.Data["Events"].([]any)
		if err := s.Assert("event.filters", len(items) == 0 && result.Data["TotalCount"] == json.Number("0")); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for offset := range 2 {
		query := maps.Clone(key)
		query["Offset"], query["Limit"] = offset, 1
		result, err := sessionCall(s, ctx, "session.event.list", query)
		if err != nil {
			return err
		}
		items, _ := result.Data["Events"].([]any)
		if err := s.Assert("event.pagination", len(items) == 1 && result.Data["TotalCount"] == json.Number("2")); err != nil {
			return err
		}
		id := sessionString(sessionObject(items[0]), "EventId")
		if err := s.Assert("event.pagination", !seen[id] && (id == name+"-event" || id == name+"-second")); err != nil {
			return err
		}
		seen[id] = true
	}
	request = maps.Clone(key)
	request["Offset"], request["Limit"], request["Author"], request["AfterTimestamp"] = 0, 10, "user", firstTime.Add(-time.Second).Format(time.RFC3339Nano)
	list, err = sessionCall(s, ctx, "session.event.list", request)
	if err != nil {
		return err
	}
	events, _ := list.Data["Events"].([]any)
	if err := s.Assert("event.list", len(events) == 1 && list.Data["TotalCount"] == json.Number("1")); err != nil {
		return err
	}
	actual := sessionObject(events[0])
	for field, value := range event {
		if field == "Timestamp" {
			got, parseErr := time.Parse(time.RFC3339Nano, sessionString(actual, field))
			want, _ := time.Parse(time.RFC3339Nano, value.(string))
			if err := s.Assert("event.list", parseErr == nil && got.Equal(want)); err != nil {
				return err
			}
			continue
		}
		if value == "" && actual[field] == nil {
			continue
		}
		if err := s.Assert("event.list", sessionEventValueEqual(field, actual[field], value)); err != nil {
			return err
		}
	}
	get, err = sessionCall(s, ctx, "session.get", key)
	if err != nil {
		return err
	}
	if err := s.Assert("event.append", sessionJSONEquals(sessionObject(sessionObject(get.Data["Session"])["State"])["CustomState"], `{"counter":2}`)); err != nil {
		return err
	}
	bad := maps.Clone(key)
	bad["Event"] = map[string]any{"UnknownPreviewField": true}
	failed, callErr := sessionCall(s, ctx, "session.event.append", bad)
	if err := s.Assert("input.validation", callErr != nil && failed.Failure != nil && failed.Failure.Code == "INVALID_REQUEST_JSON"); err != nil {
		return err
	}
	if _, err := sessionCall(s, ctx, "session.delete", key); err != nil {
		return err
	}
	sessionDeleted = true
	if err := sessionAbsent(s, ctx, "session.get", key); err != nil {
		return err
	}
	if err := s.Assert("session.delete", true); err != nil {
		return err
	}
	if _, err := sessionCall(s, ctx, "session-space.delete", map[string]any{"SpaceId": spaceID}); err != nil {
		return err
	}
	spaceDeleted = true
	if err := sessionAbsent(s, ctx, "session-space.get", map[string]any{"SpaceId": spaceID}); err != nil {
		return err
	}
	// The cleanup callbacks independently confirm both resources are absent.
	return s.Assert("space.delete", true)
}

func sessionJSONEquals(value any, expected string) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	var left, right any
	return json.Unmarshal([]byte(text), &left) == nil && json.Unmarshal([]byte(expected), &right) == nil && reflect.DeepEqual(left, right)
}

func sessionAbsent(s *patchtest.Session, ctx context.Context, command string, key map[string]any) error {
	result, err := sessionCall(s, ctx, command, key)
	if err == nil || result.Failure == nil || result.Failure.Kind != "not_found" {
		return errors.New("resource absence not confirmed")
	}
	return nil
}

// sessionEventValueEqual accepts JSON text normalization and omitted false Thought.
// Other missing fields remain failures, including InlineData if the service drops it.
func sessionEventValueEqual(field string, actual, expected any) bool {
	switch field {
	case "FunctionCall", "FunctionResponse", "Metadata", "Extensions", "StateDelta":
		want, ok := expected.(string)
		return ok && sessionJSONEquals(actual, want)
	case "Thought":
		if expected == false && actual == nil {
			return true
		}
	}
	if want, ok := expected.(map[string]any); ok {
		got, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range want {
			if !sessionEventValueEqual(key, got[key], value) {
				return false
			}
		}
		return true
	}
	if want, ok := expected.([]any); ok {
		got, ok := actual.([]any)
		if !ok || len(got) != len(want) {
			return false
		}
		for i := range want {
			if !sessionEventValueEqual("", got[i], want[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(actual, expected)
}
