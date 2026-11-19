package patchscenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

var endpointAssertions = []string{
	"endpoint.create", "endpoint.get", "endpoint.list", "endpoint.update",
	"endpoint.conflict", "endpoint.delete", "endpoint.metrics",
	"target.candidates", "target.create", "target.get", "target.list",
	"target.update", "target.conflict", "target.sync", "target.delete",
	"token.create", "token.list", "token.revoke",
}

type endpointCallError struct{ command, code string }

func (e *endpointCallError) Error() string { return e.command + ": " + e.code }

func endpointErrorIs(err error, code string) bool {
	var failure *endpointCallError
	return errors.As(err, &failure) && failure.code == code
}

func endpointCall(s *patchtest.Session, ctx context.Context, command string, flags ...string) (apivalue.Object, error) {
	args := append([]string{"endpoint"}, strings.Fields(command)...)
	args = append(append(args, flags...), "-o", "json")
	raw, callErr := s.CLI(ctx, args...)
	var out struct {
		Status  string
		Data    apivalue.Object
		Failure *struct{ Code string }
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, fmt.Errorf("endpoint %s: invalid CLI envelope", command)
	}
	if out.Failure != nil {
		return out.Data, &endpointCallError{command, out.Failure.Code}
	}
	if callErr != nil || out.Status != "succeeded" || out.Data.String("RequestId") == "" {
		return out.Data, fmt.Errorf("endpoint %s: missing successful API response", command)
	}
	return out.Data, nil
}

func endpointFind(data apivalue.Object, list, field, value string) apivalue.Object {
	for _, item := range data.Objects(list) {
		if item.String(field) == value {
			return item
		}
	}
	return nil
}

func createEndpointFixture(s *patchtest.Session, name string, flags ...string) (apivalue.Object, error) {
	var endpointID string
	// If creation timed out after the write, find the resource by the unique
	// scenario name before deciding there is nothing to delete.
	s.Cleanup(func(ctx context.Context) error {
		if endpointID == "" {
			list, err := endpointCall(s, ctx, "list")
			if err != nil {
				return err
			}
			endpointID = endpointFind(list, "EndpointSet", "Name", name).String("EndpointId")
			if endpointID == "" {
				return nil
			}
		}
		return deleteEndpointFixture(s, ctx, endpointID)
	})
	data, err := endpointCall(s, s.Context, "create", append([]string{"--name", name}, flags...)...)
	endpoint := data.Object("Endpoint")
	endpointID = endpoint.String("EndpointId")
	return endpoint, err
}

func deleteEndpointFixture(s *patchtest.Session, ctx context.Context, endpointID string) error {
	state, err := endpointCall(s, ctx, "get", "--endpoint-id", endpointID)
	if endpointErrorIs(err, "ResourceNotFound") {
		return nil
	}
	if err != nil {
		return err
	}
	revision := strconv.FormatInt(state.Object("Endpoint").Int64("Revision"), 10)
	if _, err := endpointCall(s, ctx, "delete", "--endpoint-id", endpointID, "--expected-revision", revision); err != nil {
		return err
	}
	if _, err := endpointCall(s, ctx, "get", "--endpoint-id", endpointID); !endpointErrorIs(err, "ResourceNotFound") {
		return errors.New("deleted Endpoint was not confirmed absent")
	}
	return nil
}

func runEndpointLifecycle(s *patchtest.Session) error {
	recordID := os.Getenv("AGR_ENDPOINT_FIXTURE_RECORD_ID")
	if recordID == "" {
		return errors.New("AGR_ENDPOINT_FIXTURE_RECORD_ID must identify an MCP record with two READY versions")
	}
	fixture, err := loadEndpointMetricFixture()
	if err != nil {
		return err
	}
	ctx := s.Context
	name := fmt.Sprintf("agr-endpoint-e2e-%d", time.Now().UnixNano())
	endpoint, err := createEndpointFixture(s, name, "--description", "preview lifecycle", "--qps", "17")
	if err != nil {
		return err
	}
	id := endpoint.String("EndpointId")
	if err := s.Assert("endpoint.create", id != "" && endpoint.String("Name") == name &&
		endpoint.String("Description") == "preview lifecycle" && endpoint.String("Status") == "ACTIVE" &&
		endpoint.String("InboundAuthType") == "ENDPOINT_TOKEN" && endpoint.String("MCPURL") != "" &&
		endpoint.Int64("QPS") == 17 && endpoint.Int64("Revision") == 1); err != nil {
		return err
	}
	got, err := endpointCall(s, ctx, "get", "--endpoint-id", id)
	if err := s.Assert("endpoint.get", err == nil && got.Object("Endpoint").String("MCPURL") == endpoint.String("MCPURL") &&
		got.Object("Endpoint").String("EndpointId") == id && got.Object("Endpoint").Int64("Revision") == 1); err != nil {
		return err
	}
	list, err := endpointCall(s, ctx, "list")
	if err := s.Assert("endpoint.list", err == nil && list.Int64("TotalCount") >= 1 &&
		endpointFind(list, "EndpointSet", "EndpointId", id).String("Name") == name); err != nil {
		return err
	}
	updated, err := endpointCall(s, ctx, "update", "--endpoint-id", id, "--expected-revision", "1",
		"--name", name+"-updated", "--description=", "--status", "DISABLED", "--qps", "31")
	if err != nil {
		return err
	}
	persisted, err := endpointCall(s, ctx, "get", "--endpoint-id", id)
	if err != nil {
		return err
	}
	if err := s.Assert("endpoint.update", endpointUpdated(updated, id, name+"-updated") &&
		endpointUpdated(persisted, id, name+"-updated")); err != nil {
		return err
	}
	_, err = endpointCall(s, ctx, "delete", "--endpoint-id", id, "--expected-revision", "1")
	if err := s.Assert("endpoint.conflict", endpointErrorIs(err, "ResourceInUse.ConfigConflict")); err != nil {
		return err
	}
	if _, err := endpointCall(s, ctx, "update", "--endpoint-id", id, "--expected-revision", "2", "--status", "ACTIVE"); err != nil {
		return err
	}
	versions, err := endpointTargetVersions(s, id, recordID)
	if err != nil {
		return err
	}
	if err := runEndpointTarget(s, id, recordID, versions); err != nil {
		return err
	}
	if err := runEndpointToken(s, endpoint); err != nil {
		return err
	}
	other, err := createEndpointFixture(s, name+"-metrics")
	if err != nil {
		return err
	}
	if err := runEndpointMetrics(s, endpoint, other, versions[0], fixture); err != nil {
		return err
	}
	if err := deleteEndpointFixture(s, ctx, id); err != nil {
		return err
	}
	return s.Assert("endpoint.delete", true)
}

func endpointUpdated(response apivalue.Object, id, name string) bool {
	state := response.Object("Endpoint")
	return state.String("EndpointId") == id && state.String("Name") == name && state["Description"] == "" &&
		state.String("Status") == "DISABLED" && state.Int64("QPS") == 31 && state.Int64("Revision") == 2
}

func endpointTargetVersions(s *patchtest.Session, endpointID, recordID string) ([]apivalue.Object, error) {
	data, err := endpointCall(s, s.Context, "target candidates", "--endpoint-id", endpointID)
	if err != nil {
		return nil, err
	}
	var versions []apivalue.Object
	for _, candidate := range data.Objects("CandidateSet") {
		if candidate.String("RecordId") == recordID && candidate.String("Compatibility") == "READY" &&
			candidate.String("Protocol") == "MCP" && candidate.String("VersionId") != "" {
			versions = append(versions, candidate)
		}
	}
	if err := s.Assert("target.candidates", data.Int64("TotalCount") >= 2 && len(versions) >= 2 &&
		versions[0].String("RegistryId") != "" && versions[0].String("RegistryId") == versions[1].String("RegistryId") &&
		versions[0].String("VersionId") != versions[1].String("VersionId")); err != nil {
		return nil, fmt.Errorf("target.candidates: two distinct READY MCP versions from the configured record are required: %w", err)
	}
	return versions, nil
}

func runEndpointTarget(s *patchtest.Session, endpointID, recordID string, versions []apivalue.Object) error {
	ctx := s.Context
	run := func(command string, flags ...string) (apivalue.Object, error) {
		return endpointCall(s, ctx, "target "+command, append([]string{"--endpoint-id", endpointID}, flags...)...)
	}
	registryID := versions[0].String("RegistryId")
	oldVersion, newVersion := versions[0].String("VersionId"), versions[1].String("VersionId")
	data, err := run("create", "--registry-id", registryID, "--record-id", recordID, "--version-id", oldVersion,
		"--name", "preview target", "--description", "preview target description", "--path-alias", "preview-target")
	if err != nil {
		return err
	}
	target := data.Object("Target")
	id := target.String("TargetId")
	if err := s.Assert("target.create", id != "" && target.String("EndpointId") == endpointID &&
		target.String("RegistryId") == registryID && target.String("RecordId") == recordID && target.String("VersionId") == oldVersion &&
		target.String("Name") == "preview target" && target.String("Description") == "preview target description" &&
		target.String("PathAlias") == "preview-target" && strings.HasSuffix(target.String("InvocationURL"), "/mcp/preview-target") &&
		target.String("SourceType") == "REGISTRY_VERSION" && target.String("BackendAuthType") == "NONE" &&
		target.String("Protocol") == "MCP" && target.String("Type") == "MCP" &&
		target.String("Status") == "ENABLED" && target.String("Availability") == "READY" && target.Int64("Revision") == 1); err != nil {
		return err
	}
	got, err := run("get", "--target-id", id)
	if err := s.Assert("target.get", err == nil && got.Object("Target").String("TargetId") == id &&
		got.Object("Target").String("VersionId") == oldVersion && got.Object("Target").Int64("Revision") == 1); err != nil {
		return err
	}
	data, err = run("update", "--target-id", id, "--expected-revision", "1", "--version-id", newVersion,
		"--name", "updated target", "--description=", "--path-alias=", "--status", "DISABLED")
	if err != nil {
		return err
	}
	persisted, err := run("get", "--target-id", id)
	if err != nil {
		return err
	}
	if err := s.Assert("target.update", endpointTargetUpdated(data, id, newVersion) &&
		endpointTargetUpdated(persisted, id, newVersion)); err != nil {
		return err
	}
	list, err := run("list")
	if err := s.Assert("target.list", err == nil && list.Int64("TotalCount") == 1 &&
		endpointFind(list, "TargetSet", "TargetId", id).String("Status") == "DISABLED"); err != nil {
		return err
	}
	_, err = run("sync", "--target-id", id, "--expected-revision", "1")
	if err := s.Assert("target.conflict", endpointErrorIs(err, "ResourceInUse.ConfigConflict")); err != nil {
		return err
	}
	data, err = run("sync", "--target-id", id, "--expected-revision", "2")
	if err != nil {
		return err
	}
	synced := data.Object("Target")
	if err := s.Assert("target.sync", synced.String("VersionId") == newVersion && synced.String("Availability") == "READY" &&
		synced.String("Status") == "DISABLED" && synced.Int64("Revision") == 3); err != nil {
		return err
	}
	if _, err := run("delete", "--target-id", id, "--expected-revision", "3"); err != nil {
		return err
	}
	_, err = run("get", "--target-id", id)
	return s.Assert("target.delete", endpointErrorIs(err, "ResourceNotFound"))
}

func endpointTargetUpdated(response apivalue.Object, id, version string) bool {
	state := response.Object("Target")
	return state.String("TargetId") == id && state.String("VersionId") == version &&
		state.String("Name") == "updated target" && state["Description"] == "" &&
		(state["PathAlias"] == nil || state["PathAlias"] == "") &&
		(state["InvocationURL"] == nil || state["InvocationURL"] == "") &&
		state.String("Status") == "DISABLED" && state.Int64("Revision") == 2
}

func runEndpointToken(s *patchtest.Session, endpoint apivalue.Object) error {
	ctx := s.Context
	run := func(command string, flags ...string) (apivalue.Object, error) {
		return endpointCall(s, ctx, "token "+command, append([]string{"--endpoint-id", endpoint.String("EndpointId")}, flags...)...)
	}
	first, err := run("create", "--name", "temporary")
	if err != nil {
		return err
	}
	second, err := run("create", "--name", "permanent", "--expires-in", "0")
	if err != nil {
		return err
	}
	id, bearer := first.String("TokenId"), first.String("Token")
	expiry, expiryErr := time.Parse(time.RFC3339, first.String("ExpiresAt"))
	remaining := time.Until(expiry)
	if err := s.Assert("token.create", strings.HasPrefix(id, "eptk-") && strings.HasPrefix(bearer, "ept_") &&
		len(bearer) == 47 && expiryErr == nil && remaining > 29*24*time.Hour && remaining <= 31*24*time.Hour &&
		strings.HasPrefix(second.String("TokenId"), "eptk-") && second.String("TokenId") != id &&
		second.String("Token") != "" && second["ExpiresAt"] == nil); err != nil {
		return err
	}
	items := make(map[string]apivalue.Object, 2)
	for _, offset := range []string{"0", "1"} {
		page, err := run("list", "--limit", "1", "--offset", offset)
		if err != nil {
			return err
		}
		tokens := page.Objects("TokenSet")
		if page.Int64("TotalCount") != 2 || len(tokens) != 1 {
			return errors.New("token.list: expected one Token per page and a total of two")
		}
		item := tokens[0]
		if item["Token"] != nil || item.String("Status") != "ACTIVE" || item.String("CreatedAt") == "" {
			return errors.New("token.list: unexpected Token metadata")
		}
		items[item.String("TokenId")] = item
	}
	temporary, permanent := items[id], items[second.String("TokenId")]
	if err := s.Assert("token.list", len(items) == 2 &&
		temporary.String("Name") == "temporary" && temporary.String("ExpiresAt") == first.String("ExpiresAt") &&
		permanent.String("Name") == "permanent" && permanent["ExpiresAt"] == nil); err != nil {
		return err
	}
	before, err := endpointMCPStatus(ctx, endpoint.String("MCPURL"), bearer)
	if err != nil || before != http.StatusOK {
		return fmt.Errorf("issued Token did not authenticate the MCP request (HTTP %d)", before)
	}
	for range 2 {
		if _, err := run("revoke", "--token-id", id); err != nil {
			return err
		}
	}
	after, err := endpointMCPStatus(ctx, endpoint.String("MCPURL"), bearer)
	if err != nil {
		return err
	}
	list, err := run("list")
	return s.Assert("token.revoke", err == nil && after == http.StatusUnauthorized &&
		endpointFind(list, "TokenSet", "TokenId", id).String("Status") == "REVOKED" &&
		endpointFind(list, "TokenSet", "TokenId", second.String("TokenId")).String("Status") == "ACTIVE")
}
