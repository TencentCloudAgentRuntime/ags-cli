package apicli

import (
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

func TestJSONInputPreservesDiagnosticCause(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	invalid := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(invalid, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	builder := NewRequestBuilder(APIDescriptor{
		Fields: []FieldSpec{{Name: "Event", Parser: "common.default_json", Inputs: []InputSpec{{Name: "event", Flag: "event", Type: command.FlagString}}}},
	})
	for _, flag := range []string{"request", "event"} {
		for _, tc := range []struct {
			name, value, stage string
			stdin              io.Reader
			cause              error
		}{
			{"missing file", "@" + missing, "request_input", nil, os.ErrNotExist},
			{"stdin read", "-", "request_input", failingInput{io.ErrClosedPipe}, io.ErrClosedPipe},
			{"invalid inline", "bad", "request_parse", nil, nil},
			{"invalid file", "@" + invalid, "request_parse", nil, nil},
			{"invalid stdin", "-", "request_parse", strings.NewReader("bad"), nil},
			{"empty stdin", "-", "request_parse", strings.NewReader(""), io.EOF},
		} {
			t.Run(flag+"/"+tc.name, func(t *testing.T) {
				_, err := builder.Build(command.Request{
					Flags: map[string]command.FlagValue{flag: {Type: command.FlagString, Changed: true, String: tc.value}},
					Stdin: tc.stdin,
				})
				classified := output.ClassifyError(err)
				code := "INVALID_REQUEST_INPUT"
				if tc.stage == "request_parse" {
					code = "INVALID_REQUEST_JSON"
					if flag == "event" {
						code = "INVALID_JSON_FLAG"
					}
				}
				if classified == nil || classified.ExitCode != 2 || classified.Failure.Code != code {
					t.Fatalf("classification changed: %v", classified)
				}
				details := classified.Failure.Details
				if details["Stage"] != tc.stage || details["Field"] != flag {
					t.Errorf("details=%v, want stage=%s field=%s", details, tc.stage, flag)
				}
				if tc.cause != nil {
					if !errors.Is(classified, tc.cause) {
						t.Errorf("cause %v lost: %v", tc.cause, classified)
					}
				} else {
					var syntax *json.SyntaxError
					if !errors.As(classified, &syntax) {
						t.Errorf("JSON syntax cause lost: %v", classified)
					}
				}
				if tc.name == "missing file" && (details["Path"] != missing || details["Operation"] != "open") {
					t.Errorf("file context lost: %v", details)
				}
				if tc.name == "stdin read" && details["Path"] != "stdin" {
					t.Errorf("stdin context lost: %v", details)
				}
			})
		}
	}
}

type failingInput struct{ err error }

func (r failingInput) Read([]byte) (int, error) { return 0, r.err }
