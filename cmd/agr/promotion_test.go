package main

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"unicode"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
)

// Exercise the real CLI, signed transport and JSON envelope. Canonical fields
// must survive whichever SDK or dynamic route the compatibility check selects.
func TestPrecachePromotedContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		stdin  string
		action string
	}{
		{"ID flag", []string{"get", "--pre-cache-image-id", "cache-test"}, "", "DescribePreCacheImageTask"},
		{"ID JSON", []string{"get", "--request", `{"PreCacheImageId":"cache-test"}`}, "", "DescribePreCacheImageTask"},
		{"ID stdin", []string{"get", "--request", "-"}, `{"PreCacheImageId":"cache-test"}`, "DescribePreCacheImageTask"},
		{"ID file", []string{"get", "--request", "@file"}, "", "DescribePreCacheImageTask"},
		{"legacy triple", []string{"get", "sha256:test", "--image", "nginx:latest", "--image-registry-type", "personal"}, "", "DescribePreCacheImageTask"},
		{"create", []string{"create", "--image", "nginx:latest", "--image-registry-type", "personal"}, "", "CreatePreCacheImageTask"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := map[string]any{"Image": "nginx:latest", "ImageDigest": "sha256:test", "ImageRegistryType": "personal", "PreCacheImageId": "cache-test", "RequestId": "request-test"}
			if tc.action == "DescribePreCacheImageTask" {
				response["Status"] = "Success"
				response["Message"] = "cached"
				response["SourceType"] = "EXPLICIT"
				response["CachedImageSizeBytes"] = json.Number("9007199254740993")
				response["CreateTime"] = "created"
				response["LastUsedTime"] = "used"
			}
			requests := make(chan map[string]any, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-TC-Action") != tc.action || r.Header.Get("Authorization") == "" {
					t.Error("wrong action or missing signature")
				}
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests <- request
				_ = json.NewEncoder(w).Encode(map[string]any{"Response": response})
			}))
			defer server.Close()
			if tc.name == "ID file" {
				path := filepath.Join(t.TempDir(), "request.json")
				if err := os.WriteFile(path, []byte(`{"PreCacheImageId":"cache-test"}`), 0600); err != nil {
					t.Fatal(err)
				}
				tc.args[2] = "@" + path
			}
			args := append([]string{"pre-cache-image-task"}, tc.args...)
			args = append(args, "-o", "json")
			cmd := exec.CommandContext(t.Context(), os.Args[0], append([]string{"-test.run=^TestTransportLifecycleHelper$", "--"}, args...)...)
			home := t.TempDir()
			cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})

			cmd.Env = []string{"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"), "AGR_TRANSPORT_LIFECYCLE_HELPER=success", "GORACE=atexit_sleep_ms=0", "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "AGR_REGION=ap-guangzhou", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_TRANSPORT_TEST_CERT=" + string(cert)}
			cmd.Stdin = strings.NewReader(tc.stdin)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("CLI: %v %s", err, stderr.String())
			}
			var envelope struct {
				Status string
				Data   map[string]any
			}
			decoder := json.NewDecoder(bytes.NewReader(out))
			decoder.UseNumber()
			if err := decoder.Decode(&envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Status != "succeeded" {
				t.Fatalf("status=%s", envelope.Status)
			}
			for key, want := range response {
				if envelope.Data[key] != want {
					t.Errorf("%s=%v want %v", key, envelope.Data[key], want)
				}
			}
			select {
			case req := <-requests:
				if strings.HasPrefix(tc.name, "ID") && (len(req) != 1 || req["PreCacheImageId"] != "cache-test") {
					t.Fatalf("ID request: %v", req)
				}
				if tc.name == "legacy triple" && (len(req) != 3 || req["ImageDigest"] != "sha256:test") {
					t.Fatalf("triple: %v", req)
				}
			default:
				t.Fatal("no API request")
			}
		})
	}
}

// Derive required coverage from the canonical request, not a second field list.
// Deleting a generated field/schema property or adding a canonical member must
// fail until the command, help and schema expose it consistently.
func TestPromotedRequestCoverage(t *testing.T) {
	contract, err := apimeta.LoadContract("../../api/ags/v20250920", apimeta.BuildChannel)
	if err != nil {
		t.Fatal(err)
	}
	for _, mapping := range contract.Mapping.Actions {
		if !strings.HasPrefix(mapping.Command, "registry.") && !strings.HasPrefix(mapping.Command, "session.") && !strings.HasPrefix(mapping.Command, "pre-cache-image-task.") {
			continue
		}
		t.Run(mapping.Command, func(t *testing.T) {
			schema := schemaForCommand(t, mapping.Command)
			cmd, ok := findCobraCommand(contractRoot(), mapping.Command)
			if !ok {
				t.Fatal("command absent")
			}
			members := map[string]bool{}
			for _, member := range contract.Spec.Object(mapping.Request).Members {
				if member.Disabled {
					continue
				}
				members[member.Name] = true
				property, ok := schema.RequestSchema[member.Name]
				if !ok {
					t.Errorf("canonical field %s absent from schema", member.Name)
					continue
				}
				field := mapping.Fields[member.Name]
				if field != nil && field.Excluded {
					t.Errorf("promoted field %s unexpectedly excluded", member.Name)
					continue
				}
				if field != nil && field.Positional {
					continue
				}
				flag := apimeta.KebabCase(member.Name)
				if field != nil {
					if field.Flag != "" {
						flag = field.Flag
					}
					if len(field.Inputs) > 0 {
						flag = field.Inputs[0].Flag
					}
				}
				if property.CliFlag == nil || *property.CliFlag != flag {
					t.Errorf("%s CliFlag=%v want %s", member.Name, property.CliFlag, flag)
				}
				if cmd.Flags().Lookup(flag) == nil || !strings.Contains(commandHelp(t, cmd), "--"+flag) {
					t.Errorf("%s missing runtime/help flag --%s", member.Name, flag)
				}
			}
			for name := range schema.RequestSchema {
				if !members[name] {
					t.Errorf("schema contains noncanonical field %s", name)
				}
			}
		})
	}
}

func TestSessionSpaceNamespace(t *testing.T) {
	contract, err := apimeta.LoadContract("../../api/ags/v20250920", apimeta.BuildChannel)
	if err != nil {
		t.Fatal(err)
	}
	root := contractRoot()
	for action, spec := range contract.Spec.Actions {
		if !strings.Contains(spec.Input, "SessionSpace") {
			continue
		}
		t.Run(action, func(t *testing.T) {
			mapping := contract.Mapping.Actions[action]
			if mapping == nil || !strings.HasPrefix(mapping.Command, "session.space.") {
				t.Fatalf("%s must be mapped under session.space", action)
			}
			cmd, ok := findCobraCommand(root, mapping.Command)
			if !ok {
				t.Fatal("nested space command absent")
			}
			schemaForCommand(t, mapping.Command)
			if !strings.Contains(commandHelp(t, cmd), "agr session space ") {
				t.Fatal("space help omits its canonical path")
			}
			oldID := strings.Replace(mapping.Command, "session.space.", "session-space.", 1)
			if _, ok := findCobraCommand(root, oldID); ok {
				t.Fatalf("obsolete command %s remains", oldID)
			}
			if out, err := runAGR(t, "schema", oldID, "-o", "json"); err == nil {
				t.Fatalf("obsolete schema %s remains: %s", oldID, out)
			}
		})
	}
	for _, id := range []string{"session", "session.space", "session.event"} {
		cmd, ok := findCobraCommand(root, id)
		if !ok {
			t.Fatalf("group %s absent", id)
		}
		if help := commandHelp(t, cmd); !strings.Contains(help, "Examples:") {
			t.Errorf("group %s omits workflow examples: %s", id, help)
		}
	}
	if out, err := runAGR(t, "session-space", "--help"); err == nil {
		t.Fatalf("obsolete top-level command remains: %s", out)
	}
}

func TestSessionMissingSpaceHint(t *testing.T) {
	for _, args := range [][]string{
		{"session", "create", "--user-id", "user-123"},
		{"session", "create", "--request", `{"UserId":"user-123"}`},
		{"session", "space", "get", "--request", `{}`},
	} {
		cmd := exec.CommandContext(t.Context(), os.Args[0], append([]string{"-test.run=^TestCharacterizationHelperProcess$", "--"}, append(args, "-o", "json")...)...)
		home := t.TempDir()
		cmd.Env = []string{"AGR_CHARACTERIZATION_HELPER=1", "HOME=" + home, "USERPROFILE=" + home, "GORACE=atexit_sleep_ms=0"}
		out, err := cmd.Output()
		if err == nil {
			t.Fatalf("missing SpaceId accepted: %s", out)
		}
		var envelope struct {
			Failure struct{ Code, Kind, Hint string }
		}
		if err := json.Unmarshal(out, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Failure.Code != "MISSING_REQUIRED_FLAG" || envelope.Failure.Kind != "usage" || !strings.Contains(envelope.Failure.Hint, "agr session space list") || !strings.Contains(envelope.Failure.Hint, "agr session space create") {
			t.Fatalf("missing space diagnostic: %+v", envelope.Failure)
		}
	}
}

func TestSessionHelpMatchesPublishedResponse(t *testing.T) {
	root := contractRoot()
	contract, err := apimeta.LoadContract("../../api/ags/v20250920", apimeta.BuildChannel)
	if err != nil {
		t.Fatal(err)
	}
	for _, mapping := range contract.Mapping.Actions {
		if !strings.HasPrefix(mapping.Command, "session.") {
			continue
		}
		cmd, ok := findCobraCommand(root, mapping.Command)
		if !ok {
			t.Fatal("session command absent")
		}
		help := commandHelp(t, cmd)
		if strings.IndexFunc(help, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0 {
			t.Errorf("mixed-language help for %s", mapping.Command)
		}
		for _, unsupported := range []string{"idempotent retries", "stable EventId", "without overwriting", "preserves shared user state", "does not remove shared user state", "creates another space", "Include recent events"} {
			if strings.Contains(help, unsupported) {
				t.Errorf("%s promises %q", mapping.Command, unsupported)
			}
		}
	}
	get, _ := findCobraCommand(root, "session.get")
	help := commandHelp(t, get)
	if !strings.Contains(help, "no event-list field") || !strings.Contains(help, "agr session event list") {
		t.Fatal("get help does not explain where to read events")
	}
	for _, name := range []string{"NumRecentEvents", "AfterTimestamp"} {
		if _, present := schemaForCommand(t, "session.get").RequestSchema[name]; !present {
			t.Fatalf("published input %s was removed", name)
		}
	}
	for _, object := range []string{"SessionInfo", "DescribeSessionResponse"} {
		for _, field := range contract.Spec.Object(object).Members {
			if field.Member == "EventInfo" && !field.Disabled {
				t.Fatal("published response now includes events; update the help and observable-response checks")
			}
		}
	}
}

func TestPrecacheInvalidSelector(t *testing.T) {
	schema := schemaForCommand(t, "pre-cache-image-task.get")
	explanations := map[string]cli.ExplainData{}
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"Response":{"RequestId":"unexpected"}}`))
	}))
	defer server.Close()
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	cases := []struct {
		name    string
		args    []string
		request string
	}{
		{"missing", nil, `{}`},
		{"digest only", []string{"sha256:x"}, `{"ImageDigest":"sha256:x"}`},
		{"image only", []string{"--image", "a"}, `{"Image":"a"}`},
		{"type only", []string{"--image-registry-type", "personal"}, `{"ImageRegistryType":"personal"}`},
		{"missing type", []string{"sha256:x", "--image", "a"}, `{"ImageDigest":"sha256:x","Image":"a"}`},
		{"missing image", []string{"sha256:x", "--image-registry-type", "personal"}, `{"ImageDigest":"sha256:x","ImageRegistryType":"personal"}`},
		{"missing digest", []string{"--image", "a", "--image-registry-type", "personal"}, `{"Image":"a","ImageRegistryType":"personal"}`},
		{"mixed", []string{"sha256:x", "--image", "a", "--image-registry-type", "personal", "--pre-cache-image-id", "c"}, `{"PreCacheImageId":"c","Image":"a","ImageDigest":"sha256:x","ImageRegistryType":"personal"}`},
		{"mixed partial", []string{"--pre-cache-image-id", "c", "--image", "a"}, `{"PreCacheImageId":"c","Image":"a"}`},
		{"empty ID", []string{"--pre-cache-image-id", ""}, `{"PreCacheImageId":""}`},
		{"blank ID", []string{"--pre-cache-image-id", " "}, `{"PreCacheImageId":" "}`},
		{"empty image", []string{"sha256:x", "--image", "", "--image-registry-type", "personal"}, `{"Image":"","ImageDigest":"sha256:x","ImageRegistryType":"personal"}`},
		{"null ID", nil, `{"PreCacheImageId":null}`},
		{"null triple", nil, `{"Image":null,"ImageDigest":"sha256:x","ImageRegistryType":"personal"}`},
		{"mixed null", nil, `{"PreCacheImageId":"c","Image":null}`},
	}
	for _, tc := range cases {
		for _, transport := range []string{"flags", "inline", "file", "stdin"} {
			if transport == "flags" && strings.Contains(tc.name, "null") {
				continue
			}
			t.Run(tc.name+"/"+transport, func(t *testing.T) {
				args := []string{"pre-cache-image-task", "get"}
				stdin := ""
				switch transport {
				case "flags":
					args = append(args, tc.args...)
				case "inline":
					args = append(args, "--request", tc.request)
				case "file":
					path := filepath.Join(t.TempDir(), "request.json")
					if err := os.WriteFile(path, []byte(tc.request), 0600); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--request", "@"+path)
				case "stdin":
					args = append(args, "--request", "-")
					stdin = tc.request
				}
				args = append(args, "-o", "json")
				cmd := exec.CommandContext(t.Context(), os.Args[0], append([]string{"-test.run=^TestTransportLifecycleHelper$", "--"}, args...)...)
				home := t.TempDir()
				cmd.Env = []string{"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"), "AGR_TRANSPORT_LIFECYCLE_HELPER=success", "GORACE=atexit_sleep_ms=0", "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "AGR_REGION=ap-guangzhou", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_TRANSPORT_TEST_CERT=" + string(cert)}
				cmd.Stdin = strings.NewReader(stdin)
				before := calls.Load()
				out, err := cmd.Output()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 2 {
					t.Fatalf("exit=%v stdout=%s", err, out)
				}
				var envelope struct {
					Failure struct {
						Kind string
						Code string
					}
				}
				if err := json.Unmarshal(out, &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Failure.Kind != "usage" {
					t.Fatalf("failure=%+v", envelope.Failure)
				}
				code := envelope.Failure.Code
				if !slices.Contains(schema.Failures, code) {
					t.Errorf("runtime error %s is absent from schema Failures", code)
				}
				explanation, known := explanations[code]
				if !known {
					output, err := runAGR(t, "explain", code, "-o", "json")
					if err != nil {
						t.Fatalf("explain %s: %v", code, err)
					}
					var result struct{ Data cli.ExplainData }
					if err := json.Unmarshal([]byte(output), &result); err != nil {
						t.Fatal(err)
					}
					explanation = result.Data
					explanations[code] = explanation
				}
				if explanation.Kind != envelope.Failure.Kind || explanation.ExitCode != exit.ExitCode() {
					t.Errorf("explain %s disagrees with runtime: %+v", code, explanation)
				}
				if !slices.Contains(explanation.AffectedCommands, schema.Name) {
					t.Errorf("explain %s omits affected command %s", code, schema.Name)
				}
				if !strings.Contains(strings.Join(explanation.Fix, " "), "pre-cache-image-task get") {
					t.Errorf("explain %s lacks applicable selector advice: %v", code, explanation.Fix)
				}
				if code == "CONFLICTING_INPUTS" && (!slices.Contains(explanation.AffectedCommands, "instance.code.run") || !strings.Contains(strings.Join(explanation.Fix, " "), "-c/--code")) {
					t.Errorf("shared conflict explanation lost code input guidance: %+v", explanation)
				}
				if calls.Load() != before {
					t.Fatal("invalid selector sent a network request")
				}
			})
		}
	}
}

func TestPrecacheHelpSelectorFormats(t *testing.T) {
	for _, id := range []string{"pre-cache-image-task.create", "pre-cache-image-task.get"} {
		t.Run(id, func(t *testing.T) {
			cmd, ok := findCobraCommand(contractRoot(), id)
			if !ok {
				t.Fatal("missing command")
			}
			help := commandHelp(t, cmd)
			for _, value := range []string{"enterprise", "personal", "custom", "repository:tag", "repository@sha256:", "repository:tag@sha256:"} {
				if !strings.Contains(help, value) {
					t.Errorf("help missing %q", value)
				}
			}
		})
	}
}
