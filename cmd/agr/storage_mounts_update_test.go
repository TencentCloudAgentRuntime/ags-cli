package main

import (
	"encoding/json"
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
// distinguishable on the wire. Snapshot restores also carry the mount on the wire.
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

func TestInstanceCreateRejectsCloneStorageMounts(t *testing.T) {
	const mounts = `[{"Name":"runtime","MountPath":"/mnt/runtime","StorageSource":{"Image":{"Reference":"repo/img:v1","ImageRegistryType":"personal"}}}]`
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"flag", []string{"--source-instance-id", "ins-source", "--storage-mounts", mounts}},
		{"raw request", []string{"--request", `{"SourceInstanceId":"ins-source","StorageMounts":` + mounts + `}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Error(w, "clone request reached the transport", http.StatusInternalServerError)
			}))
			t.Cleanup(server.Close)
			out, stderr, err := runSnapshotCLI(t, server, "", append([]string{"instance", "create"}, tc.args...)...)
			if got := calls.Load(); got != 0 {
				t.Errorf("invalid clone made %d API requests, want zero", got)
			}
			var envelope struct{ Failure struct{ Code, Kind string } }
			if exitCodeOf(err) != 2 || json.Unmarshal(out, &envelope) != nil ||
				envelope.Failure.Code != "CONFLICTING_FLAGS" || envelope.Failure.Kind != "usage" {
				t.Fatalf("expected a usage error with exit code 2: %s %s (%v)", out, stderr, err)
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
		name        string
		args        []string
		mount       string
		wantMessage string
	}{
		{"mount path flag", []string{"tool", "update", "sdt-source"}, `[{"Name":"runtime","MountPath":"/mnt/runtime","StorageSource":{"Image":{"Reference":"repo/img:v1","ImageRegistryType":"personal","SubPath":"usr/bin"}}}]`, "StorageMounts[0].MountPath cannot be changed"},
		{"read only false flag", []string{"tool", "update", "sdt-source"}, `[{"Name":"runtime","ReadOnly":false,"StorageSource":{"Image":{"Reference":"repo/img:v1","ImageRegistryType":"personal","SubPath":"usr/bin"}}}]`, "StorageMounts[0].ReadOnly cannot be changed"},
		{"non-image mount path in raw request", []string{"tool", "update", "--request", `{"ToolId":"sdt-source","StorageMounts":[{"Name":"runtime","MountPath":"/mnt/runtime"}]}`}, "", "StorageMounts[0].MountPath cannot be changed"},
		{"wrong mount path type in raw request", []string{"tool", "update", "--request", `{"ToolId":"sdt-source","StorageMounts":[{"Name":"runtime","MountPath":false,"StorageSource":{"Image":{"Reference":"repo/img:v1","ImageRegistryType":"personal"}}}]}`}, "", "StorageMounts[0].MountPath: expected string"},
		{"wrong read only type in raw request", []string{"tool", "update", "--request", `{"ToolId":"sdt-source","StorageMounts":[{"Name":"runtime","ReadOnly":"false","StorageSource":{"Image":{"Reference":"repo/img:v1","ImageRegistryType":"personal"}}}]}`}, "", "StorageMounts[0].ReadOnly: expected bool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls.Store(0)
			args := slices.Clone(tc.args)
			if tc.mount != "" {
				args = append(args, "--storage-mounts", tc.mount)
			}
			out, stderr, err := runSnapshotCLI(t, server, "", args...)
			var envelope struct{ Failure struct{ Code string } }
			if exitCodeOf(err) != 2 || json.Unmarshal(out, &envelope) != nil || envelope.Failure.Code != "INVALID_REQUEST_JSON" {
				t.Fatalf("unexpected result: %s %s (%v)", out, stderr, err)
			}
			if !strings.Contains(string(out), tc.wantMessage) {
				t.Fatalf("failure did not explain the field error %q: %s", tc.wantMessage, out)
			}
			if calls.Load() != 0 {
				t.Fatal("invalid mount caused an API call")
			}
		})
	}
}

func TestToolUpdateReusesRawImageMount(t *testing.T) {
	const image = `{"Reference":"repo/img:v2","ImageRegistryType":"personal","SubPath":"","Digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	const raw = `{"ToolId":"sdt-source","Description":"kept","StorageMounts":[{"Name":"runtime","MountPath":"/mnt/different","ReadOnly":false,"StorageSource":{"Image":` + image + `}}]}`
	const forwarded = `{"ToolId":"sdt-source","Description":"kept","StorageMounts":[{"Name":"runtime","StorageSource":{"Image":` + image + `}}]}`
	var want map[string]any
	if err := json.Unmarshal([]byte(forwarded), &want); err != nil {
		t.Fatal(err)
	}
	requestFile := filepath.Join(t.TempDir(), "tool-update.json")
	if err := os.WriteFile(requestFile, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, request, stdin string }{
		{"inline", raw, ""},
		{"file", "@" + requestFile, ""},
		{"stdin", "-", raw},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, requests := storageMountServer(t)
			out, stderr, err := runSnapshotCLI(t, server, tc.stdin, "tool", "update", "--request", tc.request)
			if err != nil {
				t.Fatalf("CLI: %v; stdout=%s stderr=%s", err, out, stderr)
			}
			select {
			case got := <-requests:
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("request = %#v, want %#v", got, want)
				}
			default:
				t.Fatal("no UpdateSandboxTool request")
			}
			select {
			case extra := <-requests:
				t.Fatalf("unexpected second request: %#v", extra)
			default:
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
				"Support depends on the launch mode",
				"use MountOptions to adjust mounts the tool already declares",
				"SubPath",
			},
		},
		{
			command: "tool.update",
			fragments: []string{
				"Omit --storage-mounts or pass [] to keep the current mounts",
				"ImageRegistryType: personal or enterprise; required with Image",
				"SubPath: omitted keeps the current directory, an empty string switches to the image root, a non-empty string switches to that directory",
				"omit MountPath and ReadOnly",
				"With --request, you can reuse an Image mount from tool get",
				"The update response acknowledges the request",
				"--wait checks the tool lifecycle (ACTIVE or ISOLATED)",
				`agr tool get sdt-example -o json --jq '.Data.StorageMounts[]? | select(.Name == "runtime") | .StorageSource.Image'`,
				"Repeat readback until Reference and ImageRegistryType match and the expected SubPath is visible",
				"an empty or omitted readback means the image root",
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
				if tc.command == "tool.update" && strings.Contains(example, "--wait") {
					t.Errorf("tool.update mount example implies --wait verifies the mount: %q", example)
				}
			}
		})
	}
}

func TestInstanceCreateMountSourcesStayInThePublicContract(t *testing.T) {
	sources := schemaForCommand(t, "instance.create").Flags["storage-mounts"].Values
	cmd, ok := findCobraCommand(contractRoot(), "instance.create")
	if !ok {
		t.Fatal("instance.create command missing")
	}
	help := commandHelp(t, cmd)
	for _, want := range []string{
		"Tool start (--tool-id or --tool-name) supports Volume (VolumeId or VolumeName), StorageSource.Cfs, StorageSource.Cos, or StorageSource.Image.",
		"Clone (--source-instance-id) inherits the source instance's mounts. Omit StorageMounts or supply an empty array.",
		"Snapshot restore (--snapshot-id) supports only direct StorageSource.Cos or StorageSource.Image mounts.",
	} {
		if !slices.Contains(sources, want) {
			t.Errorf("instance.create storage-mounts schema misses %q: %v", want, sources)
		}
		if !strings.Contains(help, want) {
			t.Errorf("instance create --help misses %q", want)
		}
	}
	documented := strings.Join(sources, "\n")
	for _, rejected := range []string{"VolumeTemplate", "AgentBucket"} {
		if strings.Contains(documented, rejected) {
			t.Errorf("instance.create storage-mounts values recommend %s: %s", rejected, documented)
		}
		if strings.Contains(help, rejected) {
			t.Errorf("instance create --help recommends %s:\n%s", rejected, help)
		}
	}
}
