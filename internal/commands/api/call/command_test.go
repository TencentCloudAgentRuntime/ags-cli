package call

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

type fakeRawCaller struct {
	action string
	raw    []byte
}

func (f *fakeRawCaller) RawCall(_ context.Context, action string, raw []byte) (*RawCallResult, error) {
	f.action = action
	f.raw = append([]byte(nil), raw...)
	return &RawCallResult{Response: map[string]any{"ok": true}, Warnings: []string{"warn"}, MetaExtra: map[string]any{"mode": "test"}}, nil
}

func TestModuleBuildsRawCallRequest(t *testing.T) {
	caller := &fakeRawCaller{}
	runtime, err := Module().Build(command.Deps{ControlPlane: caller})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	const rawRequest = " \n{\"Limit\":1,\"Cursor\":9007199254740993}\n"
	result, err := runtime.Handler.Run(t.Context(), testRequest("DescribeSandboxInstanceList", rawRequest))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if caller.action != "DescribeSandboxInstanceList" {
		t.Fatalf("action = %q", caller.action)
	}
	if string(caller.raw) != rawRequest {
		t.Fatalf("raw payload changed: %q", caller.raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(caller.raw, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["Limit"].(float64) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if result.MetaExtra["mode"] != "test" || len(result.Warnings) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestRawCallPreservesJSONDiagnosticCause(t *testing.T) {
	badFile := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badFile, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, input string
		stdin       io.Reader
	}{
		{"inline", "bad", nil},
		{"file", "@" + badFile, nil},
		{"stdin", "-", strings.NewReader("bad")},
		{"empty stdin", "-", strings.NewReader("")},
		{"trailing JSON", "{}{}", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &fakeRawCaller{}
			runtime, err := Module().Build(command.Deps{ControlPlane: caller})
			if err != nil {
				t.Fatal(err)
			}
			req := testRequest("DescribeQuotaOverview", tc.input)
			req.Stdin = tc.stdin
			_, err = runtime.Handler.Run(t.Context(), req)
			classified := output.ClassifyError(err)
			if classified == nil || classified.ExitCode != 2 || classified.Failure.Code != "INVALID_REQUEST_JSON" {
				t.Fatalf("classification changed: %v", classified)
			}
			if details := classified.Failure.Details; details["Stage"] != "request_parse" || details["Field"] != "request" {
				t.Errorf("JSON diagnostic context lost: %v", details)
			}
			var syntax *json.SyntaxError
			if !errors.As(classified, &syntax) {
				t.Errorf("JSON syntax cause lost: %v", classified)
			}
			if caller.action != "" {
				t.Fatal("invalid JSON sent an API request")
			}
		})
	}
}

func TestModuleRejectsInvalidInputs(t *testing.T) {
	runtime, err := Module().Build(command.Deps{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	for _, tc := range []struct {
		name    string
		request command.Request
		want    string
	}{
		{name: "missing action", request: testRequest("", `{"a":1}`), want: "MISSING_ACTION"},
		{name: "missing request", request: testRequest("DescribeSandboxInstanceList", ""), want: "MISSING_REQUIRED_FLAG"},
		{name: "array request", request: testRequest("DescribeSandboxInstanceList", `[]`), want: "INVALID_REQUEST_JSON"},
		{name: "missing request file", request: testRequest("DescribeSandboxInstanceList", `@/no/such/file`), want: "INVALID_REQUEST_INPUT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runtime.Handler.Run(context.Background(), tc.request)
			if failureCode(err) != tc.want {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

func failureCode(err error) string {
	cliErr := output.ClassifyError(err)
	if cliErr == nil || cliErr.Failure == nil {
		return ""
	}
	return cliErr.Failure.Code
}

func testRequest(action, request string) command.Request {
	flags := map[string]command.FlagValue{
		"request": {Name: "request", Type: command.FlagString, String: request, Changed: request != ""},
	}
	return command.Request{
		ArgValues: map[string]string{"Action": action},
		Flags:     flags,
	}
}
