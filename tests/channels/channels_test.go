// Package channels exercises real generated binaries with nonempty overlays.
// All HTTP calls are signed with fake credentials and terminate on loopback.
package channels

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestChannelBinaries(t *testing.T) {
	if testing.Short() {
		t.Skip("builds channel binaries")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	for _, name := range []string{"go.mod", "go.sum", "api", "cmd", "internal", "tests/integ"} {
		err := filepath.WalkDir(filepath.Join(root, name), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			dest := filepath.Join(work, rel)
			if entry.IsDir() {
				return os.MkdirAll(dest, 0755)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(dest, data, 0644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	write := func(name string, value any) {
		t.Helper()
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, "api/ags/v20250920", name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	member := func(name, kind, typ string) map[string]any {
		return map[string]any{"name": name, "type": kind, "member": typ, "required": false}
	}
	add := func(path string, value any) map[string]any {
		return map[string]any{"op": "add", "path": path, "value": value}
	}
	patch := []map[string]any{
		add("/objects/StartSandboxInstanceResponse/members/-", member("PreviewReceipt", "string", "string")),
		add("/objects/CreateSandboxToolRequest/members/-", member("PreviewObject", "object", "NetworkConfiguration")),
		add("/objects/CreateSandboxToolRequest/members/-", member("PreviewArray", "list", "NetworkConfiguration")),
		add("/objects/StartSandboxInstanceRequest/members/-", member("PreviewProbe", "string", "string")),
		add("/objects/StartSandboxInstanceRequest/members/-", member("PreviewNumber", "int", "int")),
		add("/objects/SandboxInstance/members/-", member("PreviewResult", "string", "string")),
		add("/objects/CustomConfiguration/members/-", member("PreviewNested", "string", "string")),
		add("/objects/CustomConfigurationDetail/members/-", member("PreviewNested", "string", "string")),
		add("/objects/CreateSandboxToolRequest/members/-", member("PreviewSetting", "string", "string")),
		add("/objects/SandboxTool/members/-", member("PreviewSetting", "string", "string")),
		add("/actions/PreviewProbe", map[string]any{"name": "Preview probe", "input": "PreviewProbeRequest", "output": "PreviewProbeResponse", "status": "online"}),
		add("/objects/PreviewProbeRequest", map[string]any{"type": "object", "members": []any{member("Value", "string", "string")}}),
		add("/objects/PreviewProbeResponse", map[string]any{"type": "object", "members": []any{member("Echo", "string", "string")}}),
	}
	write("api.patch.json", patch)
	write("mapping.patch.json", []any{add("/actions/PreviewProbe", map[string]any{"command": "workspace.probe", "request": "PreviewProbeRequest", "response": "PreviewProbeResponse", "status": "mapped"}), add("/actions/StartSandboxInstance/fields/PreviewProbe", map[string]any{"flag": "preview-probe-alias"})})
	write("help.patch.json", []any{add("/commands/workspace.probe", map[string]any{"short": "Preview module fixture"}), add("/commands/instance.create/fields/PreviewProbe", map[string]any{"description": "Preview input fixture"})})
	goRun := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "go", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
		// Integration tests build their own CLI; propagate the tested channel.
		for _, arg := range args {
			if strings.HasPrefix(arg, "-tags=") {
				cmd.Env = append(cmd.Env, "GOFLAGS="+arg)
			}
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
		return out
	}
	// Register a hand-written preview-only workflow alongside a generated module.
	handwritten := filepath.Join(work, "internal/commands/workspace/local")
	if err := os.MkdirAll(handwritten, 0755); err != nil {
		t.Fatal(err)
	}
	source := `//go:build preview

package local
import (
 "context"
 "github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)
func Module() command.Module {
 return command.Module{
  Descriptor:command.Descriptor{Spec:command.Spec{ID:"workspace.local",Path:[]string{"workspace","local"},Use:"local",Short:"Handwritten preview fixture",SupportsJSON:true},Source:command.SourceWorkflow},
  Build:func(deps command.Deps)(command.Runtime,error){return command.Runtime{Handler:command.HandlerFunc(func(ctx context.Context,req command.Request)(*command.Result,error){return &command.Result{Data:map[string]any{"Handwritten":true}},nil})},nil},
 }
}
`
	if err := os.WriteFile(filepath.Join(handwritten, "command_preview.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	generator := filepath.Join(work, "cmd/internal/cobragen/main.go")
	generatorBase, err := os.ReadFile(generator)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generator, bytes.Replace(generatorBase, []byte("var previewWorkflowIDs = []string{}"), []byte(`var previewWorkflowIDs = []string{"workspace.local"}`), 1), 0644); err != nil {
		t.Fatal(err)
	}
	goRun("run", "./cmd/internal/cobragen")
	goRun("run", "./cmd/internal/cobragen", "check")
	for _, channel := range []string{"stable", "preview"} {
		report := goRun("run", "./cmd/internal/apigen", "--channel", channel, "list-actions", "--json")
		if bytes.Contains(report, []byte("PreviewProbe")) != (channel == "preview") {
			t.Fatalf("maintenance report mixes channels: %s", report)
		}
	}
	goRun("test", "./internal/commands", "./cmd/agr")
	goRun("test", "-tags=preview", "./internal/commands", "./cmd/agr")
	goRun("test", "./tests/integ", "-run", "TestSchema_RegistryInvariants")
	goRun("test", "-tags=preview", "./tests/integ", "-run", "TestSchema_RegistryInvariants")
	stable, preview := filepath.Join(work, "agr-stable"), filepath.Join(work, "agr-preview")
	if os.PathSeparator == '\\' {
		stable += ".exe"
		preview += ".exe"
	}
	goRun("build", "-buildvcs=false", "-o", stable, "./cmd/agr")
	goRun("build", "-buildvcs=false", "-tags=preview", "-o", preview, "./cmd/agr")
	var mu sync.Mutex
	var requests []map[string]any
	var invalidList bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Token") != "test-session" {
			t.Error("missing signature/session token")
		}
		var payload map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			t.Error(err)
		}
		action := r.Header.Get("X-TC-Action")
		payload["_action"] = action
		requests = append(requests, payload)
		instance := map[string]any{"InstanceId": "ssi-test", "Status": "RUNNING", "AuthMode": "NONE", "PreviewResult": "preserved", "ComputerConfiguration": map[string]any{"Future": json.Number("9007199254740993")}}
		tool := map[string]any{"ToolId": "sdt-source", "ToolType": "custom", "Status": "ACTIVE", "PreviewSetting": "inherited", "CustomConfiguration": map[string]any{"Image": "example/image", "PreviewNested": "nested", "ImageDigest": "response-only"}, "NetworkConfiguration": map[string]any{"NetworkMode": "PUBLIC"}}
		response := map[string]any{"RequestId": "request-test"}
		switch action {
		case "StartSandboxInstance":
			response["Instance"] = instance
			response["PreviewReceipt"] = "receipt-preserved"
		case "DescribeSandboxInstanceList":
			response["InstanceSet"] = []any{instance}
			if invalidList {
				response["InstanceSet"] = instance
			}
			response["TotalCount"] = 1
		case "DescribeSandboxToolList":
			response["SandboxToolSet"] = []any{tool}
			response["TotalCount"] = 1
		case "CreateSandboxTool":
			response["ToolId"] = "sdt-new"
		case "PreviewProbe":
			response["Echo"] = payload["Value"]
		default:
			t.Errorf("unexpected action %s", action)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Response": response})
	}))
	defer server.Close()
	home := t.TempDir()
	run := func(binary string, wantOK bool, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), binary, args...)
		cmd.Dir = work
		cmd.Env = []string{"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"), "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "TENCENTCLOUD_TOKEN=test-session", "AGR_REGION=ap-guangzhou", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_INSECURE_SKIP_VERIFY=1"}
		out, err := cmd.CombinedOutput()
		if (err == nil) != wantOK {
			t.Fatalf("%s %v: err=%v\n%s", filepath.Base(binary), args, err, out)
		}
		return out
	}
	// Compare whole trees so new handwritten modules cannot silently disappear.
	commandNames := func(binary string) map[string]bool {
		t.Helper()
		var envelope struct {
			Data struct{ Commands []struct{ Name string } }
		}
		if err := json.Unmarshal(run(binary, true, "schema", "-o", "json"), &envelope); err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, command := range envelope.Data.Commands {
			names[command.Name] = true
		}
		return names
	}
	var schema struct {
		Data struct{ Flags []struct{ Name, Type string } }
	}
	if err := json.Unmarshal(run(preview, true, "schema", "tool.fork", "-o", "json"), &schema); err != nil {
		t.Fatal(err)
	}
	for _, flag := range schema.Data.Flags {
		if (flag.Name == "preview-object" || flag.Name == "preview-array") && flag.Type != "json" {
			t.Errorf("%s uses JSON parser but schema Flag.Type=%s", flag.Name, flag.Type)
		}
	}
	stableNames, previewNames := commandNames(stable), commandNames(preview)
	for name := range stableNames {
		if !previewNames[name] {
			t.Errorf("preview removed stable command %s", name)
		}
	}
	if stableNames["workspace.probe"] || stableNames["workspace.local"] {
		t.Fatal("preview schema leaked into stable")
	}
	for _, binary := range []string{stable, preview} {
		completion := run(binary, true, "__complete", "")
		if bytes.Contains(completion, []byte("workspace")) != (binary == preview) {
			t.Fatalf("completion channel mismatch: %s", completion)
		}
	}
	for _, tc := range []struct{ binary, channel string }{{stable, "stable"}, {preview, "preview"}} {
		out := run(tc.binary, true, "version", "-o", "json")
		if !bytes.Contains(out, []byte(`"Channel":"`+tc.channel+`"`)) {
			t.Fatalf("channel missing: %s", out)
		}
		for _, args := range [][]string{{"instance", "create", "--help"}, {"schema", "instance.create", "-o", "json"}} {
			out := run(tc.binary, true, args...)
			if bytes.Contains(out, []byte("PreviewProbe")) || bytes.Contains(out, []byte("preview-probe-alias")) {
				if tc.channel == "stable" {
					t.Fatalf("preview leaked: %s", out)
				}
			} else if tc.channel == "preview" {
				t.Fatalf("preview field absent: %s", out)
			}
		}
	}
	run(stable, false, "workspace", "local")
	run(preview, true, "workspace", "local", "-o", "json")
	for _, binary := range []string{stable, preview} {
		schema := run(binary, true, "schema", "tool.fork", "-o", "json")
		if bytes.Contains(schema, []byte("PreviewSetting")) != (binary == preview) {
			t.Fatalf("fork schema channel mismatch: %s", schema)
		}
	}
	run(stable, false, "workspace", "probe", "--value", "x", "-o", "json")
	run(stable, false, "instance", "create", "--request", `{"ToolId":"sdt-source","PreviewProbe":"x"}`, "-o", "json")
	run(stable, false, "tool", "create", "--request", `{"CustomConfiguration":{"PreviewNested":"x"}}`, "-o", "json")
	mu.Lock()
	count := len(requests)
	mu.Unlock()
	if count != 0 {
		t.Fatalf("stable rejected payload sent %d requests", count)
	}
	out := run(preview, true, "workspace", "probe", "--value", "echoed", "-o", "json")
	if !bytes.Contains(out, []byte("echoed")) {
		t.Fatalf("new module response lost: %s", out)
	}
	for _, args := range [][]string{
		{"instance", "create", "--request", `{"ToolId":"sdt-source","PreviewProbe":"x","PreviewNumber":9007199254740993,"ClientToken":"unchanged"}`, "-o", "json"},
		{"instance", "create", "--tool-id", "sdt-source", "--preview-probe-alias", "x", "--wait", "-o", "json"},
		{"instance", "get", "ssi-test", "--wait", "-o", "json"},
		{"instance", "list", "-o", "json"},
	} {
		out := run(preview, true, args...)
		if args[0] == "instance" && args[1] == "create" && args[2] == "--request" && !bytes.Contains(out, []byte("receipt-preserved")) {
			t.Errorf("create dropped response-level PreviewReceipt")
		}
		if !bytes.Contains(out, []byte("preserved")) || !bytes.Contains(out, []byte("9007199254740993")) {
			t.Fatalf("resource response truncated: %s", out)
		}
	}
	run(preview, true, "tool", "fork", "sdt-source", "--tool-name", "copy", "-o", "json")
	mu.Lock()
	last := requests[len(requests)-1]
	custom, _ := last["CustomConfiguration"].(map[string]any)
	if last["PreviewSetting"] != "inherited" || custom["PreviewNested"] != "nested" || custom["ImageDigest"] != nil {
		t.Errorf("fork lost/crossed fields: %#v", last)
	}
	if requests[1]["ClientToken"] != "unchanged" || requests[1]["PreviewNumber"] != json.Number("9007199254740993") {
		t.Errorf("client token changed: %#v", requests[1])
	}
	mu.Unlock()
	mu.Lock()
	invalidList = true
	mu.Unlock()
	for _, args := range [][]string{{"instance", "list", "-o", "json"}, {"instance", "list", "--all", "-o", "json"}} {
		out := run(preview, false, args...)
		var result struct {
			Status  string
			Data    any
			Failure struct{ Code string }
		}
		if err := json.Unmarshal(out, &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "failed" || result.Data != nil || result.Failure.Code != "INTERNAL_ERROR" {
			t.Fatalf("list parse failure lost: %s", out)
		}
	}
	mu.Lock()
	invalidList = false
	mu.Unlock()
	// Promotion is a base-contract change, independent of SDK publication.
	promoted := goRun("run", "./cmd/internal/apipatch", "render")
	// Promote only field additions; withdraw the fixture-only command.
	var base map[string]any
	if err := json.Unmarshal(promoted, &base); err != nil {
		t.Fatal(err)
	}
	delete(base["actions"].(map[string]any), "PreviewProbe")
	delete(base["objects"].(map[string]any), "PreviewProbeRequest")
	delete(base["objects"].(map[string]any), "PreviewProbeResponse")
	write("api.json", base)
	for _, name := range []string{"api.patch.json", "mapping.patch.json", "help.patch.json"} {
		write(name, []any{})
	}
	if err := os.WriteFile(generator, generatorBase, 0644); err != nil {
		t.Fatal(err)
	}
	goRun("run", "./cmd/internal/cobragen")
	goRun("run", "./cmd/internal/cobragen", "check")
	if _, err := os.Stat(filepath.Join(work, "internal/apimeta/generated_model_preview.go")); !os.IsNotExist(err) {
		t.Fatalf("obsolete preview output: %v", err)
	}
	goRun("build", "-buildvcs=false", "-o", stable, "./cmd/agr")
	out = run(stable, true, "instance", "create", "--request", `{"ToolId":"sdt-source","PreviewProbe":"promoted"}`, "-o", "json")
	if !bytes.Contains(out, []byte("preserved")) {
		t.Fatalf("promoted field lost: %s", out)
	}
	t.Log("stable/preview isolation, signed requests, create/get/wait/list/fork and promotion verified")
}
