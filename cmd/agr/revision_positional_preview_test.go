//go:build preview

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestRevisionNumberUsage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Revision uint64 }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Revision != 1 && req.Revision != math.MaxUint64) {
			t.Errorf("unexpected request for %s: revision=%d, decode error=%v", r.Header.Get("X-TC-Action"), req.Revision, err)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"Response": map[string]any{
			"HarnessRevision":       map[string]any{"Revision": req.Revision, "Status": "PUBLISHED"},
			"ModelProviderRevision": map[string]any{"Revision": req.Revision},
		}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	for _, cmd := range []struct{ group, verb, id, idField, resultField string }{
		{"harness", "get", "hns-test", "HarnessId", "HarnessRevision"},
		{"harness", "publish", "hns-test", "HarnessId", "HarnessRevision"},
		{"model-provider", "get", "mp-test", "ModelProviderId", "ModelProviderRevision"},
	} {
		for _, mode := range []string{"positional", "merged request", "request only"} {
			for _, tc := range []struct{ name, arg, raw, code string }{
				{"text", "abc", `"abc"`, "INVALID_POSITIONAL_ARG"},
				{"fraction", "1.5", "1.5", "INVALID_POSITIONAL_ARG"},
				{"zero", "0", "0", "INVALID_REVISION"},
				{"negative", "-1", "-1", "INVALID_REVISION"},
				{"valid", "1", "1", ""},
			} {
				t.Run(cmd.group+"/"+cmd.verb+"/"+mode+"/"+tc.name, func(t *testing.T) {
					args := []string{cmd.group, "revision", cmd.verb}
					wantCode := tc.code
					switch mode {
					case "positional":
						args = append(args, "--", cmd.id, tc.arg)
					case "merged request":
						args = append(args, "--request", "{}", "--", cmd.id, tc.arg)
					case "request only":
						args = append(args, "--request", fmt.Sprintf(`{"%s":%q,"Revision":%s}`, cmd.idField, cmd.id, tc.raw))
						if wantCode == "INVALID_POSITIONAL_ARG" {
							wantCode = "INVALID_REVISION"
							if cmd.verb == "get" {
								wantCode = "INVALID_REQUEST_JSON"
							}
						}
					}
					result, _, err := runHarness(t, server, args...)
					if wantCode == "" {
						if err != nil {
							t.Fatalf("valid revision rejected: %v, failure=%+v", err, result.Failure)
						}
						return
					}
					exit, ok := err.(*exec.ExitError)
					if !ok || exit.ExitCode() != 2 || failureCode(result) != wantCode || !strings.Contains(strings.ToLower(result.Failure.Message), "revision") {
						t.Fatalf("error=%v, failure=%+v, want %s and usage exit 2", err, result.Failure, wantCode)
					}
					if strings.Contains(result.Failure.Message, "--revision") {
						t.Fatalf("misleading flag name: %s", result.Failure.Message)
					}
					if wantCode == "INVALID_REVISION" && !strings.Contains(result.Failure.Hint, "agr "+cmd.group+" revision list") {
						t.Fatalf("hint does not name the right revision list: %q", result.Failure.Hint)
					}
				})
			}
		}
		if cmd.verb == "get" {
			t.Run(cmd.group+"/get/uint64 request", func(t *testing.T) {
				const revision = "18446744073709551615"
				request := fmt.Sprintf(`{"%s":%q,"Revision":%s}`, cmd.idField, cmd.id, revision)
				result, _, err := runHarness(t, server, cmd.group, "revision", cmd.verb, "--request", request)
				got, _ := result.Data[cmd.resultField].(map[string]any)
				if err != nil || got["Revision"] != json.Number(revision) {
					t.Fatalf("unsigned revision rejected or changed: error=%v, result=%+v", err, result)
				}
			})
		}
	}
}
