package patchscenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

type registryTag struct{ Key, Value string }

// Keep raw service responses and resource identifiers out of public reports.
type registryResponse struct {
	Status  string
	Raw     map[string]any `json:"-"`
	Failure *struct{ Code, Kind string }
	Data    struct {
		RegistryId                                                                                                                                     string
		UploadURL, DownloadURL, ExpireTime, ContentStatus, SHA256, ResolvedVersionId, PreviewResult, SyncStatus, ErrorCode, ErrorMessage, LastSyncTime string
		CreatedVersion                                                                                                                                 struct{ VersionId, Descriptors string }
		RecordId                                                                                                                                       string
		TotalCount                                                                                                                                     int64
		Registry                                                                                                                                       struct {
			Description, ApprovalMode string
			Tags                      []registryTag
		}
		Record  struct{ Description string }
		Version struct {
			VersionId, Status, Descriptors, ContentStatus, ContentSHA256, ChangeLog string
			ContentSizeBytes                                                        int64
		}
	}
}

func (r registryResponse) hasRegistryTag(want registryTag) bool {
	return slices.Contains(r.Data.Registry.Tags, want)
}

func registryCall(s *patchtest.Session, ctx context.Context, command string, request map[string]any) (registryResponse, error) {
	var result registryResponse
	payload, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	args := append(strings.Fields(command), "--request", string(payload), "-o", "json")
	raw, callErr := s.CLI(ctx, args...)
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("%s: invalid response", command)
	}
	if callErr != nil || result.Status != "succeeded" {
		code := "unknown"
		if result.Failure != nil {
			code = result.Failure.Code
		}
		return result, fmt.Errorf("%s failed (%s)", command, code)
	}
	var envelope struct{ Data map[string]any }
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&envelope); err != nil {
		return result, fmt.Errorf("invalid response JSON")
	}
	result.Raw = envelope.Data
	if err := validateRegistryResponse(command, result.Raw); err != nil {
		return result, err
	}
	return result, nil
}

func registryCleanup(s *patchtest.Session, command string, request map[string]any) {
	s.Cleanup(func(ctx context.Context) error {
		// Always confirm absence, including a delete response lost after execution.
		_, _ = registryCall(s, ctx, command+" delete", request)
		result, err := registryCall(s, ctx, command+" get", request)
		if err == nil || result.Failure == nil || result.Failure.Kind != "not_found" {
			return fmt.Errorf("%s cleanup unconfirmed", command)
		}
		return nil
	})
}

func registryCustomLifecycle(s *patchtest.Session) error {
	ctx := s.Context
	name := fmt.Sprintf("cli-registry-%d", time.Now().UnixNano())
	tag := registryTag{Key: "cli-regression", Value: "disposable"}
	created, err := registryCall(s, ctx, "registry create", map[string]any{
		"Name":        name,
		"Description": "CLI regression fixture", "ApprovalMode": "MANUAL", "Tags": []registryTag{tag},
	})
	reg := created.Data.RegistryId
	if reg != "" {
		registryCleanup(s, "registry", map[string]any{"RegistryId": reg})
	}
	if err != nil {
		return err
	}
	if reg == "" || created.Data.Registry.ApprovalMode != "MANUAL" {
		return fmt.Errorf("invalid registry creation")
	}
	call := func(command string, request map[string]any) (registryResponse, error) {
		request["RegistryId"] = reg
		return registryCall(s, ctx, command, request)
	}
	if _, err = call("registry update", map[string]any{"Description": ""}); err != nil {
		return err
	}
	got, err := call("registry get", map[string]any{})
	if err != nil {
		return err
	}
	if err = s.Assert("registry.description", got.Data.Registry.Description == ""); err != nil {
		return err
	}
	if err = s.Assert("registry.tags.readback", got.hasRegistryTag(tag)); err != nil {
		return err
	}
	created, err = call("registry record create", map[string]any{
		"Name": "custom-fixture", "DescriptorType": "CUSTOM", "Description": "fixture",
		"VersionName": "v1", "CustomDescriptors": `{"fixture":"v1"}`,
	})
	rec := created.Data.RecordId
	if rec != "" {
		registryCleanup(s, "registry record", map[string]any{"RegistryId": reg, "RecordId": rec})
	}
	if err != nil {
		return err
	}
	// A supplied empty selector must fail without deleting the parent record.
	for _, selector := range []string{"", " "} {
		invalid, err := call("registry record delete", map[string]any{"RecordId": rec, "VersionId": selector, "Reason": "selector boundary"})
		if err == nil || invalid.Failure == nil || invalid.Failure.Code != "InvalidParameter.VersionId" {
			return fmt.Errorf("empty version selector was not rejected")
		}
	}
	surviving, err := call("registry record get", map[string]any{"RecordId": rec})
	if err != nil {
		return err
	}
	record, _ := surviving.Raw["Record"].(map[string]any)
	if err = s.Assert("record.delete.selector", record["RecordId"] == rec); err != nil {
		return err
	}
	ver := created.Data.Version.VersionId
	if rec == "" || ver == "" || created.Data.Version.Status != "PENDING_APPROVAL" {
		return fmt.Errorf("invalid pending record")
	}
	got, err = call("registry record approve", map[string]any{"RecordId": rec, "VersionId": ver, "Comment": "regression"})
	if err != nil {
		return err
	}
	if got.Data.Version.Status != "APPROVED" {
		return fmt.Errorf("approval not observed")
	}
	_, err = call("registry record update", map[string]any{"RecordId": rec, "Description": "updated",
		"LabelMutations": []map[string]any{{"Operation": "SET", "Name": "stable", "VersionId": ver, "Reason": "regression"}},
	})
	if err != nil {
		return err
	}
	got, err = call("registry record get", map[string]any{"RecordId": rec, "Label": "stable"})
	if err != nil {
		return err
	}
	var descriptors map[string]string
	if err = json.Unmarshal([]byte(got.Data.Version.Descriptors), &descriptors); err != nil {
		return fmt.Errorf("invalid descriptors")
	}
	if err = s.Assert("record.label.readback", got.Data.Version.VersionId == ver && got.Data.Record.Description == "updated" && descriptors["fixture"] == "v1"); err != nil {
		return err
	}
	for _, decision := range []struct{ command, status string }{{"reject", "REJECTED"}, {"cancel", "CANCELED"}} {
		next, err := call("registry record update", map[string]any{"RecordId": rec, "VersionName": decision.command, "ChangeLog": "regression", "CustomDescriptors": fmt.Sprintf(`{"fixture":%q}`, decision.command)})
		if err != nil {
			return err
		}
		if next.Data.Version.VersionId == "" || next.Data.Version.Status != "PENDING_APPROVAL" {
			return fmt.Errorf("new pending version missing")
		}
		got, err = call("registry record "+decision.command, map[string]any{"RecordId": rec, "VersionId": next.Data.Version.VersionId, "Comment": "regression"})
		if err != nil {
			return err
		}
		if got.Data.Version.Status != decision.status {
			return fmt.Errorf("decision not observed")
		}
	}
	if err = s.Assert("version.decisions", true); err != nil {
		return err
	}
	versions, err := call("registry record version list", map[string]any{"RecordId": rec, "Offset": 0, "Limit": 10})
	if err != nil {
		return err
	}
	logs, err := call("registry audit-log list", map[string]any{"RecordId": rec, "Offset": 0, "Limit": 10})
	if err != nil {
		return err
	}
	rows, ok := logs.Raw["AuditLogSet"].([]any)
	if !ok || len(rows) == 0 {
		return fmt.Errorf("audit rows missing")
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid audit row")
	}
	filtered, err := call("registry audit-log list", map[string]any{"RecordId": rec, "Actor": first["Actor"], "ActionFilter": first["Action"], "StartTime": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), "EndTime": time.Now().Add(time.Minute).UTC().Format(time.RFC3339), "Offset": 0, "Limit": 10})
	if err != nil {
		return err
	}
	if filtered.Data.TotalCount < 1 {
		return fmt.Errorf("audit filters lost matching event")
	}
	if err = s.Assert("record.history", versions.Data.TotalCount >= 3 && logs.Data.TotalCount > 0); err != nil {
		return err
	}
	for _, command := range []string{"preview", "sync"} {
		got, err = call("registry record "+command, map[string]any{"RecordId": rec, "VersionId": ver})
		if err == nil || got.Failure == nil || !strings.HasPrefix(got.Failure.Code, "UnsupportedOperation") {
			return fmt.Errorf("manual source remote operation not rejected")
		}
	}
	if err := s.Assert("manual.remote.rejected", true); err != nil {
		return err
	}
	return registryCustomExtras(s, reg, rec, ver, name)

}

// Validate real responses against the patch contract, including nested fields.
// Unknown fields are forward-compatible; absent optional fields remain optional.
func validateRegistryResponse(command string, data map[string]any) error {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("scenario source unavailable")
	}
	contract, err := apimeta.LoadContract(filepath.Join(filepath.Dir(source), "../../api/ags/v20250920"), apimeta.Preview)
	if err != nil {
		return err
	}
	for _, mapping := range contract.Mapping.Actions {
		if mapping.Command == strings.ReplaceAll(command, " ", ".") {
			raw, err := apimeta.LoadEffectiveJSON(filepath.Join(filepath.Dir(source), "../../api/ags/v20250920"))
			if err != nil {
				return err
			}
			var schema registryWireSchema
			if err := json.Unmarshal(raw, &schema); err != nil {
				return err
			}
			return validateRegistryObject(schema, mapping.Response, data)
		}
	}
	return fmt.Errorf("unmapped Registry command")
}

type registryWireSchema struct {
	Objects map[string]struct {
		Members []struct {
			Name, Type, Member string
			OutputRequired     bool `json:"output_required"`
			ValueAllowedNull   bool `json:"value_allowed_null"`
		}
	}
}

func validateRegistryObject(spec registryWireSchema, name string, data map[string]any) error {
	object, exists := spec.Objects[name]
	if !exists {
		return fmt.Errorf("unknown response object %s", name)
	}
	for _, m := range object.Members {
		value, exists := data[m.Name]
		if !exists {
			if m.OutputRequired {
				return fmt.Errorf("missing response field %s.%s", name, m.Name)
			}
			continue
		}
		if value == nil {
			if !m.ValueAllowedNull {
				return fmt.Errorf("null response field %s.%s", name, m.Name)
			}
			continue
		}
		values := []any{value}
		if m.Type == "list" {
			var ok bool
			values, ok = value.([]any)
			if !ok {
				return fmt.Errorf("expected response array %s.%s", name, m.Name)
			}
		}
		for _, v := range values {
			valid := false
			switch m.Member {
			case "string":
				_, valid = v.(string)
			case "int64", "uint64", "int":
				n, ok := v.(json.Number)
				if ok {
					_, e := n.Int64()
					valid = e == nil
				}
			case "bool", "boolean":
				_, valid = v.(bool)
			default:
				nested, ok := v.(map[string]any)
				if ok {
					if err := validateRegistryObject(spec, m.Member, nested); err != nil {
						return err
					}
					valid = true
				}
			}
			if !valid {
				return fmt.Errorf("invalid response type %s.%s", name, m.Name)
			}
		}
	}
	return nil
}

func registryCustomExtras(s *patchtest.Session, reg, rec, ver, name string) error {
	ctx := s.Context
	for _, item := range []struct {
		command, field, id string
		request            map[string]any
	}{
		{"registry list", "RegistrySet", reg, map[string]any{"Offset": 0, "Limit": 10, "Filters": []map[string]any{{"Name": "name", "Values": []string{name}}}}},
		{"registry record list", "RecordSet", rec, map[string]any{"RegistryId": reg, "Offset": 0, "Limit": 10, "Filters": []map[string]any{{"Name": "name", "Values": []string{"custom-fixture"}}, {"Name": "descriptor_type", "Values": []string{"CUSTOM"}}, {"Name": "lifecycle_status", "Values": []string{"ACTIVE"}}}}},
		{"registry record version list", "VersionSet", ver, map[string]any{"RegistryId": reg, "RecordId": rec, "Offset": 0, "Limit": 10, "Filters": []map[string]any{{"Name": "status", "Values": []string{"APPROVED"}}}}},
	} {
		result, err := registryCall(s, ctx, item.command, item.request)
		if err != nil {
			return err
		}
		rows, ok := result.Raw[item.field].([]any)
		if !ok || len(rows) != 1 || result.Data.TotalCount != 1 {
			return fmt.Errorf("filtered list mismatch")
		}
		row, ok := rows[0].(map[string]any)
		if !ok {
			return fmt.Errorf("invalid list row")
		}
		key := "RegistryId"
		if item.field == "RecordSet" {
			key = "RecordId"
		}
		if item.field == "VersionSet" {
			key = "VersionId"
		}
		if row[key] != item.id {
			return fmt.Errorf("filtered list identity mismatch")
		}
	}
	if err := s.Assert("lists.filtered", true); err != nil {
		return err
	}
	for _, request := range []map[string]any{
		{"RegistryId": reg, "RecordId": rec, "Description": "metadata", "CustomDescriptors": `{"fixture":"invalid"}`},
		{"RegistryId": reg, "RecordId": rec, "CustomDescriptors": `[]`},
	} {
		result, err := registryCall(s, ctx, "registry record update", request)
		if err == nil || result.Failure == nil {
			return fmt.Errorf("invalid update accepted")
		}
	}
	if err := s.Assert("input.boundaries", true); err != nil {
		return err
	}
	// Custom labels can be deleted; system labels cannot.
	_, err := registryCall(s, ctx, "registry record update", map[string]any{"RegistryId": reg, "RecordId": rec, "LabelMutations": []map[string]any{{"Operation": "SET", "Name": "regression", "VersionId": ver, "Reason": "regression"}}})
	if err != nil {
		return err
	}

	_, err = registryCall(s, ctx, "registry record update", map[string]any{"RegistryId": reg, "RecordId": rec, "Description": "", "LabelMutations": []map[string]any{{"Operation": "DELETE", "Name": "regression", "Reason": "regression"}}})
	if err != nil {
		return err
	}
	result, err := registryCall(s, ctx, "registry record get", map[string]any{"RegistryId": reg, "RecordId": rec, "Label": "regression"})
	if err == nil || result.Failure == nil {
		return fmt.Errorf("removed label still resolves")
	}
	if err = s.Assert("labels.removed", true); err != nil {
		return err
	}
	return registryManualSources(s, reg)
}

func registryManualSources(s *patchtest.Session, reg string) error {
	for _, source := range []struct{ kind, field, descriptors string }{
		{"MCP", "MCPSource", `{"$schema":"https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json","name":"example.com/fixture","description":"Disposable fixture","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.com/mcp"}]}`},
		{"A2A", "AgentSource", `{"name":"fixture","description":"Disposable fixture","version":"1.0.0","supportedInterfaces":[{"url":"https://example.com/agent","protocolBinding":"JSONRPC","protocolVersion":"1.0"}],"capabilities":{},"defaultInputModes":["text/plain"],"defaultOutputModes":["text/plain"],"skills":[{"id":"echo","name":"Echo","description":"Fixture","tags":["test"]}]}`},
	} {
		created, err := registryCall(s, s.Context, "registry record create", map[string]any{"RegistryId": reg, "Name": "manual-" + strings.ToLower(source.kind), "DescriptorType": source.kind, source.field: map[string]any{"Type": "MANUAL", "Descriptors": source.descriptors}})
		rec := created.Data.RecordId
		if rec != "" {
			registryCleanup(s, "registry record", map[string]any{"RegistryId": reg, "RecordId": rec})
		}
		if err != nil {
			return err
		}
		next, err := registryCall(s, s.Context, "registry record update", map[string]any{"RegistryId": reg, "RecordId": rec, "VersionName": "second", "ChangeLog": "manual update", source.field: map[string]any{"Type": "MANUAL", "Descriptors": strings.Replace(source.descriptors, "1.0.0", "2.0.0", 1)}})
		if err != nil {
			return err
		}
		ver := next.Data.Version.VersionId
		got, err := registryCall(s, s.Context, "registry record get", map[string]any{"RegistryId": reg, "RecordId": rec, "VersionId": ver})
		if err != nil {
			return err
		}
		if err = s.Assert(strings.ToLower(source.kind)+".manual.readback", ver != "" && ver != created.Data.Version.VersionId && descriptorVersion(got.Data.Version.Descriptors, "2.0.0")); err != nil {
			return err
		}
		_, err = registryCall(s, s.Context, "registry record delete", map[string]any{"RegistryId": reg, "RecordId": rec, "VersionId": ver, "Reason": "version cleanup"})
		if err != nil {
			return err
		}
		got, err = registryCall(s, s.Context, "registry record get", map[string]any{"RegistryId": reg, "RecordId": rec, "VersionId": ver})
		if err == nil || got.Failure == nil || got.Failure.Kind != "not_found" {
			return fmt.Errorf("deleted version still resolves")
		}
	}
	return s.Assert("version.deleted", true)
}
