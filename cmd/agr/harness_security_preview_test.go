//go:build preview

package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func diagnosticMCP(value string) []any {
	return []any{map[string]any{
		"Name": "docs", "Source": map[string]any{"Remote": map[string]any{"URL": "https://example.com/mcp"}},
		"Credential": map[string]any{"CredentialProviderId": "agc-test", "UserId": "reader", "Scope": "default"},
		"Headers": []any{
			map[string]any{"Name": "Authorization", "Value": value},
			map[string]any{"Name": "X-Unlisted-Future-Header", "Value": value},
			map[string]any{"Name": "X-Template", "Value": "Bearer ${credential}"},
		},
	}}
}

func TestHarnessMCPDiagnosticRedaction(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		for _, command := range []string{"status", "diff"} {
			t.Run(command+"/"+format, func(t *testing.T) {
				const draftSecret = "diagnostic-draft-secret"
				const liveSecret = "diagnostic-live-secret"
				const latestSecret = "diagnostic-latest-secret"
				fake := newHarnessFake(t)
				fake.seedHarness("PUBLISHED", "UNPUBLISHED")
				draft := fake.harness["Spec"].(map[string]any)
				draft["SystemPrompt"] = "same prompt"
				draft["MCPServers"] = diagnosticMCP(draftSecret)
				for n, secret := range map[int64]string{1: liveSecret, 2: latestSecret} {
					s := fake.revisions[n]["Spec"].(map[string]any)
					s["SystemPrompt"] = "same prompt"
					s["MCPServers"] = diagnosticMCP(secret)
				}
				server := httptest.NewTLSServer(fake)
				defer server.Close()
				args := []string{"harness", "status", "hns-test"}
				if command == "diff" {
					args = []string{"harness", "revision", "diff", "hns-test", "live", "draft"}
				}
				out, stderr, err := runSnapshotCLIWithOutput(t, server, strings.NewReader(""), format, args...)
				if err != nil {
					t.Fatalf("diagnostic command failed: %v; stderr=%s", err, stderr)
				}
				for _, value := range []string{draftSecret, liveSecret, latestSecret, "${credential}"} {
					if strings.Contains(string(out), value) || strings.Contains(stderr, value) {
						t.Errorf("diagnostic output disclosed a header value")
					}
				}
				if format == "json" {
					var envelope harnessRun
					if err := json.Unmarshal(out, &envelope); err != nil {
						t.Fatal(err)
					}
					key := "Changed"
					if command == "status" {
						key = "DraftChanged"
						for _, field := range []string{"Harness", "LiveRevision", "LatestRevision"} {
							resource := envelope.Data[field].(map[string]any)
							mcp := resource["Spec"].(map[string]any)["MCPServers"].([]any)[0].(map[string]any)
							for _, raw := range mcp["Headers"].([]any) {
								header := raw.(map[string]any)
								if header["Name"] == "" || header["Value"] != "[REDACTED]" {
									t.Errorf("%s lost header names or exposed values", field)
								}
							}
						}
					}
					if envelope.Data[key] != true {
						t.Error("redaction hid a real header-value change")
					}
				}
				for _, call := range fake.actions() {
					if call != "DescribeHarness" && call != "DescribeHarnessRevisionList" {
						t.Errorf("diagnostic command mutated resources: %s", call)
					}
				}
			})
		}
	}
}

func TestHarnessPlaintextMCPHeaderWarning(t *testing.T) {
	const secret = "warning-secret-value"
	for _, command := range []string{"create", "update"} {
		for _, format := range []string{"json", "text"} {
			t.Run(command+"/"+format, func(t *testing.T) {
				fake := newHarnessFake(t)
				fake.seedHarness("PUBLISHED")
				server := httptest.NewTLSServer(fake)
				defer server.Close()
				args := []string{"harness", "update", "hns-test"}
				action := "ModifyHarness"
				if command == "create" {
					args = []string{"harness", "create", "--name", "agent", "--engine", "pi", "--engine-version", "1.3.0",
						"--model", "mp-a:hy3", "--role-arn", "role", "--subnet-id", "subnet", "--security-group-id", "sg"}
					action = "CreateHarness"
				}
				args = append(args, "--mcp", "docs=https://example.com/mcp", "--mcp-header", "docs:X-SeCrEt-Key="+secret)
				out, stderr, err := runSnapshotCLIWithOutput(t, server, strings.NewReader(""), format, args...)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(stderr, "Warning:") || !strings.Contains(stderr, "X-SeCrEt-Key") || strings.Contains(stderr, secret) {
					t.Errorf("missing or unsafe plaintext-header warning: %s", stderr)
				}
				if format == "json" {
					var envelope struct{ Warnings []string }
					if err := json.Unmarshal(out, &envelope); err != nil {
						t.Fatal(err)
					}
					if len(envelope.Warnings) != 1 || strings.Contains(envelope.Warnings[0], secret) {
						t.Error("JSON warning was missing or exposed the value")
					}
				}
				servers := fake.last(action)["MCPServers"].([]any)
				headers := servers[0].(map[string]any)["Headers"].([]any)
				if headers[0].(map[string]any)["Value"] != secret {
					t.Error("warning changed the request sent to the service")
				}
			})
		}
	}
}

func TestHarnessMCPHeaderWarningInputSources(t *testing.T) {
	const secret = "input-source-secret"
	for _, flag := range []string{"mcp-servers", "request"} {
		for _, source := range []string{"inline", "file", "stdin", "dry-run"} {
			t.Run(flag+"/"+source, func(t *testing.T) {
				fake := newHarnessFake(t)
				fake.seedHarness("PUBLISHED")
				server := httptest.NewTLSServer(fake)
				defer server.Close()
				servers := diagnosticMCP(secret)
				// Only Authorization should warn. Unknown names are still redacted
				// by diagnostics, but are not classified as secrets by the advisory.
				var value any = servers
				args := []string{"harness", "update", "hns-test"}
				if flag == "request" {
					value = map[string]any{"HarnessId": "hns-test", "MCPServers": servers}
					args = []string{"harness", "update"}
				}
				payload, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				input, stdin := string(payload), ""
				switch source {
				case "file":
					path := filepath.Join(t.TempDir(), "input.json")
					if err := os.WriteFile(path, payload, 0600); err != nil {
						t.Fatal(err)
					}
					input = "@" + path
				case "stdin":
					input, stdin = "-", string(payload)
				case "dry-run":
					args = append(args, "--dry-run")
				}
				args = append(args, "--"+flag, input)
				out, stderr, err := runSnapshotCLIWithOutput(t, server, strings.NewReader(stdin), "json", args...)
				if err != nil {
					t.Fatalf("update failed: %v; stderr=%s", err, stderr)
				}
				var result struct {
					Warnings []string
					Data     map[string]any
				}
				if err := json.Unmarshal(out, &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "Authorization") || strings.Contains(result.Warnings[0], secret) || strings.Contains(stderr, secret) {
					t.Fatal("missing or unsafe warning for assembled request")
				}
				got := fake.last("ModifyHarness")
				if source == "dry-run" {
					if len(fake.actions()) != 0 {
						t.Fatal("dry-run sent a cloud request")
					}
					got = result.Data["Request"].(map[string]any)
				}
				encoded, _ := json.Marshal(got["MCPServers"])
				want, _ := json.Marshal(servers)
				if string(encoded) != string(want) {
					t.Error("warning changed MCP configuration")
				}
			})
		}
	}
}
