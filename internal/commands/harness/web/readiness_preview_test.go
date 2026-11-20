//go:build preview

package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

func TestWebReadinessColdStartRetainsActualAffinity(t *testing.T) {
	statuses := []int{404, 502, 503, 200}
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			if r.URL.Query().Get("token") != testToken || r.URL.Query().Get("affinity_id") != "requested" {
				t.Error("first request lost token/requested affinity")
			}
			http.SetCookie(w, &http.Cookie{Name: "tencentags-deployment-token", Value: "cookie-token", Secure: true, Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: affinityCookie, Value: url.QueryEscape("actual /+中文"), Secure: true, Path: "/"})
			http.Redirect(w, r, "/?affinity_id=requested", http.StatusTemporaryRedirect)
			return
		}
		cookie, err := r.Cookie("tencentags-deployment-token")
		if err != nil || cookie.Value != "cookie-token" || r.URL.Query().Get("affinity_id") != "actual /+中文" || r.URL.Query().Has("token") || r.Referer() != "" {
			t.Error("retry/redirect lost cookies or actual affinity, or leaked the token in URL/Referer")
		}
		w.WriteHeader(statuses[calls-2])
	}))
	defer server.Close()
	entry, _ := url.Parse(server.URL + "/?token=" + url.QueryEscape(testToken) + "&affinity_id=requested")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	affinity, err := waitForWeb(ctx, entry, server.Client().Transport, time.Millisecond)
	if err != nil || affinity != "actual /+中文" || calls != 5 {
		t.Fatalf("affinity=%q calls=%d err=%v", affinity, calls, err)
	}
}

func TestWebReadinessFailsWithoutRetry(t *testing.T) {
	for _, status := range []int{401, 403, 400, 500, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = w.Write([]byte(testToken))
			}))
			defer server.Close()
			entry, _ := url.Parse(server.URL + "/?token=" + url.QueryEscape(testToken))
			_, err := waitForWeb(t.Context(), entry, server.Client().Transport, time.Millisecond)
			var failure *output.CLIError
			if !errors.As(err, &failure) || failure.Failure.Code != "WEB_READY_FAILED" || failure.Failure.Details["HTTPStatus"] != status || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if (status == 401 || status == 403) && failure.Failure.Kind != output.KindAuthOrPermission {
				t.Fatalf("authentication failure lost kind: %+v", failure.Failure)
			}
			assertNoWebSecret(t, failure)
		})
	}
}

func TestWebReadinessDeadlineAndCancellation(t *testing.T) {
	for _, blockedRequest := range []bool{false, true} {
		for _, cancelWait := range []bool{false, true} {
			t.Run(fmt.Sprintf("blocked=%t/cancel=%t", blockedRequest, cancelWait), func(t *testing.T) {
				started, stopped := make(chan struct{}), make(chan struct{})
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(started)
					if blockedRequest {
						<-r.Context().Done()
					}
					close(stopped)
					w.WriteHeader(502)
				}))
				defer server.Close()
				entry, _ := url.Parse(server.URL + "/?token=" + url.QueryEscape(testToken))
				ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()
				if cancelWait {
					go func() { <-started; cancel() }()
				}
				_, err := waitForWeb(ctx, entry, server.Client().Transport, time.Hour)
				var failure *output.CLIError
				code := "WEB_READY_TIMEOUT"
				if cancelWait {
					code = "CANCELED"
				}
				if !errors.As(err, &failure) || failure.Failure.Code != code {
					t.Fatalf("err=%v, want %s", err, code)
				}
				select {
				case <-stopped:
				case <-time.After(time.Second):
					t.Fatal("canceled readiness request remained active")
				}
				if !blockedRequest && !cancelWait && failure.Failure.Details["HTTPStatus"] != 502 {
					t.Fatal("timeout lost the last HTTP status")
				}
				assertNoWebSecret(t, failure)
			})
		}
	}
}

func TestWebReadinessRejectsCrossOriginAndDowngrade(t *testing.T) {
	for _, location := range []string{"https://other.invalid/?token=", "http://same.invalid/?token="} {
		calls := 0
		transport := webRoundTrip(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 307, Header: http.Header{"Location": {location + url.QueryEscape(testToken)}}, Body: http.NoBody, Request: req}, nil
		})
		entry, _ := url.Parse("https://same.invalid/?token=" + url.QueryEscape(testToken))
		_, err := waitForWeb(t.Context(), entry, transport, time.Millisecond)
		if err == nil || calls != 1 {
			t.Fatalf("unsafe redirect followed: calls=%d err=%v", calls, err)
		}
		assertNoWebSecret(t, err)
	}
}

func TestWebReadinessTransportErrorRedaction(t *testing.T) {
	entry, _ := url.Parse("https://example.invalid/?token=" + url.QueryEscape(testToken))
	_, err := waitForWeb(t.Context(), entry, webRoundTrip(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("transport error %s %s", entry, testToken)
	}), time.Millisecond)
	if err == nil {
		t.Fatal("transport failure reported ready")
	}
	assertNoWebSecret(t, err)
}

func assertNoWebSecret(t *testing.T, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), testToken) || strings.Contains(string(raw), url.QueryEscape(testToken)) || strings.Contains(string(raw), "token=") {
		t.Fatalf("readiness error exposed credentials: %s", raw)
	}
}

func TestWebInvalidTimeoutHasNoSideEffects(t *testing.T) {
	webConfig(t)
	for _, timeout := range []string{"0", "-1s", "invalid"} {
		plane := &webPlane{beforeRead: func(context.Context, string) error { t.Fatal("invalid timeout reached the cloud"); return nil }}
		client, err := harnesscli.NewClient(command.Deps{ControlPlane: plane}, "harness.web")
		if err != nil {
			t.Fatal(err)
		}
		_, err = run(t.Context(), client, plane, command.Request{Flags: map[string]command.FlagValue{"timeout": {String: timeout}}}, func(string) error { t.Fatal("invalid timeout launched browser"); return nil })
		var failure *output.CLIError
		if !errors.As(err, &failure) || failure.Failure.Code != "INVALID_TIMEOUT" {
			t.Fatalf("err=%v", err)
		}
	}
}
