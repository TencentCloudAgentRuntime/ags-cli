package apimeta_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func TestKebabCase(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"ToolId":               "tool-id",
		"ToolName":             "tool-name",
		"StorageMounts":        "storage-mounts",
		"CustomConfiguration":  "custom-configuration",
		"NetworkConfiguration": "network-configuration",
		"ClientToken":          "client-token",
		"VPCConfig":            "vpc-config",
		"CLSConfig":            "cls-config",
		"APIKeyInfo":           "api-key-info",
		"ImageDigest":          "image-digest",
	}
	for in, want := range cases {
		if got := apimeta.KebabCase(in); got != want {
			t.Errorf("KebabCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadAndSortedActionNames(t *testing.T) {
	root := repoRoot(t)
	spec, err := apimeta.LoadSpec(filepath.Join(root, "api", "ags", "v20250920", "api.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	names := spec.SortedActionNames()
	if len(names) == 0 {
		t.Fatalf("expected actions, got 0")
	}
	if !sortedAscending(names) {
		t.Fatalf("SortedActionNames not sorted: %v", names)
	}
	for _, name := range names {
		a, ok := spec.Actions[name]
		if !ok {
			t.Fatalf("action %s missing", name)
		}
		if a.Input == "" {
			t.Errorf("action %s has empty input", name)
		}
		if spec.Object(a.Input) == nil {
			t.Errorf("action %s input %s does not exist", name, a.Input)
		}
	}
}

func TestPublishedOSWorldAndTokenPaginationContract(t *testing.T) {
	root := repoRoot(t)
	spec, err := apimeta.LoadSpec(filepath.Join(root, "api", "ags", "v20250920", "api.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	hasMember := func(object *apimeta.Object, name string) bool {
		return object != nil && slices.ContainsFunc(object.Members, func(member apimeta.Member) bool {
			return member.Name == name
		})
	}

	computer := spec.Object("ComputerConfiguration")
	osWorld := spec.Object("OSWorldConfiguration")
	if !hasMember(computer, "OSWorldConfiguration") {
		t.Fatal("ComputerConfiguration is missing OSWorldConfiguration")
	}
	if !hasMember(osWorld, "Version") {
		t.Fatal("OSWorldConfiguration is missing Version")
	}

	request := spec.Object("DescribeSandboxInstanceListRequest")
	for _, name := range []string{"MaxResults", "NextToken", "NeedTotalCount"} {
		if !hasMember(request, name) {
			t.Fatalf("DescribeSandboxInstanceListRequest is missing %s", name)
		}
	}
	response := spec.Object("DescribeSandboxInstanceListResponse")
	if !hasMember(response, "NextToken") {
		t.Fatal("DescribeSandboxInstanceListResponse is missing NextToken")
	}
}

func TestScalarFlagType(t *testing.T) {
	cases := []struct {
		m    apimeta.Member
		want string
	}{
		{apimeta.Member{Type: "string"}, "string"},
		{apimeta.Member{Type: "bool"}, "bool"},
		{apimeta.Member{Type: "int"}, "int64"},
		{apimeta.Member{Type: "list", Member: "string"}, "json"},
		{apimeta.Member{Type: "object", Member: "MountOption"}, "json"},
	}
	for _, c := range cases {
		if got := apimeta.ScalarFlagType(c.m); got != c.want {
			t.Errorf("ScalarFlagType(%v) = %s, want %s", c.m, got, c.want)
		}
	}
}

func sortedAscending(names []string) bool {
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			return false
		}
	}
	return true
}

func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..")
}
