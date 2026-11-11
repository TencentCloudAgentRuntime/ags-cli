package main

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

// A normal create/list/get workflow must remain discoverable through resource
// commands. A reasoned raw-only mapping alone does not satisfy this contract.
func TestPrecacheWorkflowHasResourceCommands(t *testing.T) {
	catalog, err := apimeta.Get()
	if err != nil {
		t.Fatal(err)
	}
	for action, id := range map[string]string{
		"CreatePreCacheImageTask":   "pre-cache-image-task.create",
		"DescribePreCacheImageList": "pre-cache-image-task.list",
		"DescribePreCacheImageTask": "pre-cache-image-task.get",
	} {
		mapped := catalog.Mapping.Actions[action]
		if mapped == nil || mapped.Status != "mapped" || mapped.Command != id {
			t.Fatalf("workflow action %s must expose %s, got %+v", action, id, mapped)
		}
		cmd, ok := findCobraCommand(contractRoot(), id)
		if !ok {
			t.Fatalf("resource command missing: %s", id)
		}
		if !strings.Contains(commandHelp(t, cmd), "agr pre-cache-image-task") {
			t.Errorf("%s lacks executable workflow examples", id)
		}
		if !schemaForCommand(t, id).SupportsRequest {
			t.Errorf("%s has no request schema", id)
		}
	}
	list := schemaForCommand(t, "pre-cache-image-task.list")
	if list.RequestSchema["Filters"].Type != "array" || list.Flags["filters"].Type != "json" {
		t.Fatalf("filter schema drift: %+v %+v", list.RequestSchema["Filters"], list.Flags["filters"])
	}
}

func TestPrecacheListSignedTransport(t *testing.T) {
	const filters = `[{"Name":"status","Values":["Success"]}]`
	const request = `{"Filters":[{"Name":"status","Values":["Success"]}],"MaxResults":20,"NextToken":"cursor","NeedTotalCount":false}`
	for _, mode := range []string{"flags", "filters file", "filters stdin", "request", "text", "empty"} {
		t.Run(mode, func(t *testing.T) {
			requests := make(chan map[string]any, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-TC-Action") != "DescribePreCacheImageList" || r.Header.Get("Authorization") == "" {
					t.Error("wrong action or unsigned resource request")
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				requests <- payload
				_, _ = w.Write([]byte(`{"Response":{"PreCacheImageList":[{"PreCacheImageId":"pcimg-test","Image":"example:latest","Status":"Success","ReadyTime":"2026-11-09T22:54:47+08:00","CachedImageSizeBytes":9007199254740993},{"PreCacheImageId":"pcimg-no-time","Status":"Success"}],"NextToken":"next-cursor","Future":9007199254740993}}`))
			}))
			defer server.Close()
			input, stdin := filters, ""
			if mode == "filters file" {
				path := filepath.Join(t.TempDir(), "filters.json")
				if err := os.WriteFile(path, []byte(filters), 0600); err != nil {
					t.Fatal(err)
				}
				input = "@" + path
			} else if mode == "filters stdin" {
				input, stdin = "-", filters
			}
			args := []string{"pre-cache-image-task", "list", "--filters", input, "--max-results", "20", "--next-token", "cursor", "--need-total-count=false"}
			if mode == "request" {
				args = []string{"pre-cache-image-task", "list", "--request", request}
			} else if mode == "empty" {
				args = []string{"pre-cache-image-task", "list"}
			}
			format := "json"
			if mode == "text" {
				format = "text"
			}
			args = append(args, "-o", format)
			cmd := exec.CommandContext(t.Context(), os.Args[0], append([]string{"-test.run=^TestTransportLifecycleHelper$", "--"}, args...)...)
			home := t.TempDir()
			cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			cmd.Env = []string{"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"), "AGR_TRANSPORT_LIFECYCLE_HELPER=success", "GORACE=atexit_sleep_ms=0", "TENCENTCLOUD_SECRET_ID=fake", "TENCENTCLOUD_SECRET_KEY=fake", "AGR_REGION=ap-guangzhou", "AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(server.URL, "https://"), "AGR_TRANSPORT_TEST_CERT=" + string(cert)}
			cmd.Stdin = strings.NewReader(stdin)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil || stderr.Len() != 0 {
				t.Fatalf("CLI failed: %v %s %s", err, out, stderr.String())
			}
			if mode == "text" {
				for _, value := range []string{"ID", "IMAGE", "STATUS", "READY TIME", "pcimg-test", "2026-11-09T22:54:47+08:00", "next-cursor"} {
					if !bytes.Contains(out, []byte(value)) {
						t.Errorf("table missing %q: %s", value, out)
					}
				}
			} else {
				var envelope struct {
					Command string
					Data    map[string]any
				}
				decoder := json.NewDecoder(bytes.NewReader(out))
				decoder.UseNumber()
				if err := decoder.Decode(&envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Command != "pre-cache-image-task.list" || envelope.Data["NextToken"] != "next-cursor" || envelope.Data["Future"] != json.Number("9007199254740993") {
					t.Fatalf("response changed: %+v", envelope)
				}
				items := envelope.Data["PreCacheImageList"].([]any)
				item := items[0].(map[string]any)
				if item["ReadyTime"] != "2026-11-09T22:54:47+08:00" || item["CachedImageSizeBytes"] != json.Number("9007199254740993") {
					t.Fatalf("list fields changed: %v", item)
				}
				if _, present := items[1].(map[string]any)["ReadyTime"]; present {
					t.Fatal("invented an absent ReadyTime")
				}
				if _, present := envelope.Data["TotalCount"]; present {
					t.Fatal("invented an unrequested TotalCount")
				}
			}
			payload := <-requests
			if mode == "empty" {
				if len(payload) != 0 {
					t.Fatalf("default flags changed the request: %v", payload)
				}
			} else if len(payload) != 4 || payload["MaxResults"] != float64(20) || payload["NeedTotalCount"] != false || payload["NextToken"] != "cursor" || payload["Filters"].([]any)[0].(map[string]any)["Name"] != "status" {
				t.Fatalf("request changed: %v", payload)
			}
		})
	}
}
