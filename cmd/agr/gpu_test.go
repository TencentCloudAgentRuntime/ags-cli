package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGPUCommandsSignedTransport(t *testing.T) {
	const specs = `{"GPUResourceSpecSet":[{"GPUType":"sold-out","CPU":32,"Memory":96,"GPUNum":1,"GPUMemoryGiB":32,"AvailableCount":0},{"GPUType":"unknown-quota","CPU":64,"Memory":192,"GPUNum":2,"GPUMemoryGiB":64,"AvailableCount":null}],"TotalCount":2,"RequestId":"gpu-request"}`
	for _, tt := range []struct {
		name, action, request, response string
		args                            []string
	}{
		{"group filter", "DescribeGPUResourceGroups", `{"Filters":[{"Name":"resource-group-id","Values":["gpug-test"]}],"Offset":20,"Limit":10}`, `{"ResourceGroups":[],"TotalCount":1,"RequestId":"gpu-request"}`,
			[]string{"gpu", "group", "list", "--filters", `[{"Name":"resource-group-id","Values":["gpug-test"]}]`, "--offset", "20", "--limit", "10"}},
		{"resource filters", "DescribeGPUResources", `{"ResourceGroupId":"gpug-test","ResourceIds":["ins-gpu1","ins-gpu2"],"State":"Active","AllocationState":"InUse"}`, `{"Resources":[],"TotalCount":0,"RequestId":"gpu-request"}`,
			[]string{"gpu", "resource", "list", "--resource-group-id", "gpug-test", "--resource-ids", "ins-gpu1", "--resource-ids", "ins-gpu2", "--state", "Active", "--allocation-state", "InUse"}},
		{"spec quota", "DescribeGPUResourceSpecs", `{}`, specs, []string{"gpu", "spec", "list"}},
		{"exact spec", "DescribeGPUResourceSpecs", `{"GPUType":"5090d","CPU":32,"Memory":96,"GPUNum":1}`, specs,
			[]string{"gpu", "spec", "list", "--gpu-type", "5090d", "--cpu", "32", "--memory", "96", "--gpu-num", "1"}},
		{"rename", "ModifyGPUResourceGroup", `{"ResourceGroupId":"gpug-test","Name":"inference"}`, `{"RequestId":"gpu-request"}`,
			[]string{"gpu", "group", "rename", "gpug-test", "--name", "inference"}},
		{"rename via request", "ModifyGPUResourceGroup", `{"ResourceGroupId":"gpug-test","Name":"inference"}`, `{"RequestId":"gpu-request"}`,
			[]string{"gpu", "group", "rename", "--request", `{"ResourceGroupId":"gpug-test","Name":"inference"}`}},
		{"rename via positional and request", "ModifyGPUResourceGroup", `{"ResourceGroupId":"gpug-test","Name":"inference"}`, `{"RequestId":"gpu-request"}`,
			[]string{"gpu", "group", "rename", "gpug-test", "--request", `{"Name":"inference"}`}},
		{"delete via request", "DeleteGPUResourceGroup", `{"ResourceGroupId":"gpug-test"}`, `{"RequestId":"gpu-request"}`,
			[]string{"gpu", "group", "delete", "--request", `{"ResourceGroupId":"gpug-test"}`}},
		{"delete via positional and request", "DeleteGPUResourceGroup", `{"ResourceGroupId":"gpug-test"}`, `{"RequestId":"gpu-request"}`,
			[]string{"gpu", "group", "delete", "gpug-test", "--request", `{}`}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan []byte, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("X-TC-Action") != tt.action || req.Header.Get("Authorization") == "" {
					t.Error("expected signed " + tt.action)
				}
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Error(err)
					return
				}
				requests <- body
				_, _ = w.Write([]byte(`{"Response":` + tt.response + `}`))
			}))
			defer server.Close()
			// Reuse the HTTPS fixture runner and its isolated CLI configuration.
			result := operationalEventCLI(t, server, "", append(tt.args, "-o", "json")...)
			if result.code != 0 || len(result.stderr) != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
			}
			envelope := operationalEventObject(t, result.stdout)
			if !reflect.DeepEqual(envelope["Data"], operationalEventObject(t, []byte(tt.response))) {
				t.Fatalf("JSON lost API response fields: %s", result.stdout)
			}
			select {
			case body := <-requests:
				if !reflect.DeepEqual(operationalEventObject(t, body), operationalEventObject(t, []byte(tt.request))) {
					t.Fatalf("request=%s want=%s", body, tt.request)
				}
			default:
				t.Fatal("CLI did not call the API")
			}
			if tt.name != "spec quota" {
				return
			}
			result = operationalEventCLI(t, server, "", append(tt.args, "-o", "text")...)
			if result.code != 0 || len(result.stderr) != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
			}
			text := string(result.stdout)
			lines := strings.Split(strings.TrimSpace(text), "\n")
			if len(lines) != 4 || !strings.HasPrefix(lines[1], "sold-out") || !strings.HasSuffix(strings.TrimSpace(lines[1]), " 0") ||
				!strings.HasPrefix(lines[2], "unknown-quota") || !strings.HasSuffix(strings.TrimSpace(lines[2]), " unknown") ||
				lines[3] != "Total: 2" {
				t.Fatalf("text does not distinguish zero from unknown: %s", text)
			}
		})
	}
}

func TestGPUGroupMutationRejectsExtraRequestIDs(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"Response":{"RequestId":"unexpected-request"}}`)
	}))
	defer server.Close()
	for _, tt := range []struct{ verb, body string }{
		{"rename", `{"Name":"inference"}`},
		{"delete", `{}`},
	} {
		t.Run(tt.verb, func(t *testing.T) {
			before := calls.Load()
			result := operationalEventCLI(t, server, "", "gpu", "group", tt.verb, "gpug-first", "gpug-second",
				"--request", tt.body, "-o", "json")
			if n := calls.Load() - before; n != 0 {
				t.Errorf("invalid request reached the control plane %d time(s)", n)
			}
			failure, _ := operationalEventObject(t, result.stdout)["Failure"].(map[string]any)
			if result.code != 2 || failure["Code"] != "INVALID_USAGE" || !strings.Contains(string(result.stdout), "accepts at most 1 arg(s), got 2") {
				t.Fatalf("extra IDs did not fail with a usage error: exit=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
			}
		})
	}
}

func TestGPUSpecListRejectsPartialExactMatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	for _, extra := range [][]string{{"--gpu-type", "5090d"}, {"--request", `{"CPU":32,"Memory":96}`}} {
		result := operationalEventCLI(t, server, "", append(append([]string{"gpu", "spec", "list"}, extra...), "-o", "json")...)
		if result.code != 2 || !strings.Contains(string(result.stdout), "must all be supplied or omitted together") {
			t.Fatalf("partial specification did not fail with a usage error: exit=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid spec query reached control plane")
	}
}

func TestGPUDeletePreservesGroupInUse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"Response":{"Error":{"Code":"ResourceInUse","Message":"group still has bindings"},"RequestId":"blocked-gpu-delete"}}`)
	}))
	defer server.Close()
	result := operationalEventCLI(t, server, "", "gpu", "group", "delete", "gpug-in-use", "-o", "json")
	if result.code == 0 {
		t.Fatalf("blocked group deletion unexpectedly succeeded: %s", result.stdout)
	}
	failure, _ := operationalEventObject(t, result.stdout)["Failure"].(map[string]any)
	details, _ := failure["Details"].(map[string]any)
	if failure["Code"] != "ResourceInUse" || details["RequestId"] != "blocked-gpu-delete" {
		t.Fatalf("group deletion lost the service error or request ID: %s", result.stdout)
	}
}
