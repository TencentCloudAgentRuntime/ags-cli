package apimeta_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func loadInputs(t *testing.T) (*apimeta.Spec, *apimeta.Mapping) {
	t.Helper()
	root := filepath.Join("..", "..")
	spec, err := apimeta.LoadEffectiveSpec(filepath.Join(root, "api", "ags", "v20250920"))
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	mapping, err := apimeta.LoadMapping(filepath.Join(root, "api", "ags", "v20250920", "mapping.yaml"))
	if err != nil {
		t.Fatalf("load mapping: %v", err)
	}
	return spec, mapping
}

func TestBuildCatalog_DeterministicOutputs(t *testing.T) {
	spec, mapping := loadInputs(t)
	a, err := json.Marshal(apimeta.BuildCatalog(spec, mapping))
	if err != nil {
		t.Fatalf("first marshal: %v", err)
	}
	b, err := json.Marshal(apimeta.BuildCatalog(spec, mapping))
	if err != nil {
		t.Fatalf("second marshal: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("generated metadata model differs between runs")
	}
}

func TestGenerate_FlagsContainBaseLongFlags(t *testing.T) {
	spec, mapping := loadInputs(t)
	flags := apimeta.BuildFlags(spec, mapping)
	have := map[string]bool{}
	for _, f := range flags.Flags {
		have[f.Action+"|"+f.Field+"|"+f.Flag] = true
	}
	expectations := []struct{ action, field, flag string }{
		{"StartSandboxInstance", "ToolName", "tool-name"},
		{"StartSandboxInstance", "ToolId", "tool-id"},
		{"StartSandboxInstance", "MountOptions", "mount-options"},
		{"PauseSandboxInstance", "Memory", "memory"},
		{"ResumeSandboxInstance", "Timeout", "timeout"},
		{"CreateSandboxTool", "StorageMounts", "storage-mounts"},
		{"CreateSandboxTool", "NetworkConfiguration", "network-configuration"},
		{"CreatePreCacheImageTask", "Image", "image"},
	}
	for _, e := range expectations {
		key := e.action + "|" + e.field + "|" + e.flag
		if !have[key] {
			t.Errorf("missing generated flag: %s -> --%s", e.field, e.flag)
		}
	}
}

func TestSpecIncludesLatestStorageContract(t *testing.T) {
	spec, _ := loadInputs(t)
	tests := []struct {
		object string
		field  string
		typeID string
		member string
	}{
		{object: "ResourceConfiguration", field: "Storage", typeID: "string", member: "string"},
		{object: "StorageSource", field: "AgentBucket", typeID: "object", member: "AgentBucketStorageSource"},
		{object: "AgentBucketStorageSource", field: "LibraryId", typeID: "string", member: "string"},
		{object: "AgentBucketStorageSource", field: "SpaceId", typeID: "string", member: "string"},
		{object: "AgentBucketStorageSource", field: "AccessDomain", typeID: "string", member: "string"},
	}
	for _, tt := range tests {
		t.Run(tt.object+"."+tt.field, func(t *testing.T) {
			object := spec.Object(tt.object)
			if object == nil {
				t.Fatalf("object %s is missing", tt.object)
			}
			for _, member := range object.Members {
				if member.Name != tt.field {
					continue
				}
				if member.Type != tt.typeID || member.Member != tt.member {
					t.Fatalf("%s.%s type=%s member=%s", tt.object, tt.field, member.Type, member.Member)
				}
				return
			}
			t.Fatalf("field %s.%s is missing", tt.object, tt.field)
		})
	}
}

func TestGenerate_CoverageMatchesMappingStats(t *testing.T) {
	spec, mapping := loadInputs(t)
	rep := apimeta.BuildCoverage(spec, mapping)
	if rep.TotalActions != len(spec.Actions) {
		t.Errorf("TotalActions=%d", rep.TotalActions)
	}
	if rep.MappedActions+rep.RawOnlyActions+rep.DeferredActions != rep.TotalActions {
		t.Errorf("status counts do not add up: %+v", rep)
	}
	if len(rep.UnmappedActions) > 0 {
		t.Errorf("expected no unmapped actions, got %v", rep.UnmappedActions)
	}
}

func TestBuildCatalog_IncludesRawOnlyReason(t *testing.T) {
	spec, mapping := loadInputs(t)
	data, err := json.Marshal(apimeta.BuildCatalog(spec, mapping))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var cat map[string]any
	if err := json.Unmarshal(data, &cat); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	actions, _ := cat["Actions"].([]any)
	foundRaw := false
	for _, a := range actions {
		m := a.(map[string]any)
		if m["Status"] == "raw_only" {
			foundRaw = true
			if r, _ := m["Reason"].(string); r == "" {
				t.Errorf("raw_only action missing Reason: %v", m)
			}
		}
	}
	if !foundRaw {
		t.Fatalf("expected at least one raw_only action")
	}
}
