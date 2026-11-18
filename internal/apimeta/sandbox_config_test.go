package apimeta_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

const sandboxContractDir = "../../api/ags/v20250920"

var sandboxRequests = []string{
	"CreateSandboxToolRequest",
	"UpdateSandboxToolRequest",
	"StartSandboxInstanceRequest",
	"UpdateSandboxInstanceRequest",
}

var sandboxResources = []string{"SandboxTool", "SandboxInstance"}

// The seven actions that carry the new configuration through the contract.
var sandboxActions = map[string][2]string{
	"CreateSandboxTool":           {"CreateSandboxToolRequest", "CreateSandboxToolResponse"},
	"UpdateSandboxTool":           {"UpdateSandboxToolRequest", "UpdateSandboxToolResponse"},
	"StartSandboxInstance":        {"StartSandboxInstanceRequest", "StartSandboxInstanceResponse"},
	"UpdateSandboxInstance":       {"UpdateSandboxInstanceRequest", "UpdateSandboxInstanceResponse"},
	"DescribeSandboxToolList":     {"DescribeSandboxToolListRequest", "DescribeSandboxToolListResponse"},
	"DescribeSandboxInstanceList": {"DescribeSandboxInstanceListRequest", "DescribeSandboxInstanceListResponse"},
	"DescribeSandboxInstances":    {"DescribeSandboxInstancesRequest", "DescribeSandboxInstancesResponse"},
}

func loadSandboxContract(t *testing.T, channel apimeta.Channel) *apimeta.Contract {
	t.Helper()
	contract, err := apimeta.LoadContract(sandboxContractDir, channel)
	if err != nil {
		t.Fatalf("load %s contract: %v", channel, err)
	}
	return contract
}

func requireMember(t *testing.T, spec *apimeta.Spec, object, name string) apimeta.Member {
	t.Helper()
	parent := spec.Object(object)
	if parent == nil {
		t.Fatalf("object %s is missing", object)
	}
	for _, member := range parent.Members {
		if member.Name == name {
			return member
		}
	}
	t.Fatalf("%s is missing member %s", object, name)
	return apimeta.Member{}
}

func requireNoMember(t *testing.T, spec *apimeta.Spec, object, name string) {
	t.Helper()
	parent := spec.Object(object)
	if parent == nil {
		t.Fatalf("object %s is missing", object)
	}
	for _, member := range parent.Members {
		if member.Name == name {
			t.Fatalf("%s unexpectedly has member %s", object, name)
		}
	}
}

func TestSandboxPreviewContractAddsKeepaliveAndLogFormat(t *testing.T) {
	preview := loadSandboxContract(t, apimeta.Preview).Spec

	keepalive := preview.Object("KeepaliveConfiguration")
	if keepalive == nil {
		t.Fatal("KeepaliveConfiguration object is missing")
	}
	if keepalive.Type != "object" || keepalive.Usage != "both" {
		t.Fatalf("KeepaliveConfiguration type=%q usage=%q", keepalive.Type, keepalive.Usage)
	}
	if len(keepalive.Members) != 2 {
		t.Fatalf("KeepaliveConfiguration members=%v", keepalive.Members)
	}
	for _, name := range []string{"KeepaliveMode", "KeepaliveTimeout"} {
		member := requireMember(t, preview, "KeepaliveConfiguration", name)
		if member.Type != "string" || member.Member != "string" {
			t.Fatalf("%s type=%q member=%q", name, member.Type, member.Member)
		}
		if member.Required {
			t.Fatalf("%s must be optional", name)
		}
	}
	modeDoc := requireMember(t, preview, "KeepaliveConfiguration", "KeepaliveMode").Document
	for _, value := range []string{"NONE", "REQUEST", "CONNECTION", "MIXED"} {
		if !strings.Contains(modeDoc, value) {
			t.Fatalf("KeepaliveMode document is missing %s: %s", value, modeDoc)
		}
	}
	timeoutDoc := requireMember(t, preview, "KeepaliveConfiguration", "KeepaliveTimeout").Document
	for _, fragment := range []string{"300", "1800", "600s"} {
		if !strings.Contains(timeoutDoc, fragment) {
			t.Fatalf("KeepaliveTimeout document is missing %s: %s", fragment, timeoutDoc)
		}
	}
	if !strings.Contains(keepalive.Document, "继承") {
		t.Fatalf("KeepaliveConfiguration document does not describe inheritance: %s", keepalive.Document)
	}

	for _, request := range sandboxRequests {
		for _, name := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
			member := requireMember(t, preview, request, name)
			if member.Required {
				t.Fatalf("%s.%s must be optional", request, name)
			}
			switch name {
			case "KeepaliveConfiguration":
				if member.Type != "object" || member.Member != "KeepaliveConfiguration" {
					t.Fatalf("%s.%s type=%q member=%q", request, name, member.Type, member.Member)
				}
			default:
				if member.Type != "bool" || member.Member != "bool" {
					t.Fatalf("%s.%s type=%q member=%q", request, name, member.Type, member.Member)
				}
			}
		}
	}
	for _, resource := range sandboxResources {
		for _, name := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
			member := requireMember(t, preview, resource, name)
			if member.Required || member.Disabled {
				t.Fatalf("%s.%s must be an enabled output field", resource, name)
			}
		}
	}

	contentFormat := requireMember(t, preview, "LogConfiguration", "ContentFormat")
	if contentFormat.Type != "string" || contentFormat.Member != "string" || contentFormat.Required {
		t.Fatalf("ContentFormat = %+v", contentFormat)
	}
	for _, fragment := range []string{"raw", "auto", "TopicId"} {
		if !strings.Contains(contentFormat.Document, fragment) {
			t.Fatalf("ContentFormat document is missing %s: %s", fragment, contentFormat.Document)
		}
	}
}

func TestSandboxPreviewContractActionChain(t *testing.T) {
	preview := loadSandboxContract(t, apimeta.Preview).Spec
	for action, types := range sandboxActions {
		got, ok := preview.Actions[action]
		if !ok {
			t.Fatalf("action %s is missing", action)
		}
		if got.Input != types[0] || got.Output != types[1] {
			t.Fatalf("%s routes %s -> %s, want %s -> %s", action, got.Input, got.Output, types[0], types[1])
		}
	}
	requireMember(t, preview, "StartSandboxInstanceResponse", "Instance")
	requireMember(t, preview, "DescribeSandboxToolListResponse", "SandboxToolSet")
	requireMember(t, preview, "DescribeSandboxInstanceListResponse", "InstanceSet")
	requireMember(t, preview, "DescribeSandboxInstancesResponse", "InstanceSet")
	for _, response := range []string{
		"CreateSandboxToolResponse",
		"UpdateSandboxToolResponse",
		"StartSandboxInstanceResponse",
		"UpdateSandboxInstanceResponse",
	} {
		for _, name := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
			requireNoMember(t, preview, response, name)
		}
	}
}

func TestSandboxPreviewContractExcludesOutOfScopeMembers(t *testing.T) {
	preview := loadSandboxContract(t, apimeta.Preview).Spec
	for name, object := range preview.Objects {
		for _, member := range object.Members {
			if member.Name == "MaxKeepaliveDuration" {
				t.Fatalf("%s unexpectedly carries MaxKeepaliveDuration", name)
			}
		}
	}
	for _, object := range slices.Concat(sandboxRequests, sandboxResources) {
		requireNoMember(t, preview, object, "MaxKeepaliveDuration")
		requireNoMember(t, preview, object, "KeepaliveMode")
	}
	requireNoMember(t, preview, "UpdateSandboxToolRequest", "LogConfiguration")
	requireNoMember(t, preview, "StartSandboxInstanceRequest", "LogConfiguration")
}

func TestSandboxStableContractStaysUnchanged(t *testing.T) {
	stable := loadSandboxContract(t, apimeta.Stable).Spec
	if stable.Object("KeepaliveConfiguration") != nil {
		t.Fatal("stable contract exposes KeepaliveConfiguration")
	}
	for _, name := range slices.Concat(sandboxRequests, sandboxResources) {
		for _, member := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume"} {
			requireNoMember(t, stable, name, member)
		}
	}
	requireNoMember(t, stable, "LogConfiguration", "ContentFormat")
}

// TestSandboxPreviewContractMemberMetadata checks the raw effective document for
// metadata the typed model does not surface, so a resource member cannot ship as
// a required output or accept an explicit null.
func TestSandboxPreviewContractMemberMetadata(t *testing.T) {
	base, err := os.ReadFile(filepath.Join(sandboxContractDir, "api.json"))
	if err != nil {
		t.Fatal(err)
	}
	patch, err := os.ReadFile(filepath.Join(sandboxContractDir, "api.patch.json"))
	if err != nil {
		t.Fatal(err)
	}
	effective, err := apimeta.ApplyAPIPatch(base, patch)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Objects map[string]struct {
			Members []map[string]any `json:"members"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(effective, &document); err != nil {
		t.Fatal(err)
	}
	for _, object := range slices.Concat(sandboxRequests, sandboxResources, []string{"KeepaliveConfiguration"}) {
		for _, member := range document.Objects[object].Members {
			name, _ := member["name"].(string)
			if name == "ContentFormat" {
				name = object + ".ContentFormat"
			}
			switch name {
			case "KeepaliveConfiguration", "AutoPause", "AutoResume", "KeepaliveMode", "KeepaliveTimeout", "LogConfiguration.ContentFormat":
				if required, _ := member["required"].(bool); required {
					t.Fatalf("%s.%s must be optional", object, name)
				}
				if disabled, _ := member["disabled"].(bool); disabled {
					t.Fatalf("%s.%s must not be disabled", object, name)
				}
				if allowedNull, ok := member["value_allowed_null"].(bool); ok && allowedNull {
					t.Fatalf("%s.%s must not allow explicit null", object, name)
				}
				if name != "KeepaliveConfiguration" && name != "AutoPause" && name != "AutoResume" && name != "KeepaliveMode" && name != "KeepaliveTimeout" {
					continue
				}
				if outputRequired, ok := member["output_required"].(bool); ok && outputRequired {
					t.Fatalf("%s.%s must not be a required output", object, name)
				}
				if _, exists := member["default"]; exists {
					t.Fatalf("%s.%s must not carry a default", object, name)
				}
			}
		}
	}
}
