package main

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// Cover the real CLI, signing, SDK decoding and derived-tool projection in both
// channels. Explicit false and nested references must reach the wire unchanged.
func TestStorageSecuritySignedRequests(t *testing.T) {
	customJSON := `{"SecurityContext":{"Privileged":false,"Capabilities":{"Add":["NET_BIND_SERVICE"],"Drop":["ALL"]}}}`
	mountsJSON := `[{"Name":"by-id","MountPath":"/id","ReadOnly":false,"Volume":{"VolumeId":"vol-test"}},{"Name":"by-name","MountPath":"/name","ReadOnly":true,"Volume":{"VolumeName":"workspace"}},{"Name":"template","MountPath":"/template","VolumeTemplate":{"VolumeTemplateName":"sessions","ReuseKey":"${metadata.session_id}"}}]`
	overrideJSON := `{"SecurityContext":{"Privileged":true,"Capabilities":{"Drop":["NET_RAW"]}}}`
	partialJSON := `{"Resources":{"CPU":"4","Memory":"8Gi"}}`
	var custom, override, partial map[string]any
	var mounts []any
	for raw, target := range map[string]any{customJSON: &custom, mountsJSON: &mounts, overrideJSON: &override, partialJSON: &partial} {
		if err := json.Unmarshal([]byte(raw), target); err != nil {
			t.Fatal(err)
		}
	}
	type testCase struct {
		name, action string
		args         []string
		stdin        string
		wantCustom   map[string]any
		wantMounts   bool
		debug        bool
	}
	var cases []testCase
	for _, command := range []struct {
		name, action string
		args         []string
		payload      map[string]any
	}{
		{"tool create", "CreateSandboxTool", []string{"tool", "create", "-n", "contract-test", "-t", "custom", "--network-configuration", `{"NetworkMode":"SANDBOX"}`, "--role-arn", "fixture-role"}, map[string]any{"ToolName": "contract-test", "ToolType": "custom", "NetworkConfiguration": map[string]any{"NetworkMode": "SANDBOX"}, "RoleArn": "fixture-role", "CustomConfiguration": custom, "StorageMounts": mounts}},
		{"tool update", "UpdateSandboxTool", []string{"tool", "update", "sdt-source"}, map[string]any{"ToolId": "sdt-source", "CustomConfiguration": custom}},
		{"instance create", "StartSandboxInstance", []string{"instance", "create", "--tool-id", "sdt-source"}, map[string]any{"ToolId": "sdt-source", "CustomConfiguration": custom}},
	} {
		for _, mode := range []string{"inline", "file", "stdin", "request"} {
			tc := testCase{name: command.name + "/" + mode, action: command.action, args: slices.Clone(command.args), wantCustom: custom, wantMounts: command.action == "CreateSandboxTool"}
			customInput, mountsInput := customJSON, mountsJSON
			switch mode {
			case "file":
				for name, raw := range map[string]string{"custom": customJSON, "mounts": mountsJSON} {
					path := filepath.Join(t.TempDir(), name+".json")
					if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
						t.Fatal(err)
					}
					if name == "custom" {
						customInput = "@" + path
					} else {
						mountsInput = "@" + path
					}
				}
			case "stdin":
				if tc.wantMounts {
					mountsInput, tc.stdin = "-", mountsJSON
				} else {
					customInput, tc.stdin = "-", customJSON
				}
			case "request":
				raw, err := json.Marshal(command.payload)
				if err != nil {
					t.Fatal(err)
				}
				tc.args = append(slices.Clone(command.args[:2]), "--request", string(raw))
			}
			if mode != "request" {
				tc.args = append(tc.args, "--custom-configuration", customInput)
				if tc.wantMounts {
					tc.args = append(tc.args, "--storage-mounts", mountsInput)
				}
			}
			cases = append(cases, tc)
		}
	}
	cases = append(cases,
		testCase{name: "fork inherits", action: "CreateSandboxTool", args: []string{"tool", "fork", "sdt-source", "-n", "copy"}, wantCustom: custom, wantMounts: true},
		testCase{name: "fork overrides", action: "CreateSandboxTool", args: []string{"tool", "fork", "sdt-source", "-n", "copy", "--custom-configuration", overrideJSON}, wantCustom: override, wantMounts: true},
		testCase{name: "fork partial configuration replaces source", action: "CreateSandboxTool", args: []string{"tool", "fork", "sdt-source", "-n", "copy", "--custom-configuration", partialJSON}, wantCustom: partial, wantMounts: true},
		testCase{name: "fork empty configuration replaces source", action: "CreateSandboxTool", args: []string{"tool", "fork", "sdt-source", "-n", "copy", "--custom-configuration", "{}"}, wantCustom: map[string]any{}, wantMounts: true},
		testCase{name: "debug inherits", action: "CreateSandboxTool", args: []string{"instance", "debug", "--tool-id", "sdt-source"}, wantCustom: custom, wantMounts: true, debug: true},
		testCase{name: "debug instance overrides", action: "StartSandboxInstance", args: []string{"instance", "debug", "--tool-id", "sdt-source", "--custom-configuration", overrideJSON}, wantCustom: override},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan map[string]any, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Version") != "2025-09-20" {
					t.Error("missing signature or wrong API version")
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				action := r.Header.Get("X-TC-Action")
				if action == tc.action {
					requests <- payload
				}
				response := map[string]any{"RequestId": "contract-test"}
				instance := map[string]any{"InstanceId": "ins-test", "Status": "RUNNING", "AuthMode": "NONE", "CustomConfiguration": custom}
				switch action {
				case "CreateSandboxTool":
					response["ToolId"] = "sdt-copy"
				case "UpdateSandboxTool":
				case "StartSandboxInstance":
					response["Instance"] = instance
				case "DescribeSandboxToolList":
					// Service readback may include both the name and resolved ID.
					resolved := map[string]any{"VolumeId": "vol-test", "VolumeName": "resolved-name"}
					first := maps.Clone(mounts[0].(map[string]any))
					first["Volume"] = resolved
					sourceMounts := append([]any{first}, mounts[1:]...)
					response["SandboxToolSet"] = []any{map[string]any{"ToolId": payload["ToolIds"].([]any)[0], "ToolName": "source", "ToolType": "custom", "Status": "ACTIVE", "RoleArn": "fixture-role", "CustomConfiguration": custom, "StorageMounts": sourceMounts}}
				case "DescribeSandboxInstanceList":
					response["InstanceSet"] = []any{instance}
				default:
					t.Errorf("unexpected action %s", action)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"Response": response})
			}))
			defer server.Close()
			out, stderr, err := runSnapshotCLI(t, server, tc.stdin, tc.args...)
			if err != nil {
				t.Fatalf("CLI: %v; stdout=%s stderr=%s", err, out, stderr)
			}
			var envelope struct{ Status string }
			if err := json.Unmarshal(out, &envelope); err != nil || envelope.Status != "succeeded" {
				t.Fatalf("invalid success envelope: %s (%v)", out, err)
			}
			select {
			case request := <-requests:
				configuration, _ := request["CustomConfiguration"].(map[string]any)
				if !reflect.DeepEqual(configuration["SecurityContext"], tc.wantCustom["SecurityContext"]) {
					t.Fatalf("security context lost: %#v", configuration)
				}
				if strings.HasPrefix(tc.name, "fork ") && !reflect.DeepEqual(configuration, tc.wantCustom) {
					t.Fatalf("fork configuration = %#v, want whole-object replacement %#v", configuration, tc.wantCustom)
				}
				if tc.wantMounts {
					got, _ := request["StorageMounts"].([]any)
					wantLength := len(mounts)
					if tc.debug {
						wantLength++ // The debug workflow adds its envd image mount.
					}
					if len(got) != wantLength || !reflect.DeepEqual(got[:len(mounts)], mounts) {
						t.Fatalf("volume references lost: %#v", got)
					}
				}
			default:
				t.Fatalf("no %s request", tc.action)
			}
		})
	}
}

func TestStorageSecurityInvalidRequestsStayLocal(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "invalid input reached transport", http.StatusInternalServerError)
	}))
	defer server.Close()
	for _, payload := range []string{
		`{"CustomConfiguration":{"SecurityContext":{"Privileged":"false"}}}`,
		`{"CustomConfiguration":{"SecurityContext":{"Capabilities":{"Add":"SYS_ADMIN"}}}}`,
		`{"StorageMounts":[{"Volume":{"VolumeId":123}}]}`,
		`{"StorageMounts":[{"VolumeTemplate":{"VolumeTemplateName":123}}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			var request map[string]any
			if err := json.Unmarshal([]byte(payload), &request); err != nil {
				t.Fatal(err)
			}
			// Start from the same valid create prerequisites as the positive cases,
			// so only the malformed nested field can cause the usage error.
			maps.Copy(request, map[string]any{"ToolName": "test", "ToolType": "custom", "NetworkConfiguration": map[string]any{"NetworkMode": "SANDBOX"}, "RoleArn": "fixture-role"})
			raw, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			out, stderr, err := runSnapshotCLI(t, server, "", "tool", "create", "--request", string(raw))
			var envelope struct{ Failure struct{ Code string } }
			if err == nil || json.Unmarshal(out, &envelope) != nil || envelope.Failure.Code != "INVALID_REQUEST_JSON" {
				t.Fatalf("unexpected invalid-input result: %s %s (%v)", out, stderr, err)
			}
			if calls.Load() != 0 {
				t.Fatal("invalid input caused an API call")
			}
		})
	}
}

func TestStorageSecurityHelpAndSchema(t *testing.T) {
	for _, id := range []string{"tool.create", "tool.update", "tool.fork", "instance.create", "instance.debug"} {
		t.Run(id, func(t *testing.T) {
			schema := schemaForCommand(t, id)
			flag := schema.Flags["custom-configuration"]
			wantType := "json"
			if id == "instance.debug" {
				wantType = "string" // Existing workflow flag; no --request schema.
			}
			if flag.Type != wantType || !strings.Contains(flag.Format, "SecurityContext") || !strings.Contains(flag.Format, "Privileged") {
				t.Fatalf("security help/schema missing: %+v", flag)
			}
			if id != "instance.debug" {
				requirePropertyType(t, schema, "CustomConfiguration", "object")
				requireCliFlag(t, schema, "CustomConfiguration", "custom-configuration")
			}
			cmd, ok := findCobraCommand(contractRoot(), id)
			if !ok || !strings.Contains(commandHelp(t, cmd), "--custom-configuration") {
				t.Fatal("custom configuration absent from runtime help")
			}
			if id == "tool.create" || id == "tool.fork" {
				flag := schema.Flags["storage-mounts"]
				if flag.Type != "json" || !strings.Contains(flag.Format, "VolumeId") || !strings.Contains(flag.Format, "VolumeTemplateName") {
					t.Fatalf("volume help/schema missing: %+v", flag)
				}
				requirePropertyType(t, schema, "StorageMounts", "array")
				requireCliFlag(t, schema, "StorageMounts", "storage-mounts")
			}
		})
	}
}

func TestForkCustomConfigurationHelp(t *testing.T) {
	cmd, ok := findCobraCommand(contractRoot(), "tool.fork")
	if !ok {
		t.Fatal("tool fork command missing")
	}
	help := commandHelp(t, cmd)
	for _, fragment := range []string{"Replace the entire CustomConfiguration", "omitted fields are not inherited", "repeat SecurityContext", "Privileged defaults to true"} {
		if !strings.Contains(help, fragment) {
			t.Errorf("fork help missing %q", fragment)
		}
	}
	schema := schemaForCommand(t, "tool.fork")
	for _, name := range []string{"custom-configuration", "computer-configuration"} {
		examples := schema.Flags[name].Examples
		if len(examples) == 0 {
			t.Fatalf("%s lacks a fork example", name)
		}
		for _, example := range examples {
			if !strings.HasPrefix(example, "agr tool fork ") {
				t.Errorf("%s example invokes a different command: %s", name, example)
			}
			_, rest, overridesCustom := strings.Cut(example, "--custom-configuration '")
			if !overridesCustom {
				if name == "custom-configuration" {
					t.Error("custom configuration example lacks its flag")
				}
				continue
			}
			raw, _, _ := strings.Cut(rest, "'")
			var configuration struct {
				SecurityContext *struct{ Privileged *bool }
			}
			if err := json.Unmarshal([]byte(raw), &configuration); err != nil {
				t.Fatal(err)
			}
			if configuration.SecurityContext == nil || configuration.SecurityContext.Privileged == nil || *configuration.SecurityContext.Privileged {
				t.Errorf("%s example drops explicit privilege restrictions: %s", name, example)
			}
		}
	}
}
