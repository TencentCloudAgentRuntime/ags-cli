package patchscenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

type runtimeSession struct{ id, affinity string }

type harnessRuntime struct {
	t                               *testing.T
	binary                          string
	base, env                       []string
	harness, provider, tool, deploy string
	instances                       map[string]bool
}

// These tests invoke a reviewed preview binary and create billable resources.
// They are separate from the control-plane coverage scenario and require an
// additional opt-in, so ordinary tests never start model turns or MCP calls.
func newHarnessRuntime(t *testing.T, optIn string) *harnessRuntime {
	t.Helper()
	if os.Getenv(optIn) != "1" {
		t.Skip("requires " + optIn + "=1 and authorized live fixtures")
	}
	binary := os.Getenv("AGR_HARNESS_E2E_BINARY")
	if !filepath.IsAbs(binary) || os.Getenv("AGR_REGION") == "" {
		t.Fatal("AGR_HARNESS_E2E_BINARY must be absolute and AGR_REGION must be set")
	}
	r := &harnessRuntime{
		t: t, binary: binary, instances: map[string]bool{},
		base: []string{"--non-interactive", "--region", os.Getenv("AGR_REGION")},
		env:  append(os.Environ(), "AGR_NO_UPDATE_CHECK=1"),
	}
	if config := os.Getenv("AGR_HARNESS_E2E_CONFIG"); config != "" {
		// Read the caller's config directly; do not copy credentials to disk.
		r.base = append(r.base, "--config", config)
	}
	t.Cleanup(r.cleanup)
	return r
}

func (r *harnessRuntime) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.binary, append(slices.Clone(r.base), args...)...)
	cmd.Env = r.env
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func (r *harnessRuntime) call(ctx context.Context, args ...string) (harnessEnvelope, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	raw, callErr := r.command(ctx, append(args, "-o", "json")...).Output()
	var out harnessEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return out, fmt.Errorf("%s: invalid CLI envelope", strings.Join(args[:min(3, len(args))], " "))
	}
	if out.Failure != nil {
		return out, fmt.Errorf("%s: %s (%s)", strings.Join(args[:min(3, len(args))], " "), out.Failure.Code, out.Failure.Message)
	}
	if callErr != nil || out.Status != "succeeded" {
		return out, errors.New("live CLI command did not succeed")
	}
	return out, nil
}

func (r *harnessRuntime) must(args ...string) harnessEnvelope {
	r.t.Helper()
	out, err := r.call(r.t.Context(), args...)
	if err != nil {
		r.t.Fatal(err)
	}
	return out
}

func runtimeInitialization(raw string, capacity int) (string, error) {
	initialization := map[string]any{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &initialization); err != nil {
			return "", errors.New("invalid AGR_HARNESS_INITIALIZE_CONFIGURATION")
		}
	}
	deployment := harnessObject(initialization["Deployment"])
	if deployment == nil {
		deployment = map[string]any{}
		initialization["Deployment"] = deployment
	}
	deployment["ScalingConfiguration"] = map[string]any{
		"MinInstanceCount": 0, "MaxInstanceCount": capacity, "MaxInstanceRequestConcurrency": 200,
	}
	deployment["AffinityConfiguration"] = map[string]any{
		"Mode": "EXCLUSIVE_BEST_EFFORT", "HeaderName": "X-Tencent-Agr-Affinity-Id",
	}
	encoded, err := json.Marshal(initialization)
	return string(encoded), err
}

func runtimePrompt(revision int) string {
	return fmt.Sprintf("You are a test agent. When the entire user message is REVISION, reply only AGR_RUNTIME_REV%d. For other messages, follow the instruction exactly. Never use tools.", revision)
}

func (r *harnessRuntime) create(prompt, mcpReference string, capacity int) {
	r.t.Helper()
	fixture, err := harnessFixtureFromEnv()
	if err != nil {
		r.t.Fatal(err)
	}
	if fixture.engineVersion == "" {
		r.t.Fatal("AGR_HARNESS_ENGINE_VERSION must pin a reviewed runtime")
	}
	initialization, err := runtimeInitialization(fixture.initializeConfiguration, capacity)
	if err != nil {
		r.t.Fatal(err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	out, err := r.call(r.t.Context(), "model-provider", "create", "--name", "agr-runtime-mp-"+suffix,
		"--tokenhub-api-key-id", fixture.apiKeyID, "--model", fixture.model)
	r.provider = harnessText(harnessObject(out.Data["ModelProvider"])["ModelProviderId"])
	if r.provider != "" {
		r.t.Logf("created ModelProviderId=%s", r.provider)
	}
	if err != nil || r.provider == "" {
		r.t.Fatalf("create model provider: %v", err)
	}
	args := []string{"harness", "create", "--name", "agr-runtime-hns-" + suffix,
		"--engine", fixture.engine, "--engine-version", fixture.engineVersion,
		"--model", r.provider + ":" + fixture.model, "--system-prompt", prompt,
		"--role-arn", fixture.roleArn, "--subnet-id", fixture.subnetID,
		"--security-group-id", fixture.securityGroupID,
		"--initialize-configuration", initialization, "--wait"}
	if mcpReference != "" {
		args = append(args, "--mcp", "live="+mcpReference)
	}
	out, err = r.call(r.t.Context(), args...)
	r.harness = harnessText(harnessObject(out.Data["Harness"])["HarnessId"])
	if r.harness == "" && out.Failure != nil {
		r.harness = harnessText(out.Failure.Details["HarnessId"])
	}
	if r.harness != "" {
		r.t.Logf("created HarnessId=%s", r.harness)
	}
	if err != nil || r.harness == "" {
		r.t.Fatalf("create harness: %v", err)
	}
	out = r.must("harness", "get", r.harness)
	resources := harnessObject(harnessObject(out.Data["Harness"])["ResourceInfo"])
	r.tool, r.deploy = harnessText(resources["ToolId"]), harnessText(resources["DeploymentId"])
	if r.tool == "" || r.deploy == "" {
		r.t.Fatal("published harness has no bound Tool/Deployment")
	}
	if mcpReference != "" {
		servers := runtimeItems(harnessObject(harnessObject(out.Data["Harness"])["Spec"])["MCPServers"])
		parts := strings.Split(mcpReference, "/")
		if len(servers) != 1 || harnessText(servers[0]["Name"]) != "live" {
			r.t.Fatal("created Harness did not retain the requested MCP server")
		}
		ref := harnessObject(harnessObject(servers[0]["Source"])["Registry"])
		if harnessText(ref["RegistryId"]) != parts[0] || harnessText(ref["RecordId"]) != parts[1] || harnessText(ref["VersionId"]) != parts[2] {
			r.t.Fatal("created Harness did not retain the pinned MCP reference")
		}
		r.t.Log("MCP server name and immutable reference persisted")
	}
	r.t.Logf("new harness published; ToolId=%s DeploymentId=%s", r.tool, r.deploy)
}

func sessionOf(t *testing.T, data map[string]any) runtimeSession {
	t.Helper()
	session := runtimeSession{harnessText(data["SessionId"]), harnessText(data["AffinityId"])}
	if session.id == "" || session.affinity == "" {
		t.Fatal("prompt did not preserve session and instance affinity")
	}
	return session
}

func (s runtimeSession) flags() []string {
	return []string{"--session-id", s.id, "--affinity-id", s.affinity}
}

func (r *harnessRuntime) prompt(text string, session runtimeSession) map[string]any {
	args := []string{"harness", "prompt", r.harness, text, "--timeout", "120s"}
	if session.id != "" {
		args = append(args, session.flags()...)
	}
	return r.must(args...).Data
}

func requireReply(t *testing.T, data map[string]any, marker string) {
	t.Helper()
	if harnessText(data["StopReason"]) != "end_turn" || !strings.Contains(harnessText(data["Reply"]), marker) {
		// Model replies and recovery affinities are deliberately not logged.
		t.Fatal("real model reply did not match the expected marker")
	}
}

func (r *harnessRuntime) instanceIDs(ctx context.Context) ([]string, error) {
	out, err := r.call(ctx, "instance", "list", "--tool-id", r.tool, "--limit", "100")
	if err != nil {
		return nil, err
	}
	if _, ok := out.Data["Items"].([]any); !ok {
		return nil, errors.New("instance list is missing Items")
	}
	var ids []string
	for _, item := range runtimeItems(out.Data["Items"]) {
		if harnessText(item["ToolId"]) != r.tool {
			return nil, errors.New("instance list crossed the owned Tool boundary")
		}
		id := harnessText(item["InstanceId"])
		if id == "" {
			return nil, errors.New("instance list is missing an identity")
		}
		r.instances[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func runtimeItems(value any) []map[string]any {
	var items []map[string]any
	values, _ := value.([]any)
	for _, item := range values {
		items = append(items, harnessObject(item))
	}
	return items
}

func (r *harnessRuntime) revision(revision int) runtimeSession {
	r.t.Helper()
	before, err := r.instanceIDs(r.t.Context())
	if err != nil {
		r.t.Fatal(err)
	}
	data := r.prompt("REVISION", runtimeSession{})
	requireReply(r.t, data, fmt.Sprintf("AGR_RUNTIME_REV%d", revision))
	after, err := r.instanceIDs(r.t.Context())
	if err != nil {
		r.t.Fatal(err)
	}
	var created []string
	for _, id := range after {
		if !slices.Contains(before, id) {
			created = append(created, id)
		}
	}
	if len(created) != 1 {
		r.t.Fatal("published snapshot did not allocate one distinct instance")
	}
	r.t.Logf("revision %d verified on a distinct new instance", revision)
	return sessionOf(r.t, data)
}

func (r *harnessRuntime) retained(revision int, session runtimeSession) {
	r.t.Helper()
	data := r.prompt("REVISION", session)
	requireReply(r.t, data, fmt.Sprintf("AGR_RUNTIME_REV%d", revision))
	if sessionOf(r.t, data) != session {
		r.t.Fatal("existing snapshot moved to another session or instance")
	}
	r.t.Logf("existing revision %d snapshot retained", revision)
}

func TestHarnessRuntimeLive(t *testing.T) {
	r := newHarnessRuntime(t, "AGR_HARNESS_RUNTIME_E2E")
	r.create(runtimePrompt(1), "", 6)
	data := r.prompt("Reply only AGR_RUNTIME_OK. Do not use tools.", runtimeSession{})
	requireReply(t, data, "AGR_RUNTIME_OK")
	t.Log("prompt without --cwd succeeded with the default /workspace")
	first := sessionOf(t, data)
	continued := r.prompt("What exact AGR marker did I request in my previous message? Reply only that marker.", first)
	requireReply(t, continued, "AGR_RUNTIME_OK")
	if sessionOf(t, continued) != first {
		t.Fatal("continuation changed session or instance affinity")
	}
	r.streamSuccess(first)
	r.stdio()
	r.proxy()
	out, err := r.call(t.Context(), "harness", "proxy", r.harness)
	if err == nil || out.Failure == nil || out.Failure.Code != "UNSUPPORTED_OUTPUT" {
		t.Fatal("proxy did not reject single-envelope JSON output")
	}
	v1 := r.revision(1)
	r.must("harness", "update", r.harness, "--system-prompt", runtimePrompt(2))
	published := r.must("harness", "revision", "create", r.harness, "--publish", "--wait")
	if harnessRevisionStatus(published, "HarnessRevision") != "PUBLISHED" {
		t.Fatal("revision two did not publish")
	}
	r.retained(1, v1)
	v2 := r.revision(2)
	rollback := r.must("harness", "revision", "publish", r.harness, "1", "--wait")
	if harnessRevisionStatus(rollback, "HarnessRevision") != "PUBLISHED" {
		t.Fatal("revision one did not republish")
	}
	r.retained(2, v2)
	v1Again := r.revision(1)
	if v1Again.affinity == v1.affinity || v1Again.affinity == v2.affinity || v1.affinity == v2.affinity {
		t.Fatal("published snapshots shared an instance affinity")
	}
	resources := harnessObject(harnessObject(r.must("harness", "get", r.harness).Data["Harness"])["ResourceInfo"])
	if harnessText(resources["ToolId"]) != r.tool || harnessText(resources["DeploymentId"]) != r.deploy {
		t.Fatal("publication or rollback changed bound resources")
	}
	r.streamCancel(v1Again)
	t.Log("runtime lifecycle passed: model, continuation, NDJSON, stdio/proxy ACP, publish, rollback and snapshot isolation")
}

func TestHarnessMCPLive(t *testing.T) {
	r := newHarnessRuntime(t, "AGR_HARNESS_MCP_E2E")
	reference, prompt := os.Getenv("AGR_HARNESS_MCP_REFERENCE"), os.Getenv("AGR_HARNESS_MCP_PROMPT")
	tool, expected := os.Getenv("AGR_HARNESS_MCP_TOOL"), os.Getenv("AGR_HARNESS_MCP_EXPECT")
	if len(strings.Split(reference, "/")) != 3 || !strings.HasPrefix(reference, "MARKETPLACE/") || prompt == "" || tool == "" || expected == "" {
		t.Fatal("MCP E2E requires a reviewed pinned MARKETPLACE reference, read-only prompt, expected tool name and result marker")
	}
	record := r.must("harness", "tool", "get", strings.TrimPrefix(reference, "MARKETPLACE/"))
	if harnessText(harnessObject(record.Data["Tool"])["Reference"]) != reference {
		t.Fatal("market readback did not match the pinned reference")
	}
	r.create("Use the requested MCP tool. Never substitute local shell or your own memory for a requested MCP call.", reference, 1)
	events, err := r.events("harness", "prompt", r.harness, prompt, "--cwd", "/tmp", "--timeout", "120s")
	t.Logf("MCP evidence summary: %s", mcpDiagnostic(events, tool, expected))
	if err != nil || !mcpEvidence(events, tool, expected) {
		t.Fatal("MCP E2E requires a completed matching call and the expected tool-result content, not only a model reply")
	}
	t.Log("MCP marketplace reference, publication and real tool-result content verified")
}

func (r *harnessRuntime) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if r.harness != "" {
		if out, err := r.call(ctx, "harness", "get", r.harness); err == nil {
			resources := harnessObject(harnessObject(out.Data["Harness"])["ResourceInfo"])
			if tool := harnessText(resources["ToolId"]); tool != "" {
				r.tool = tool
			}
			if deployment := harnessText(resources["DeploymentId"]); deployment != "" {
				r.deploy = deployment
			}
		}
	}
	if r.tool != "" {
		if _, err := r.instanceIDs(ctx); err != nil {
			r.t.Error("cleanup could not enumerate owned instances:", err)
		}
	}
	var ids []string
	for id := range r.instances {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		args := append([]string{"instance", "delete"}, ids...)
		if _, err := r.call(ctx, append(args, "--yes", "--wait", "--ignore-not-found")...); err != nil {
			r.t.Error("cleanup could not stop owned instances:", err)
		}
		if out, err := r.call(ctx, "instance", "list", "--tool-id", r.tool, "--limit", "100"); err != nil {
			r.t.Error("cleanup could not verify stopped instances:", err)
		} else {
			for _, item := range runtimeItems(out.Data["Items"]) {
				if r.instances[harnessText(item["InstanceId"])] && harnessText(item["Status"]) != "STOPPED" {
					r.t.Error("cleanup left an owned instance active")
				}
			}
		}
	}
	for _, item := range []struct{ command, id string }{{"harness", r.harness}, {"tool", r.tool}, {"model-provider", r.provider}} {
		if item.id == "" {
			continue
		}
		args := []string{item.command, "delete", item.id, "--yes"}
		if item.command != "model-provider" {
			args = append(args, "--wait")
		}
		out, err := r.call(ctx, args...)
		if err != nil && (out.Failure == nil || out.Failure.Kind != "not_found") {
			r.t.Errorf("cleanup %s: %v", item.command, err)
		}
	}
	for _, item := range []struct{ command, id string }{{"harness", r.harness}, {"tool", r.tool}, {"deployment", r.deploy}, {"model-provider", r.provider}} {
		if item.id != "" {
			out, err := r.call(ctx, item.command, "get", item.id)
			if err == nil || out.Failure == nil || out.Failure.Kind != "not_found" {
				r.t.Errorf("cleanup could not confirm %s deletion", item.command)
			}
		}
	}
	r.t.Logf("cleanup checked %d owned instances and bound resources", len(ids))
}
