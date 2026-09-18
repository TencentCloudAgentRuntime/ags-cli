package patchscenarios

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// volumeFaults must each break the registered scenario. A fault that still
// passes means the corresponding assertion is not real evidence.
var volumeFaults = []string{
	"",
	"ignore-volume-ids", "ignore-volume-names", "ignore-volume-filters",
	"ignore-volume-offset", "ignore-volume-limit",
	"ignore-template-ids", "ignore-template-names", "ignore-template-filters",
	"ignore-template-offset", "ignore-template-limit",
	"ignore-client-token", "drop-volume-tags", "drop-template-update", "ignore-update-id",
	"drop-volume-storage", "drop-template-spec", "drop-template-link",
	"drop-storage-role", "drop-cbs-capacity", "drop-timestamps",
	"drop-reclaim-policy", "report-mounted-instance", "report-capacity",
	"retain-resource", "drop-mount-volume", "drop-mount-volume-id",
	"drop-mount-template", "drop-mount-reuse-key",
}

// The same registered scenario runs against the real candidate CLI here and
// against the service in the strict patch runner. This fixture is not live evidence.
func TestVolumeLifecycleCandidate(t *testing.T) {
	root := filepath.Clean("../..")
	binary := filepath.Join(t.TempDir(), "agr-preview")
	build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-tags=preview", "-o", binary, "./cmd/agr")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, fault := range volumeFaults {
		t.Run("fault="+fault, func(t *testing.T) {
			volumeStorageEnv(t)
			fixture := newVolumeFixture(t, fault)
			server := httptest.NewTLSServer(fixture)
			defer server.Close()
			env := volumeEnv(t, server.URL)
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: "volume.lifecycle", Assertions: volumeAssertions}}}
			results, err := patchtest.Run(t.Context(), plan, Registry, binary, env)
			if (err != nil) != (fault != "") {
				t.Fatalf("fault %q: err=%v results=%+v", fault, err, results)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fault != "retain-resource" && (len(fixture.volumes) > 1 || len(fixture.templates) > 1 || len(fixture.tools) != 0) {
				t.Fatalf("owned resources leaked: volumes=%d templates=%d tools=%d", len(fixture.volumes), len(fixture.templates), len(fixture.tools))
			}
			if fault == "" && (len(results) != 1 || results[0].Cleanup != "pass" || results[0].Calls < 60) {
				t.Fatalf("insufficient execution: %+v", results)
			}
		})
	}
	for _, missing := range volumeStorageVars {
		t.Run("missing="+missing, func(t *testing.T) {
			volumeStorageEnv(t)
			t.Setenv(missing, "")
			fixture := newVolumeFixture(t, "")
			server := httptest.NewTLSServer(fixture)
			defer server.Close()
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{{Scenario: "volume.lifecycle", Assertions: volumeAssertions}}}
			if _, err := patchtest.Run(t.Context(), plan, Registry, binary, volumeEnv(t, server.URL)); err == nil {
				t.Fatalf("missing %s produced a passing run", missing)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.calls != 0 {
				t.Fatalf("scenario contacted the service without %s: %d calls", missing, fixture.calls)
			}
		})
	}
	t.Run("channel-isolation", func(t *testing.T) {
		stable := filepath.Join(t.TempDir(), "agr-stable")
		build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", stable, "./cmd/agr")
		build.Dir = root
		build.Env = append(os.Environ(), "GOFLAGS=")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build stable: %v\n%s", err, out)
		}
		for _, group := range []string{"volume", "volume-template"} {
			if out, err := exec.CommandContext(t.Context(), stable, group, "--help").CombinedOutput(); err == nil {
				t.Fatalf("stable exposes %s: %s", group, out)
			}
			if out, err := exec.CommandContext(t.Context(), binary, group, "--help").CombinedOutput(); err != nil {
				t.Fatalf("preview omits %s: %v %s", group, err, out)
			}
		}
		// SourceVolumeId is disabled: neither channel may advertise it, and the
		// mount references must only appear in the preview skeleton.
		out, err := exec.CommandContext(t.Context(), binary, "volume", "create", "--help").CombinedOutput()
		if err != nil || strings.Contains(string(out), "source-volume-id") {
			t.Fatalf("disabled SourceVolumeId exposed: %v %s", err, out)
		}
		for _, tc := range []struct {
			binary string
			want   bool
		}{{binary, true}, {stable, false}} {
			out, err := exec.CommandContext(t.Context(), tc.binary, "tool", "create", "--generate-skeleton").CombinedOutput()
			if err != nil {
				t.Fatalf("skeleton: %v %s", err, out)
			}
			for _, reference := range []string{`"VolumeTemplateName"`, `"ReuseKey"`} {
				if strings.Contains(string(out), reference) != tc.want {
					t.Fatalf("mount reference %s present=%v, want %v: %s", reference, !tc.want, tc.want, out)
				}
			}
		}
	})
}

var volumeStorageVars = []string{
	"AGR_VOLUME_COS_ENDPOINT", "AGR_VOLUME_COS_BUCKET", "AGR_VOLUME_CFS_FILESYSTEM_ID",
	"AGR_VOLUME_STORAGE_ROLE_ARN", "AGR_VOLUME_TOOL_TYPE", "AGR_VOLUME_TOOL_ROLE_ARN",
}

// volumeStorageEnv configures the scenario process, not the candidate CLI: the
// storage identifiers are read by the runner before any command is invoked.
func volumeStorageEnv(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{
		"AGR_VOLUME_COS_ENDPOINT":      "fixture.cos.ap-guangzhou.myqcloud.com",
		"AGR_VOLUME_COS_BUCKET":        "fixture-bucket",
		"AGR_VOLUME_CFS_FILESYSTEM_ID": "cfs-fixture",
		"AGR_VOLUME_STORAGE_ROLE_ARN":  "qcs::cam::uin/100000000001:roleName/AGSFixtureStorageRole",
		"AGR_VOLUME_TOOL_TYPE":         "browser",
		"AGR_VOLUME_TOOL_ROLE_ARN":     "qcs::cam::uin/100000000001:roleName/AGSFixtureRole",
	} {
		t.Setenv(name, value)
	}
}

func volumeEnv(t *testing.T, serverURL string) []string {
	t.Helper()
	return []string{
		"HOME=" + t.TempDir(),
		"PATH=" + os.Getenv("PATH"),
		"TENCENTCLOUD_SECRET_ID=fake",
		"TENCENTCLOUD_SECRET_KEY=fake",
		"TENCENTCLOUD_TOKEN=fake-session",
		"AGR_REGION=ap-guangzhou",
		"AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(serverURL, "https://"),
		"AGR_INSECURE_SKIP_VERIFY=1",
	}
}

type volumeFixture struct {
	t         *testing.T
	mu        sync.Mutex
	fault     string
	calls     int
	sequence  int
	volumes   map[string]map[string]any
	templates map[string]map[string]any
	tools     map[string]map[string]any
	tokens    map[string]string
}

func newVolumeFixture(t *testing.T, fault string) *volumeFixture {
	fixture := &volumeFixture{
		t:         t,
		fault:     fault,
		volumes:   map[string]map[string]any{},
		templates: map[string]map[string]any{},
		tools:     map[string]map[string]any{},
		tokens:    map[string]string{},
	}
	// Account resources the scenario does not own must not satisfy any filter
	// or pagination assertion.
	fixture.volumes["vol-preexisting"] = map[string]any{
		"VolumeId": "vol-preexisting", "VolumeName": "preexisting", "Status": "ACTIVE",
		"AccessMode": "ReadWriteOnce", "StorageType": "Cos",
	}
	fixture.templates["volt-preexisting"] = map[string]any{
		"VolumeTemplateId": "volt-preexisting", "VolumeTemplateName": "preexisting", "Status": "ACTIVE",
		"AccessMode": "ReadWriteOnce", "StorageType": "Cos",
	}
	return fixture
}

func (f *volumeFixture) nextID(prefix string) string {
	f.sequence++
	return fmt.Sprintf("%s-fixture%d", prefix, f.sequence)
}

func (f *volumeFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Token") != "fake-session" {
		f.t.Error("missing signed temporary credentials")
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Error(err)
		return
	}
	response := map[string]any{"RequestId": "fixture"}
	notFound := func() {
		response["Error"] = map[string]any{"Code": "ResourceNotFound.VolumeNotExist", "Message": "missing fixture"}
	}
	switch r.Header.Get("X-TC-Action") {
	case "CreateVolume":
		response["Volume"] = f.create(req, "vol", "VolumeId", "VolumeName", "Storage", f.volumes)
	case "DescribeVolumeList":
		set, total := f.list(req, f.volumes, "VolumeId", "VolumeName", "VolumeIds", "VolumeNames", "access-mode", "AccessMode", "volume")
		response["VolumeSet"], response["TotalCount"] = set, total
	case "UpdateVolume":
		stored := f.find(req, f.volumes, "VolumeId", "VolumeName")
		if stored == nil {
			notFound()
			break
		}
		if f.fault != "drop-volume-tags" && !f.ignoresIDSelector(req, "VolumeId") {
			maps.Copy(stored, req)
		}
	case "DeleteVolume":
		f.remove(req, f.volumes, "VolumeId", "VolumeName")
	case "CreateVolumeTemplate":
		response["VolumeTemplate"] = f.create(req, "volt", "VolumeTemplateId", "VolumeTemplateName", "StorageSpec", f.templates)
	case "DescribeVolumeTemplateList":
		set, total := f.list(req, f.templates, "VolumeTemplateId", "VolumeTemplateName", "VolumeTemplateIds", "VolumeTemplateNames", "storage-type", "StorageType", "template")
		response["VolumeTemplateSet"], response["TotalCount"] = set, total
	case "UpdateVolumeTemplate":
		stored := f.find(req, f.templates, "VolumeTemplateId", "VolumeTemplateName")
		if stored == nil {
			notFound()
			break
		}
		if f.fault != "drop-template-update" && !f.ignoresIDSelector(req, "VolumeTemplateId") {
			maps.Copy(stored, req)
		}
	case "DeleteVolumeTemplate":
		f.remove(req, f.templates, "VolumeTemplateId", "VolumeTemplateName")
	case "CreateSandboxTool":
		id := f.nextID("tool")
		tool := maps.Clone(req)
		tool["ToolId"], tool["Status"] = id, "ACTIVE"
		tool["StorageMounts"] = f.mounts(req["StorageMounts"])
		f.tools[id] = tool
		response["ToolId"] = id
	case "DescribeSandboxToolList":
		var set []any
		for _, id := range fixtureStrings(req["ToolIds"]) {
			if tool, ok := f.tools[id]; ok {
				set = append(set, tool)
			}
		}
		response["SandboxToolSet"], response["TotalCount"] = set, len(set)
	case "DeleteSandboxTool":
		id, _ := req["ToolId"].(string)
		delete(f.tools, id)
	default:
		f.t.Errorf("unexpected action %q", r.Header.Get("X-TC-Action"))
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"Response": response}); err != nil {
		f.t.Error(err)
	}
}

// create models client-token idempotency and the stored resource projection.
func (f *volumeFixture) create(req map[string]any, prefix, idField, nameField, storageField string, store map[string]map[string]any) map[string]any {
	if token, ok := req["ClientToken"].(string); ok && token != "" && f.fault != "ignore-client-token" {
		if id, seen := f.tokens[token]; seen {
			return store[id]
		}
	}
	id := f.nextID(prefix)
	created := maps.Clone(req)
	delete(created, "ClientToken")
	created[idField], created["Status"] = id, "ACTIVE"
	created["CreatedAt"], created["UpdatedAt"] = "2026-09-18T00:00:00Z", "2026-09-18T00:00:00Z"
	if idField == "VolumeId" {
		// A standalone volume keeps its storage until it is deleted explicitly,
		// and AgentCbs is the only backend that reports a capacity of its own.
		created["ReclaimPolicy"] = "Retain"
		if capacity := volumeString(volumeNested(created, storageField, "AgentCbs"), "Capacity"); capacity != "" {
			created["Capacity"] = capacity
		}
	}
	switch f.fault {
	case "drop-volume-storage":
		if storageField == "Storage" {
			delete(created, storageField)
		}
	case "drop-template-spec":
		if storageField == "StorageSpec" {
			delete(created, storageField)
		}
	case "drop-template-link":
		// A standalone volume must never report a template linkage.
		if idField == "VolumeId" {
			created["VolumeTemplateId"], created["ReuseKey"] = "volt-bogus", "bogus"
		}
	case "drop-storage-role":
		delete(created, "StorageRoleArn")
	case "drop-cbs-capacity":
		if storage := volumeObject(created[storageField]); storage["AgentCbs"] != nil {
			delete(created, "Capacity")
			delete(created, "DefaultCapacity")
			created[storageField] = map[string]any{"AgentCbs": map[string]any{}}
		}
	case "drop-timestamps":
		delete(created, "CreatedAt")
		delete(created, "UpdatedAt")
	case "drop-reclaim-policy":
		if idField == "VolumeId" {
			delete(created, "ReclaimPolicy")
		}
	case "report-mounted-instance":
		if idField == "VolumeId" {
			created["MountedInstanceId"] = "rd2xjhpjrs7qqdbg37dwxu7nfdqltvbo"
		}
	case "report-capacity":
		if idField == "VolumeId" && created["Capacity"] == nil {
			created["Capacity"] = "20Gi"
		}
	}
	store[id] = created
	if token, ok := req["ClientToken"].(string); ok && token != "" {
		f.tokens[token] = id
	}
	return created
}

// ignoresIDSelector drops updates addressed by ID, leaving the name selector
// working, so only the by-ID assertions can notice.
func (f *volumeFixture) ignoresIDSelector(req map[string]any, idField string) bool {
	id, _ := req[idField].(string)
	return f.fault == "ignore-update-id" && id != ""
}

func (f *volumeFixture) find(req map[string]any, store map[string]map[string]any, idField, nameField string) map[string]any {
	if id, ok := req[idField].(string); ok && id != "" {
		return store[id]
	}
	name, _ := req[nameField].(string)
	for _, stored := range store {
		if stored[nameField] == name {
			return stored
		}
	}
	return nil
}

func (f *volumeFixture) remove(req map[string]any, store map[string]map[string]any, idField, nameField string) {
	if f.fault == "retain-resource" {
		return
	}
	stored := f.find(req, store, idField, nameField)
	if stored == nil {
		return
	}
	id, _ := stored[idField].(string)
	delete(store, id)
}

// list applies the documented filter and pagination semantics; each fault turns
// exactly one of them into a no-op.
func (f *volumeFixture) list(req map[string]any, store map[string]map[string]any, idField, nameField, idsField, namesField, filterName, filterField, kind string) ([]any, int) {
	var rows []any
	for _, stored := range fixtureSorted(store, idField) {
		if ids := fixtureStrings(req[idsField]); len(ids) > 0 && f.fault != "ignore-"+kind+"-ids" {
			if !fixtureContains(ids, stored[idField]) {
				continue
			}
		}
		if names := fixtureStrings(req[namesField]); len(names) > 0 && f.fault != "ignore-"+kind+"-names" {
			if !fixtureContains(names, stored[nameField]) {
				continue
			}
		}
		if filters, ok := req["Filters"].([]any); ok && f.fault != "ignore-"+kind+"-filters" {
			matched := true
			for _, raw := range filters {
				filter, _ := raw.(map[string]any)
				if filter["Name"] != filterName {
					f.t.Errorf("unexpected filter %v", filter["Name"])
					continue
				}
				if !fixtureContains(fixtureStrings(filter["Values"]), stored[filterField]) {
					matched = false
				}
			}
			if !matched {
				continue
			}
		}
		rows = append(rows, stored)
	}
	total := len(rows)
	if offset, ok := req["Offset"].(float64); ok && f.fault != "ignore-"+kind+"-offset" {
		rows = rows[min(int(offset), len(rows)):]
	}
	if limit, ok := req["Limit"].(float64); ok && limit > 0 && f.fault != "ignore-"+kind+"-limit" {
		rows = rows[:min(int(limit), len(rows))]
	}
	return rows, total
}

// mounts models the wire projection of the patched StorageMount members.
func (f *volumeFixture) mounts(value any) []any {
	items, _ := value.([]any)
	out := make([]any, 0, len(items))
	for _, item := range items {
		mount := maps.Clone(volumeObject(item))
		switch f.fault {
		case "drop-mount-volume":
			delete(mount, "Volume")
		case "drop-mount-volume-id":
			if reference, ok := mount["Volume"].(map[string]any); ok && reference["VolumeId"] != nil {
				mount["Volume"] = map[string]any{}
			}
		case "drop-mount-template":
			delete(mount, "VolumeTemplate")
		case "drop-mount-reuse-key":
			if reference, ok := mount["VolumeTemplate"].(map[string]any); ok {
				reference = maps.Clone(reference)
				delete(reference, "ReuseKey")
				mount["VolumeTemplate"] = reference
			}
		}
		out = append(out, mount)
	}
	return out
}

func fixtureSorted(store map[string]map[string]any, idField string) []map[string]any {
	ids := make([]string, 0, len(store))
	for id := range store {
		ids = append(ids, id)
	}
	// Stable order keeps one-per-page assertions deterministic.
	slices.Sort(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, store[id])
	}
	return out
}

func fixtureStrings(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func fixtureContains(values []string, candidate any) bool {
	text, _ := candidate.(string)
	for _, value := range values {
		if value == text {
			return true
		}
	}
	return false
}
