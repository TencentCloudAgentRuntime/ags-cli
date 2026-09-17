package patchscenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// The correct contract is enforced even while the backend has a known count bug.
// Keep this separate so reports distinguish lifecycle success from count failure.
func runSessionEventCount(s *patchtest.Session) error {
	ctx := s.Context
	name := fmt.Sprintf("agr-count-e2e-%d", time.Now().UnixNano())
	space, err := sessionCall(s, ctx, "session-space.create", map[string]any{"Name": name})
	spaceID := sessionString(sessionObject(space.Data["SessionSpace"]), "SpaceId")
	if spaceID != "" {
		s.Cleanup(func(ctx context.Context) error {
			if _, err := sessionCall(s, ctx, "session-space.delete", map[string]any{"SpaceId": spaceID}); err != nil {
				return err
			}
			return sessionAbsent(s, ctx, "session-space.get", map[string]any{"SpaceId": spaceID})
		})
	}
	if err != nil {
		return err
	}
	if spaceID == "" {
		return fmt.Errorf("session count fixture missing SpaceId")
	}
	key := map[string]any{"SpaceId": spaceID, "UserId": "agr-count-user", "SessionId": name}
	s.Cleanup(func(ctx context.Context) error {
		if _, err := sessionCall(s, ctx, "session.delete", key); err != nil {
			return err
		}
		return sessionAbsent(s, ctx, "session.get", key)
	})
	if _, err := sessionCall(s, ctx, "session.create", key); err != nil {
		return err
	}
	for count := 0; count <= 2; count++ {
		if count > 0 {
			request := maps.Clone(key)
			request["Event"] = map[string]any{"EventId": fmt.Sprintf("%s-%d", name, count), "InvocationId": "count-check", "Author": "user", "Content": map[string]any{"Role": "user", "Parts": []any{map[string]any{"Text": "count-check"}}}}
			if _, err := sessionCall(s, ctx, "session.event.append", request); err != nil {
				return err
			}
		}
		got, err := sessionCall(s, ctx, "session.get", key)
		if err != nil {
			return err
		}
		listed, err := sessionCall(s, ctx, "session.list", map[string]any{"SpaceId": spaceID, "UserIds": []string{"agr-count-user"}, "SessionIds": []string{name}})
		if err != nil {
			return err
		}
		events, err := sessionCall(s, ctx, "session.event.list", key)
		if err != nil {
			return err
		}
		rows, _ := listed.Data["Sessions"].([]any)
		if len(rows) != 1 {
			return fmt.Errorf("session count fixture list mismatch")
		}
		single := sessionObject(got.Data["Session"])
		item := sessionObject(rows[0])
		eventRows, _ := events.Data["Events"].([]any)
		expected := json.Number(fmt.Sprint(count))
		if err := s.Assert("event-count.consistent", sessionString(single, "SessionId") == name && sessionString(item, "SessionId") == name && single["EventCount"] == expected && item["EventCount"] == expected && events.Data["TotalCount"] == expected && len(eventRows) == count); err != nil {
			return err
		}
	}
	return nil
}
