package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	createsnapshot "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/instance/snapshot/create"
	deletesnapshot "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/instance/snapshot/delete"
	listsnapshots "github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/instance/snapshot/list"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

type snapshotControlPlane struct {
	action   string
	request  map[string]any
	response any
	err      error
	calls    int
}

func (f *snapshotControlPlane) Call(_ context.Context, action string, request map[string]any) (any, error) {
	f.action, f.request = action, request
	f.calls++
	return f.response, f.err
}

func stringFlag(value string) command.FlagValue {
	return command.FlagValue{Type: command.FlagString, String: value, Changed: true}
}

func snapshotRuntime(t *testing.T, module command.Module, cp *snapshotControlPlane) command.Runtime {
	t.Helper()
	if module.Descriptor.Source != command.SourceMixedAPI || module.Descriptor.Generated == nil || module.Descriptor.API == nil {
		t.Fatal("snapshot module lost its generated API identity")
	}
	runtime, err := module.Build(command.Deps{ControlPlane: cp})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestCreateSnapshotPreservesInputTransportsAndResponse(t *testing.T) {
	const payload = `{"InstanceId":"ssi-source","SnapshotName":"checkpoint","Memory":false,"Tags":[{"Key":"env","Value":"test"}],"ClientToken":"retry-token"}`
	file := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(file, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		flags map[string]command.FlagValue
		stdin string
	}{
		{"flags", map[string]command.FlagValue{
			"instance-id":   stringFlag("ssi-source"),
			"snapshot-name": stringFlag("checkpoint"),
			"memory":        {Type: command.FlagBool, Bool: false, Changed: true},
			"tags":          stringFlag(`[{"Key":"env","Value":"test"}]`),
			"client-token":  stringFlag("retry-token"),
		}, ""},
		{"request-inline", map[string]command.FlagValue{"request": stringFlag(payload)}, ""},
		{"request-file", map[string]command.FlagValue{"request": stringFlag("@" + file)}, ""},
		{"request-stdin", map[string]command.FlagValue{"request": stringFlag("-")}, payload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := map[string]any{"Snapshot": map[string]any{
				"SnapshotId": "snapshot-test", "SourceInstanceId": "ssi-source", "MemoryIncluded": false,
				"Status": "ACTIVE", "FutureField": map[string]any{"Value": "preserved"},
			}, "RequestId": "req-test"}
			cp := &snapshotControlPlane{response: response}
			runtime := snapshotRuntime(t, createsnapshot.GeneratedModule(), cp)
			result, err := runtime.Handler.Run(t.Context(), command.Request{Flags: tc.flags, Stdin: strings.NewReader(tc.stdin)})
			if err != nil {
				t.Fatal(err)
			}
			if cp.calls != 1 || cp.action != "CreateSandboxSnapshot" || cp.request["InstanceId"] != "ssi-source" || cp.request["SnapshotName"] != "checkpoint" || cp.request["Memory"] != false || cp.request["ClientToken"] != "retry-token" {
				t.Fatalf("unexpected call: %+v", cp)
			}
			wantTags := []any{map[string]any{"Key": "env", "Value": "test"}}
			if !reflect.DeepEqual(cp.request["Tags"], wantTags) || !reflect.DeepEqual(result.Data, response) {
				t.Fatalf("request or response changed: request=%#v response=%#v", cp.request, result.Data)
			}
			var text bytes.Buffer
			result.Text(&text)
			for _, want := range []string{"snapshot-test", "ssi-source", "MemoryIncluded:", "false", "ACTIVE"} {
				if !strings.Contains(text.String(), want) {
					t.Errorf("missing %q: %s", want, text.String())
				}
			}
			if len(result.Effects) != 1 || result.Effects[0].Id != "snapshot-test" || result.Effects[0].Resource != "snapshot" {
				t.Fatalf("effects = %#v", result.Effects)
			}
		})
	}
}

func TestCreateSnapshotDoesNotInjectMemoryDefault(t *testing.T) {
	cp := &snapshotControlPlane{response: map[string]any{"Snapshot": map[string]any{"SnapshotId": "snapshot-test"}}}
	runtime := snapshotRuntime(t, createsnapshot.GeneratedModule(), cp)
	_, err := runtime.Handler.Run(t.Context(), command.Request{Flags: map[string]command.FlagValue{
		"instance-id": stringFlag("ssi-source"),
		"memory":      {Type: command.FlagBool, Bool: false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := cp.request["Memory"]; present {
		t.Fatalf("omitted Memory was sent: %#v", cp.request)
	}
}

func TestSnapshotJSONFieldsSupportFileAndStdin(t *testing.T) {
	for _, field := range []struct {
		name, property, payload string
		module                  command.Module
		response                map[string]any
	}{
		{"tags", "Tags", `[{"Key":"env","Value":"test"}]`, createsnapshot.GeneratedModule(), map[string]any{"Snapshot": map[string]any{"SnapshotId": "snapshot-test"}}},
		{"filters", "Filters", `[{"Name":"status","Values":["ACTIVE"]}]`, listsnapshots.GeneratedModule(), map[string]any{"SnapshotSet": []any{}}},
	} {
		file := filepath.Join(t.TempDir(), "field.json")
		if err := os.WriteFile(file, []byte(field.payload), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name, value, stdin string
			valid              bool
		}{
			{"inline", field.payload, "", true},
			{"file", "@" + file, "", true},
			{"stdin", "-", field.payload, true},
			{"missing-file", "@" + file + ".missing", "", false},
			{"invalid-json", "{", "", false},
			{"empty-stdin", "-", "", false},
		} {
			t.Run(field.name+"/"+tc.name, func(t *testing.T) {
				cp := &snapshotControlPlane{response: field.response}
				runtime := snapshotRuntime(t, field.module, cp)
				flags := map[string]command.FlagValue{field.name: stringFlag(tc.value)}
				if field.name == "tags" {
					flags["instance-id"] = stringFlag("ssi-source")
				}
				result, err := runtime.Handler.Run(t.Context(), command.Request{Flags: flags, Stdin: strings.NewReader(tc.stdin)})
				if !tc.valid {
					if err == nil || result != nil || cp.calls != 0 {
						t.Fatalf("invalid input accepted: calls=%d result=%#v err=%v", cp.calls, result, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(cp.request[field.property])
				if err != nil || string(encoded) != field.payload {
					t.Fatalf("field changed: value=%s err=%v", encoded, err)
				}
			})
		}
	}
}

func TestCreateSnapshotRejectsMissingResponseIdentity(t *testing.T) {
	for _, response := range []any{nil, map[string]any{}, map[string]any{"Snapshot": nil}, map[string]any{"Snapshot": map[string]any{}}, map[string]any{"Snapshot": "invalid"}} {
		cp := &snapshotControlPlane{response: response}
		runtime := snapshotRuntime(t, createsnapshot.GeneratedModule(), cp)
		result, err := runtime.Handler.Run(t.Context(), command.Request{Flags: map[string]command.FlagValue{"instance-id": stringFlag("ssi-source")}})
		if err == nil || result != nil {
			t.Fatalf("malformed response succeeded: response=%#v result=%#v err=%v", response, result, err)
		}
	}
}

func TestDeleteSnapshotSupportsPositionalAndRawRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  command.Request
	}{
		{"positional", command.Request{Args: []string{"snapshot-test"}, ArgValues: map[string]string{"snapshot-id": "snapshot-test"}}},
		{"request", command.Request{Flags: map[string]command.FlagValue{"request": stringFlag(`{"SnapshotId":"snapshot-test"}`)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := map[string]any{"RequestId": "req-delete"}
			cp := &snapshotControlPlane{response: response}
			runtime := snapshotRuntime(t, deletesnapshot.GeneratedModule(), cp)
			result, err := runtime.Handler.Run(t.Context(), tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if cp.calls != 1 || cp.action != "DeleteSandboxSnapshot" || cp.request["SnapshotId"] != "snapshot-test" || !reflect.DeepEqual(result.Data, response) {
				t.Fatalf("call=%+v result=%#v", cp, result.Data)
			}
			var text bytes.Buffer
			result.Text(&text)
			if got := text.String(); got != "Snapshot deletion requested: snapshot-test\n" {
				t.Fatalf("unexpected acknowledgement: %q", got)
			}
			if len(result.Effects) != 1 || result.Effects[0].Id != "snapshot-test" {
				t.Fatalf("effects = %#v", result.Effects)
			}
		})
	}
}

func TestDeleteSnapshotConfirmation(t *testing.T) {
	for _, rawRequest := range []bool{false, true} {
		for _, tc := range []struct {
			name           string
			answer         string
			tty            bool
			yes            bool
			nonInteractive bool
			wantPrompt     bool
			wantDelete     bool
		}{
			{name: "accept", answer: "y\n", tty: true, wantPrompt: true, wantDelete: true},
			{name: "accept-uppercase", answer: "Y\n", tty: true, wantPrompt: true, wantDelete: true},
			{name: "decline", answer: "n\n", tty: true, wantPrompt: true},
			{name: "default-cancel", answer: "\n", tty: true, wantPrompt: true},
			{name: "eof-cancel", tty: true, wantPrompt: true},
			{name: "yes-skips", answer: "n\n", tty: true, yes: true, wantDelete: true},
			{name: "non-interactive-skips", answer: "n\n", tty: true, nonInteractive: true, wantDelete: true},
			{name: "non-tty-skips", answer: "n\n", wantDelete: true},
		} {
			t.Run(fmt.Sprintf("raw=%t/%s", rawRequest, tc.name), func(t *testing.T) {
				flag := cli.RootCmd().PersistentFlags().Lookup("non-interactive")
				previous := flag.Value.String()
				if err := flag.Value.Set(strconv.FormatBool(tc.nonInteractive)); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := flag.Value.Set(previous); err != nil {
						t.Error(err)
					}
				})
				ios, stdin, stdout, stderr := iostreams.Test()
				ios.SetStdinTTY(tc.tty)
				stdin.WriteString(tc.answer)
				cp := &snapshotControlPlane{response: map[string]any{"RequestId": "req-delete"}}
				runtime, err := deletesnapshot.GeneratedModule().Build(command.Deps{ControlPlane: cp, IO: ios})
				if err != nil {
					t.Fatal(err)
				}
				req := command.Request{Flags: map[string]command.FlagValue{
					"yes": {Type: command.FlagBool, Bool: tc.yes, Changed: true},
				}}
				if rawRequest {
					req.Flags["request"] = stringFlag(`{"SnapshotId":"snapshot-test"}`)
				} else {
					req.ArgValues = map[string]string{"snapshot-id": "snapshot-test"}
				}
				result, err := runtime.Handler.Run(t.Context(), req)
				if err != nil || result == nil {
					t.Fatalf("result=%#v err=%v", result, err)
				}
				if stdout.Len() != 0 {
					t.Fatalf("prompt leaked to stdout: %s", stdout)
				}
				if tc.wantPrompt {
					if !strings.Contains(stderr.String(), "snapshot-test") || !strings.Contains(stderr.String(), "Proceed? [y/N]") {
						t.Fatalf("missing confirmation: %q", stderr.String())
					}
				} else if stderr.Len() != 0 || stdin.String() != tc.answer {
					t.Fatalf("non-interactive path prompted or read stdin: stderr=%q stdin=%q", stderr.String(), stdin.String())
				}
				if tc.wantDelete {
					if cp.calls != 1 || !reflect.DeepEqual(cp.request, map[string]any{"SnapshotId": "snapshot-test"}) || len(result.Effects) != 1 {
						t.Fatalf("delete call=%+v effects=%#v", cp, result.Effects)
					}
				} else {
					if cp.calls != 0 || len(result.Effects) != 0 || result.ExitCode != 0 || result.Failure != nil {
						t.Fatalf("cancel caused mutation or failure: calls=%d result=%#v", cp.calls, result)
					}
					data, err := json.Marshal(result.Data)
					if err != nil || string(data) != `{"Cancelled":true}` {
						t.Fatalf("cancel data=%s err=%v", data, err)
					}
					result.Text(stdout)
					if stdout.String() != "Cancelled.\n" {
						t.Fatalf("cancel text=%q", stdout.String())
					}
				}
			})
		}
	}
}

func TestSnapshotInvalidInputDoesNotCallAPI(t *testing.T) {
	for _, tc := range []struct {
		name   string
		module command.Module
		req    command.Request
	}{
		{"delete-missing-id", deletesnapshot.GeneratedModule(), command.Request{}},
		{"delete-empty-request", deletesnapshot.GeneratedModule(), command.Request{Flags: map[string]command.FlagValue{"request": stringFlag(`{}`)}}},
		{"delete-conflicting-id", deletesnapshot.GeneratedModule(), command.Request{
			ArgValues: map[string]string{"snapshot-id": "snapshot-one"},
			Flags:     map[string]command.FlagValue{"request": stringFlag(`{"SnapshotId":"snapshot-two"}`)},
		}},
		{"create-mixed-request", createsnapshot.GeneratedModule(), command.Request{Flags: map[string]command.FlagValue{
			"request": stringFlag(`{"InstanceId":"ssi-source"}`), "memory": {Type: command.FlagBool, Changed: true, Bool: false},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &snapshotControlPlane{}
			ios, _, _, stderr := iostreams.Test()
			ios.SetStdinTTY(true)
			runtime, err := tc.module.Build(command.Deps{ControlPlane: cp, IO: ios})
			if err != nil {
				t.Fatal(err)
			}
			result, err := runtime.Handler.Run(t.Context(), tc.req)
			if err == nil || result != nil || cp.calls != 0 {
				t.Fatalf("invalid input accepted: calls=%d result=%#v err=%v", cp.calls, result, err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("invalid input prompted for confirmation: %q", stderr.String())
			}
		})
	}
}

func TestListSnapshotsPreservesPaginationAndResponse(t *testing.T) {
	response := map[string]any{
		"SnapshotSet": []any{map[string]any{"SnapshotId": "snapshot-test", "SourceInstanceId": "ssi-source", "MemoryIncluded": false, "Status": "ACTIVE"}},
		"TotalCount":  2, "NextToken": "next-page", "RequestId": "req-list", "FutureField": "preserved",
	}
	cp := &snapshotControlPlane{response: response}
	runtime := snapshotRuntime(t, listsnapshots.GeneratedModule(), cp)
	result, err := runtime.Handler.Run(t.Context(), command.Request{Flags: map[string]command.FlagValue{
		"next-token": stringFlag("previous-page"), "max-results": {Type: command.FlagInt, Changed: true, Int: 20},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if cp.action != "DescribeSandboxSnapshotList" || cp.request["NextToken"] != "previous-page" || !reflect.DeepEqual(result.Data, response) {
		t.Fatalf("call=%+v result=%#v", cp, result.Data)
	}
	var text bytes.Buffer
	result.Text(&text)
	for _, want := range []string{"snapshot-test", "ssi-source", "false", "Showing 1 of 2 snapshots", "NextToken: next-page"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("missing %q: %s", want, text.String())
		}
	}
}

func TestListSnapshotsRejectsMalformedMembers(t *testing.T) {
	for _, value := range []any{"invalid", map[string]any{}, []any{"invalid"}, []any{nil}} {
		cp := &snapshotControlPlane{response: map[string]any{"SnapshotSet": value}}
		runtime := snapshotRuntime(t, listsnapshots.GeneratedModule(), cp)
		result, err := runtime.Handler.Run(t.Context(), command.Request{})
		if err == nil || result != nil {
			t.Fatalf("malformed SnapshotSet succeeded: value=%#v result=%#v err=%v", value, result, err)
		}
	}
}

func TestSnapshotPaginationMatchesServiceModes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload map[string]any
		valid   bool
	}{
		{"default", map[string]any{}, true},
		{"zeros", map[string]any{"Offset": 0, "Limit": 0, "MaxResults": 0}, true},
		{"negative-offset", map[string]any{"Offset": -1}, false},
		{"negative-limit", map[string]any{"Limit": -1}, false},
		{"negative-max-results", map[string]any{"MaxResults": -1}, false},
		{"minimum-limit", map[string]any{"Limit": 1}, true},
		{"maximum-limit", map[string]any{"Limit": 100}, true},
		{"limit-too-large", map[string]any{"Limit": 101}, false},
		{"minimum-max-results", map[string]any{"MaxResults": 1}, true},
		{"maximum-max-results", map[string]any{"MaxResults": 100}, true},
		{"max-results-too-large", map[string]any{"MaxResults": 101}, false},
		{"offset", map[string]any{"Offset": 10, "Limit": 20}, true},
		{"cursor", map[string]any{"NextToken": "cursor", "MaxResults": 20}, true},
		{"cursor-with-limit", map[string]any{"NextToken": "cursor", "Limit": 20}, true},
		{"cursor-with-zero-offset", map[string]any{"NextToken": "cursor", "Offset": 0}, true},
		{"max-results-with-zeros", map[string]any{"MaxResults": 20, "Offset": 0, "Limit": 0}, true},
		{"zero-max-results", map[string]any{"MaxResults": 0, "Offset": 10, "Limit": 20}, true},
		{"empty-cursor", map[string]any{"NextToken": "", "Offset": 10}, true},
		{"cursor-offset-conflict", map[string]any{"NextToken": "cursor", "Offset": 1}, false},
		{"max-results-offset-conflict", map[string]any{"MaxResults": 20, "Offset": 1}, false},
		{"max-results-limit-conflict", map[string]any{"MaxResults": 20, "Limit": 1}, false},
	} {
		for _, raw := range []bool{false, true} {
			mode := "flags"
			if raw {
				mode = "request"
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				flags := map[string]command.FlagValue{}
				if raw {
					payload, err := json.Marshal(tc.payload)
					if err != nil {
						t.Fatal(err)
					}
					flags["request"] = stringFlag(string(payload))
				} else {
					for property, name := range map[string]string{"Offset": "offset", "Limit": "limit", "MaxResults": "max-results", "NextToken": "next-token"} {
						switch value := tc.payload[property].(type) {
						case int:
							flags[name] = command.FlagValue{Type: command.FlagInt, Int: value, Changed: true}
						case string:
							flags[name] = stringFlag(value)
						}
					}
				}
				cp := &snapshotControlPlane{response: map[string]any{"SnapshotSet": []any{}, "TotalCount": 0}}
				runtime := snapshotRuntime(t, listsnapshots.GeneratedModule(), cp)
				result, err := runtime.Handler.Run(t.Context(), command.Request{Flags: flags})
				if tc.valid {
					if err != nil || result == nil || cp.calls != 1 {
						t.Fatalf("valid pagination failed: calls=%d result=%#v err=%v", cp.calls, result, err)
					}
					got, err := json.Marshal(cp.request)
					if err != nil {
						t.Fatal(err)
					}
					want, err := json.Marshal(tc.payload)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("pagination changed: got=%s want=%s err=%v", got, want, err)
					}
				} else {
					var cliErr *output.CLIError
					if !errors.As(err, &cliErr) || cliErr.Failure.Code != "INVALID_PAGINATION" || result != nil || cp.calls != 0 {
						t.Fatalf("invalid pagination accepted: calls=%d result=%#v err=%v", cp.calls, result, err)
					}
				}
			})
		}
	}
}

func TestSnapshotPaginationJSONIntegerBounds(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		valid         bool
	}{
		{"exact-large-offset", `{"Offset":9007199254740993}`, true},
		{"maximum-offset", `{"Offset":9223372036854775807}`, true},
		{"offset-overflow", `{"Offset":9223372036854775808}`, false},
		{"offset-underflow", `{"Offset":-9223372036854775809}`, false},
		{"limit-overflow", `{"Limit":9223372036854775808}`, false},
		{"max-results-overflow", `{"MaxResults":9223372036854775808}`, false},
		{"fractional-offset", `{"Offset":0.5}`, false},
		{"fractional-limit", `{"Limit":1.5}`, false},
		{"fractional-max-results", `{"MaxResults":1.5}`, false},
		{"string-limit", `{"Limit":"20"}`, false},
		{"nulls", `{"Limit":null,"MaxResults":null,"Offset":null}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &snapshotControlPlane{response: map[string]any{"SnapshotSet": []any{}}}
			runtime := snapshotRuntime(t, listsnapshots.GeneratedModule(), cp)
			result, err := runtime.Handler.Run(t.Context(), command.Request{Flags: map[string]command.FlagValue{
				"request": stringFlag(tc.payload),
			}})
			if !tc.valid {
				var cliErr *output.CLIError
				if !errors.As(err, &cliErr) || cliErr.Failure.Code != "INVALID_PAGINATION" || result != nil || cp.calls != 0 {
					t.Fatalf("invalid pagination accepted: calls=%d result=%#v err=%v", cp.calls, result, err)
				}
				return
			}
			if err != nil || result == nil || cp.calls != 1 {
				t.Fatalf("valid pagination failed: calls=%d result=%#v err=%v", cp.calls, result, err)
			}
			got, err := json.Marshal(cp.request)
			if err != nil || string(got) != tc.payload {
				t.Fatalf("pagination changed: got=%s want=%s err=%v", got, tc.payload, err)
			}
		})
	}
}

func TestSnapshotPreservesAPIErrorsWithoutRetry(t *testing.T) {
	cause := errors.New("snapshot rejected")
	cp := &snapshotControlPlane{err: cause}
	runtime := snapshotRuntime(t, createsnapshot.GeneratedModule(), cp)
	result, err := runtime.Handler.Run(t.Context(), command.Request{Flags: map[string]command.FlagValue{"instance-id": stringFlag("ssi-source")}})
	if !errors.Is(err, cause) || result != nil || cp.calls != 1 {
		t.Fatalf("calls=%d result=%#v err=%v", cp.calls, result, err)
	}
}
