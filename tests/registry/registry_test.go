// Package registry tests the preview-only Registry contract through real binaries.
// The server is a serialization fixture, not evidence of cloud service acceptance.
package registry

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func TestRegistryChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("builds both CLI channels")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := apimeta.LoadContract(filepath.Join(root, "api/ags/v20250920"), apimeta.Preview)
	if err != nil {
		t.Fatal(err)
	}
	stable, err := apimeta.LoadContract(filepath.Join(root, "api/ags/v20250920"), apimeta.Stable)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var action string
	var payload map[string]any
	var response map[string]any
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Token") != "fixture-token" || r.Header.Get("X-TC-Version") != "2025-09-20" {
			t.Error("missing signed Cloud API headers")
		}
		mu.Lock()
		defer mu.Unlock()
		calls++
		action = r.Header.Get("X-TC-Action")
		payload = nil
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		if err := dec.Decode(&payload); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if action == "DeleteRegistryRecord" {
			if version, present := payload["VersionId"]; present && strings.TrimSpace(version.(string)) == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"Response": map[string]any{"Error": map[string]string{"Code": "InvalidParameter.VersionId", "Message": "VersionId must be non-empty when present"}}})
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Response": response})
	}))
	defer server.Close()
	dir := t.TempDir()
	env := []string{"HOME=" + dir, "USERPROFILE=" + dir, "PATH=" + os.Getenv("PATH"), "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "TENCENTCLOUD_TOKEN=fixture-token", "AGR_REGION=ap-guangzhou", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_INSECURE_SKIP_VERIFY=1"}
	bins := map[string]string{}
	for _, channel := range []string{"stable", "preview"} {
		bin := filepath.Join(dir, "agr-"+channel)
		args := []string{"build", "-buildvcs=false", "-o", bin}
		if channel == "preview" {
			args = append(args, "-tags=preview")
		}
		args = append(args, "./cmd/agr")
		cmd := exec.CommandContext(t.Context(), "go", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, out)
		}
		bins[channel] = bin
	}
	run := func(channel, input string, want bool, args ...string) map[string]any {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bins[channel], args...)
		cmd.Env = env
		cmd.Stdin = strings.NewReader(input)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if (err == nil) != want {
			t.Fatalf("%v: %v %s %s", args, err, out, stderr.String())
		}
		var result map[string]any
		dec := json.NewDecoder(bytes.NewReader(out))
		dec.UseNumber()
		if err := dec.Decode(&result); err != nil {
			t.Fatalf("decode %v: %v %s", args, err, out)
		}
		return result
	}
	encode := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	count := 0
	for _, name := range contract.Spec.SortedActionNames() {
		mapping := contract.Mapping.Actions[name]
		if !strings.HasPrefix(mapping.Command, "registry.") {
			continue
		}
		count++
		if _, ok := stable.Spec.Actions[name]; ok {
			t.Fatalf("%s leaked into stable API", name)
		}
		t.Run(name, func(t *testing.T) {
			a := contract.Spec.Actions[name]
			request := sampleObject(contract.Spec, a.Input)
			expected := sampleObject(contract.Spec, a.Output)
			// Values beyond float64's exact range and future fields must survive JSON output.
			expected["Future"] = json.Number("9007199254740993")
			mu.Lock()
			response = expected
			before := calls
			mu.Unlock()
			args := strings.Split(mapping.Command, ".")
			got := run("preview", "", true, append(args, "--request", encode(request), "-o", "json")...)
			mu.Lock()
			observedAction, observedRequest, after := action, payload, calls
			mu.Unlock()
			if observedAction != name || after != before+1 || !reflect.DeepEqual(observedRequest, request) {
				t.Fatalf("wire mismatch: action=%s request=%v want=%v", observedAction, observedRequest, request)
			}
			if !reflect.DeepEqual(got["Data"], expected) {
				t.Fatalf("response lost: got=%v want=%v", got["Data"], expected)
			}
			schema := run("preview", "", true, "schema", mapping.Command, "-o", "json")["Data"].(map[string]any)
			if schema["RequiresAuth"] != true || schema["SupportsRequest"] != true {
				t.Fatalf("invalid schema: %v", schema)
			}
			wantMutation := !strings.HasPrefix(name, "Describe") && !strings.HasPrefix(name, "Preview") && name != "GetSkillPackageDownloadURL"
			if schema["Mutation"] != wantMutation {
				t.Fatalf("%s mutation=%v, want %v", name, schema["Mutation"], wantMutation)
			}
			props := schema["RequestSchema"].(map[string]any)["Properties"].(map[string]any)
			if len(props) != len(contract.Spec.Object(a.Input).Members) {
				t.Fatal("schema omits request fields")
			}
			flagArgs := append([]string{}, args...)
			for _, m := range contract.Spec.Object(a.Input).Members {
				p, ok := props[m.Name].(map[string]any)
				if !ok {
					t.Fatalf("missing %s", m.Name)
				}
				flag, ok := p["CliFlag"].(string)
				if !ok {
					t.Fatalf("missing CLI flag for %s", m.Name)
				}
				value := request[m.Name]
				text, ok := value.(string)
				if !ok {
					text = encode(value)
				}
				flagArgs = append(flagArgs, "--"+flag, text)
			}
			run("preview", "", true, append(flagArgs, "-o", "json")...)
			mu.Lock()
			same := reflect.DeepEqual(payload, request)
			before = calls
			mu.Unlock()
			if !same {
				t.Fatal("dedicated flags changed the request")
			}
			run("stable", "", false, append(args, "--request", encode(request), "-o", "json")...)
			mu.Lock()
			after = calls
			mu.Unlock()
			if after != before {
				t.Fatal("stable sent a Registry request")
			}
		})
	}
	if count != 19 {
		t.Fatalf("Registry action count=%d, want 19", count)
	}
	for _, transport := range []string{"inline", "file", "stdin"} {
		t.Run(transport, func(t *testing.T) {
			source := `{"Type":"MANUAL","SkillMd":"---\nname: test\ndescription: Test\n---\n# Test"}`
			value, input := source, ""
			if transport == "file" {
				p := filepath.Join(dir, "source.json")
				if err := os.WriteFile(p, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				value = "@" + p
			}
			if transport == "stdin" {
				value, input = "-", source
			}
			run("preview", input, true, "registry", "record", "create", "--registry-id", "reg-test", "--name", "test", "--descriptor-type", "AGENT_SKILLS", "--skill-source", value, "-o", "json")
			var want any
			_ = json.Unmarshal([]byte(source), &want)
			mu.Lock()
			same := reflect.DeepEqual(payload["SkillSource"], want)
			mu.Unlock()
			if !same {
				t.Fatal("source transport changed value")
			}
		})
	}
	for _, body := range []string{
		`{"RegistryId":"reg-test","Name":"x","DescriptorType":"MCP","MCPSource":{"Type":"MANUAL","Unknown":true}}`,
		`{"RegistryId":"reg-test","Name":"x","DescriptorType":"MCP","MCPSource":{"Type":"MANUAL","Descriptors":{}}}`,
	} {
		mu.Lock()
		before := calls
		mu.Unlock()
		got := run("preview", "", false, "registry", "record", "create", "--request", body, "-o", "json")
		if got["Failure"].(map[string]any)["Code"] != "INVALID_REQUEST_JSON" {
			t.Fatalf("unexpected failure: %v", got)
		}
		mu.Lock()
		after := calls
		mu.Unlock()
		if after != before {
			t.Fatal("invalid request reached server")
		}
	}
	// Explicit selectors must never disappear and widen the deletion scope.
	for _, raw := range []bool{false, true} {
		for _, selector := range []struct {
			present bool
			value   string
		}{{false, ""}, {true, ""}, {true, " "}, {true, "rv-test"}} {
			request := map[string]any{"RegistryId": "reg-test", "RecordId": "rec-test", "Reason": "version cleanup"}
			args := []string{"registry", "record", "delete"}
			if selector.present {
				request["VersionId"] = selector.value
			}
			if raw {
				args = append(args, "--request", encode(request))
			} else {
				args = append(args, "--registry-id", "reg-test", "--record-id", "rec-test", "--reason", "version cleanup")
				if selector.present {
					args = append(args, "--version-id", selector.value)
				}
			}
			valid := !selector.present || strings.TrimSpace(selector.value) != ""
			result := run("preview", "", valid, append(args, "-o", "json")...)
			if !valid && result["Failure"].(map[string]any)["Code"] != "InvalidParameter.VersionId" {
				t.Fatalf("empty delete selector was not rejected: %v", result)
			}
			mu.Lock()
			matches := reflect.DeepEqual(payload, request)
			mu.Unlock()
			if !matches {
				t.Fatalf("raw=%v: deletion scope changed", raw)
			}
		}
	}
	// Required Description must preserve an explicit empty value for clearing it.
	run("preview", "", true, "registry", "update", "--registry-id", "reg-test", "--description", "", "-o", "json")
	mu.Lock()
	description, exists := payload["Description"]
	mu.Unlock()
	if !exists || description != "" {
		t.Fatal("explicit empty description lost")
	}
}

func sampleObject(spec *apimeta.Spec, name string) map[string]any {
	out := map[string]any{}
	for _, m := range spec.Object(name).Members {
		switch m.Type {
		case "object":
			out[m.Name] = sampleObject(spec, m.Member)
		case "list":
			if spec.Object(m.Member) != nil {
				out[m.Name] = []any{sampleObject(spec, m.Member)}
			} else {
				out[m.Name] = []any{"fixture"}
			}
		case "int", "int64", "uint64":
			out[m.Name] = json.Number("1")
		case "bool":
			out[m.Name] = true
		default:
			out[m.Name] = "fixture"
		}
	}
	return out
}
