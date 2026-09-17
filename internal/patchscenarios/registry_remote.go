package patchscenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// AGR_REGISTRY_FIXTURE_URL must host the reviewed tests/registry/fixture service.
// The fixture only returns public metadata; no cloud credentials are sent to it.
func registryRemoteLifecycle(s *patchtest.Session) error {
	base, err := url.Parse(os.Getenv("AGR_REGISTRY_FIXTURE_URL"))
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil {
		return fmt.Errorf("AGR_REGISTRY_FIXTURE_URL must identify the HTTPS metadata fixture")
	}
	ctx, cancel := context.WithTimeout(s.Context, 3*time.Minute)
	defer cancel()
	health := *base
	health.Path = "/health"
	health.RawQuery = ""
	raw, err := registryHTTP(ctx, http.MethodGet, health.String(), nil)
	if err != nil {
		return err
	}
	var identity struct{ Fixture string }
	if json.Unmarshal(raw, &identity) != nil || identity.Fixture != "registry" {
		return fmt.Errorf("unexpected metadata fixture")
	}
	created, err := registryCall(s, ctx, "registry create", map[string]any{"Name": fmt.Sprintf("cli-remote-%d", time.Now().UnixNano()), "ApprovalMode": "AUTO"})
	reg := created.Data.RegistryId
	if reg != "" {
		registryCleanup(s, "registry", map[string]any{"RegistryId": reg})
	}
	if err != nil {
		return err
	}
	if reg == "" {
		return fmt.Errorf("missing RegistryId")
	}
	for _, kind := range []struct{ descriptor, source, path string }{{"MCP", "MCPSource", "/mcp"}, {"A2A", "AgentSource", "/agent.json"}} {
		change := time.Now().Add(20 * time.Second)
		fail := change.Add(15 * time.Second)
		endpoint := *base
		endpoint.Path = fmt.Sprintf("/case/%d/%d%s", change.UnixMilli(), fail.UnixMilli(), kind.path)
		endpoint.RawQuery = ""
		created, err := registryCall(s, ctx, "registry record create", map[string]any{"RegistryId": reg, "Name": "remote-" + strings.ToLower(kind.descriptor), "DescriptorType": kind.descriptor, kind.source: map[string]any{"Type": "URL_IMPORT", "EndpointURL": endpoint.String()}})
		rec := created.Data.RecordId
		ver := created.Data.Version.VersionId
		if rec != "" {
			registryCleanup(s, "registry record", map[string]any{"RegistryId": reg, "RecordId": rec})
		}
		if err != nil {

			return err
		}
		if rec == "" || ver == "" || !descriptorVersion(created.Data.Version.Descriptors, "1.0.0") {
			return fmt.Errorf("initial remote version mismatch")
		}
		request := map[string]any{"RegistryId": reg, "RecordId": rec, "VersionId": ver}
		preview, err := registryCall(s, ctx, "registry record preview", request)
		if err != nil {
			return err
		}
		if err = checkRegistryPreview(preview, ver, false, false); err != nil {
			return err
		}
		unchanged, err := registryCall(s, ctx, "registry record sync", request)
		if err != nil {
			return err
		}
		if unchanged.Data.SyncStatus != "UNCHANGED" || unchanged.Data.ResolvedVersionId != ver {
			return fmt.Errorf("unchanged sync mismatch")
		}
		if err = registryWaitUntil(ctx, change.Add(time.Second)); err != nil {
			return err
		}
		preview, err = registryCall(s, ctx, "registry record preview", request)
		if err != nil {
			return err
		}
		if err = checkRegistryPreview(preview, ver, true, false); err != nil {
			return err
		}
		// Preview is read-only: stored content must still refer to version 1.
		before, err := registryCall(s, ctx, "registry record get", request)
		if err != nil {
			return err
		}
		if !descriptorVersion(before.Data.Version.Descriptors, "1.0.0") {
			return fmt.Errorf("preview mutated content")
		}
		request["ChangeLog"] = "remote fixture changed"
		synced, err := registryCall(s, ctx, "registry record sync", request)
		if err != nil {
			return err
		}
		delete(request, "ChangeLog")
		next := synced.Data.CreatedVersion.VersionId
		if synced.Data.SyncStatus != "VERSION_CREATED" || next == "" || next == ver || synced.Data.ResolvedVersionId != ver {
			return fmt.Errorf("changed sync did not create version")
		}
		request["VersionId"] = next
		readback, err := registryCall(s, ctx, "registry record get", request)
		if err != nil {
			return err
		}
		if err = s.Assert(strings.ToLower(kind.descriptor)+".sync.changed", descriptorVersion(readback.Data.Version.Descriptors, "2.0.0") && readback.Data.Version.ChangeLog == "remote fixture changed"); err != nil {
			return err
		}
		if err = registryWaitUntil(ctx, fail.Add(time.Second)); err != nil {
			return err
		}
		preview, err = registryCall(s, ctx, "registry record preview", request)
		if err != nil {
			return err
		}
		if err = checkRegistryPreview(preview, next, false, true); err != nil {
			return err
		}
		failed, err := registryCall(s, ctx, "registry record sync", request)
		if err != nil {
			return err
		}
		if err = s.Assert(strings.ToLower(kind.descriptor)+".sync.failed", failed.Data.SyncStatus == "FAILED" && failed.Data.ErrorCode != "" && failed.Data.ErrorMessage != "" && failed.Data.ResolvedVersionId == next); err != nil {
			return err
		}
	}
	return nil
}

func descriptorVersion(raw, want string) bool {
	var d struct{ Version string }
	return json.Unmarshal([]byte(raw), &d) == nil && d.Version == want
}
func checkRegistryPreview(response registryResponse, version string, changed, failed bool) error {
	var p struct {
		StatusCode int
		Body       string
		HasUpdate  bool
		Error      string
	}
	if json.Unmarshal([]byte(response.Data.PreviewResult), &p) != nil || response.Data.ResolvedVersionId != version || p.HasUpdate != changed || (p.Error != "") != failed || (!failed && p.StatusCode != 200) {
		return fmt.Errorf("preview result mismatch")
	}
	return nil
}
func registryWaitUntil(ctx context.Context, at time.Time) error {
	timer := time.NewTimer(max(time.Until(at), 0))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func registryFixtureConfigured() error {
	u, err := url.Parse(os.Getenv("AGR_REGISTRY_FIXTURE_URL"))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("AGR_REGISTRY_FIXTURE_URL must identify the reviewed HTTPS metadata fixture")
	}
	return nil
}
