package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func storageMountServer(t *testing.T) (*httptest.Server, chan map[string]any) {
	t.Helper()
	requests := make(chan map[string]any, 4)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Version") != "2025-09-20" {
			t.Error("missing signature or wrong API version")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		action := r.Header.Get("X-TC-Action")
		if action == "StartSandboxInstance" || action == "UpdateSandboxTool" {
			requests <- payload
		}
		response := map[string]any{"RequestId": "storage-mount-test"}
		switch action {
		case "StartSandboxInstance":
			response["Instance"] = map[string]any{"InstanceId": "ins-test", "Status": "RUNNING"}
		case "UpdateSandboxTool":
		case "DescribeSandboxToolList":
			response["SandboxToolSet"] = []any{map[string]any{"ToolId": "sdt-source", "ToolName": "source", "Status": "ACTIVE"}}
		case "AcquireSandboxInstanceToken":
			response["Token"] = "token-test"
			response["ExpiresAt"] = "2030-01-01T00:00:00Z"
		default:
			t.Errorf("unexpected action %s", action)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Response": response})
	}))
	t.Cleanup(server.Close)
	return server, requests
}

// The image mount is the only mount form whose reference, type, and SubPath can
// change through `tool update`. Explicit empty and omitted SubPath must stay
// distinguishable on the wire, and clone/restore starts carry the same shape.
func TestStorageMountsSubPathWireShape(t *testing.T) {
	const explicit = `[{"Name":"runtime","StorageSource":{"Image":{"Reference":"ccr.ccs.tencentyun.com/agr/image:v1","ImageRegistryType":"personal","SubPath":""}}}]`
	const directory = `[{"Name":"runtime","StorageSource":{"Image":{"Reference":"ccr.ccs.tencentyun.com/agr/image:v1","ImageRegistryType":"personal","SubPath":"usr/bin"}}}]`
	const omitted = `[{"Name":"runtime","StorageSource":{"Image":{"Reference":"ccr.ccs.tencentyun.com/agr/image:v1","ImageRegistryType":"personal"}}}]`
	for _, tc := range []struct {
		name     string
		args     []string
		mounts   string
		wantKey  bool
		wantPath string
	}{
		{"tool update directory", []string{"tool", "update", "sdt-source"}, directory, true, "usr/bin"},
		{"tool update explicit empty", []string{"tool", "update", "sdt-source"}, explicit, true, ""},
		{"tool update omitted", []string{"tool", "update", "sdt-source"}, omitted, false, ""},
		{"tool update request explicit empty", []string{"tool", "update", "--request", `{"ToolId":"sdt-source","StorageMounts":` + explicit + `}`}, "", true, ""},
		{"instance create clone", []string{"instance", "create", "--source-instance-id", "ins-source"}, explicit, true, ""},
		{"instance create restore", []string{"instance", "create", "--snapshot-id", "snapshot-source"}, directory, true, "usr/bin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, requests := storageMountServer(t)
			args := slices.Clone(tc.args)
			if tc.mounts != "" {
				args = append(args, "--storage-mounts", tc.mounts)
			}
			out, stderr, err := runSnapshotCLI(t, server, "", args...)
			if err != nil {
				t.Fatalf("CLI: %v; stdout=%s stderr=%s", err, out, stderr)
			}
			select {
			case payload := <-requests:
				image := firstImageMount(t, payload)
				got, present := image["SubPath"]
				if present != tc.wantKey || (tc.wantKey && got != tc.wantPath) {
					t.Fatalf("SubPath = %v (present %v), want %q (present %v): %#v", got, present, tc.wantPath, tc.wantKey, image)
				}
			default:
				t.Fatalf("no %s request", args[0])
			}
		})
	}
}

func firstImageMount(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	mounts, ok := payload["StorageMounts"].([]any)
	if !ok || len(mounts) != 1 {
		t.Fatalf("StorageMounts = %#v", payload["StorageMounts"])
	}
	mount, _ := mounts[0].(map[string]any)
	source, _ := mount["StorageSource"].(map[string]any)
	image, ok := source["Image"].(map[string]any)
	if !ok {
		t.Fatalf("StorageSource.Image missing: %#v", mount)
	}
	return image
}

func TestToolUpdateRejectsImmutableMountFields(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "rejected input reached the transport", http.StatusInternalServerError)
	}))
	defer server.Close()
	for _, tc := range []struct {
		name  string
		args  []string
		mount string
	}{
		{"mount path flag", []string{"tool", "update", "sdt-source"}, `[{"Name":"runtime","MountPath":"/mnt/runtime","StorageSource":{"Image":{"Reference":"repo/img:v1","ImageRegistryType":"personal","SubPath":"usr/bin"}}}]`},
		{"read only false flag", []string{"tool", "update", "sdt-source"}, `[{"Name":"runtime","ReadOnly":false,"StorageSource":{"Image":{"Reference":"repo/img:v1","ImageRegistryType":"personal","SubPath":"usr/bin"}}}]`},
		{"read only true request", []string{"tool", "update", "--request", `{"ToolId":"sdt-source","StorageMounts":[{"Name":"runtime","ReadOnly":true,"StorageSource":{"Image":{"Reference":"repo/img:v1","ImageRegistryType":"personal","SubPath":"usr/bin"}}}]}`}, ""},
		{"mount path request", []string{"tool", "update", "--request", `{"ToolId":"sdt-source","StorageMounts":[{"Name":"runtime","MountPath":"/mnt/runtime"}]}`}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls.Store(0)
			args := slices.Clone(tc.args)
			if tc.mount != "" {
				args = append(args, "--storage-mounts", tc.mount)
			}
			out, stderr, err := runSnapshotCLI(t, server, "", args...)
			var envelope struct{ Failure struct{ Code string } }
			if err == nil || json.Unmarshal(out, &envelope) != nil || envelope.Failure.Code != "INVALID_REQUEST_JSON" {
				t.Fatalf("unexpected result: %s %s (%v)", out, stderr, err)
			}
			failure := string(out)
			if !strings.Contains(failure, "cannot be changed by tool update") || !strings.Contains(failure, "MountPath") && !strings.Contains(failure, "ReadOnly") {
				t.Fatalf("failure does not explain the immutable field: %s", out)
			}
			if calls.Load() != 0 {
				t.Fatal("invalid mount caused an API call")
			}
		})
	}
}

func TestToolUpdateKeepsVolumeOnlyMounts(t *testing.T) {
	server, requests := storageMountServer(t)
	const mounts = `[{"Name":"workspace","Volume":{"VolumeId":"vol-test"}},{"Name":"runtime","StorageSource":{"Image":{"Reference":"repo/img:v1","ImageRegistryType":"personal","SubPath":"usr/bin"}}}]`
	if _, stderr, err := runSnapshotCLI(t, server, "", "tool", "update", "sdt-source", "--storage-mounts", mounts); err != nil {
		t.Fatalf("CLI: %v; stderr=%s", err, stderr)
	}
	select {
	case payload := <-requests:
		var want []any
		if err := json.Unmarshal([]byte(mounts), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(payload["StorageMounts"], want) {
			t.Fatalf("StorageMounts = %#v, want %#v", payload["StorageMounts"], want)
		}
	default:
		t.Fatal("no UpdateSandboxTool request")
	}
}

func TestStorageMountsHelpAndSchema(t *testing.T) {
	for _, tc := range []struct {
		command   string
		fragments []string
	}{
		{
			command: "instance.create",
			fragments: []string{
				"StorageMounts as a JSON array, @file, or - for stdin",
				"Each entry declares a mount available when the instance starts",
				"MountOptions only adjusts mounts the tool already declares",
				"SubPath",
			},
		},
		{
			command: "tool.update",
			fragments: []string{
				"Omit --storage-mounts or pass [] to keep the current mounts",
				"ImageRegistryType: personal or enterprise; required with Image",
				"SubPath: omitted keeps the current directory, an empty string switches to the image root, a non-empty string switches to that directory",
				"MountPath and ReadOnly belong to the tool configuration",
			},
		},
	} {
		t.Run(tc.command, func(t *testing.T) {
			schema := schemaForCommand(t, tc.command)
			requireCliFlag(t, schema, "StorageMounts", "storage-mounts")
			requirePropertyType(t, schema, "StorageMounts", "array")
			flag := schema.Flags["storage-mounts"]
			if flag.Type != "json" || len(flag.Examples) == 0 {
				t.Fatalf("storage-mounts schema = %+v", flag)
			}
			documented := flag.Description + "\n" + flag.Format + "\n" + strings.Join(flag.Values, "\n")
			cmd, ok := findCobraCommand(contractRoot(), tc.command)
			if !ok {
				t.Fatalf("%s command missing", tc.command)
			}
			help := commandHelp(t, cmd)
			for _, fragment := range tc.fragments {
				if !strings.Contains(documented, fragment) {
					t.Errorf("%s schema missing %q: %s", tc.command, fragment, documented)
				}
				if !strings.Contains(help, fragment) {
					t.Errorf("%s help missing %q", tc.command, fragment)
				}
			}
			prefix := "agr " + strings.ReplaceAll(tc.command, ".", " ") + " "
			for _, example := range flag.Examples {
				if !strings.HasPrefix(example, prefix) || !strings.Contains(example, "--storage-mounts") {
					t.Errorf("%s example %q does not use the command", tc.command, example)
				}
				if !strings.Contains(help, example) {
					t.Errorf("%s help missing example %q", tc.command, example)
				}
			}
		})
	}
}

// TestInstanceCreateMountSourcesStayInThePublicContract keeps instance start
// help and schema on the sources the service accepts. A start request rejects
// VolumeTemplate and AgentBucket, so recommending them sends agents into
// InvalidParameterValue.StorageMount.
func TestInstanceCreateMountSourcesStayInThePublicContract(t *testing.T) {
	sources := schemaForCommand(t, "instance.create").Flags["storage-mounts"].Values
	documented := strings.Join(sources, "\n")
	for _, want := range []string{"Image:", "Cos:", "Cfs:", "Volume:"} {
		if !strings.Contains(documented, want) {
			t.Errorf("instance.create storage-mounts values miss %q: %s", want, documented)
		}
	}
	cmd, ok := findCobraCommand(contractRoot(), "instance.create")
	if !ok {
		t.Fatal("instance.create command missing")
	}
	help := commandHelp(t, cmd)
	for _, rejected := range []string{"VolumeTemplate", "AgentBucket"} {
		if strings.Contains(documented, rejected) {
			t.Errorf("instance.create storage-mounts values recommend %s: %s", rejected, documented)
		}
		if strings.Contains(help, rejected) {
			t.Errorf("instance create --help recommends %s:\n%s", rejected, help)
		}
	}
}
