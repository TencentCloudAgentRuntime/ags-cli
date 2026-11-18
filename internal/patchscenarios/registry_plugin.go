package patchscenarios

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

type registryMetadataEntry struct{ Name, Value string }
type registryHarnessVersion struct{ HarnessEngine, HarnessEngineVersion string }

func registryPluginLifecycle(s *patchtest.Session) error {
	ctx := s.Context
	created, err := registryCall(s, ctx, "registry create", map[string]any{
		"Name": fmt.Sprintf("cli-plugin-%d", time.Now().UnixNano()), "ApprovalMode": "AUTO",
	})
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

	firstBytes := []byte("plugin-fixture-v1\x00")
	metadata := []registryMetadataEntry{{Name: "publisher", Value: "CLI fixture"}}
	harnesses := []registryHarnessVersion{{HarnessEngine: "pi", HarnessEngineVersion: "1.3.0"}}
	created, err = registryCall(s, ctx, "registry record create", map[string]any{
		"RegistryId": reg, "Name": "plugin-fixture", "DescriptorType": "PLUGIN",
		"FileSource": registryPluginFile(firstBytes), "Metadata": metadata, "SupportedHarnessVersions": harnesses,
	})
	rec := created.Data.RecordId
	if rec != "" {
		registryCleanup(s, "registry record", map[string]any{"RegistryId": reg, "RecordId": rec})
	}
	if err != nil {
		return err
	}
	if rec == "" {
		return fmt.Errorf("missing RecordId")
	}
	first, err := registryUploadPlugin(s, reg, rec, created, firstBytes)
	if err != nil {
		return fmt.Errorf("create Plugin upload: %w", err)
	}
	if err := s.Assert("plugin.create.file", slices.Equal(first.Data.Version.Metadata, metadata) && slices.Equal(first.Data.Version.SupportedHarnessVersions, harnesses)); err != nil {
		return err
	}

	nextBytes := []byte("plugin-fixture-v2\x00")
	next, err := registryCall(s, ctx, "registry record update", map[string]any{
		"RegistryId": reg, "RecordId": rec, "FileSource": registryPluginFile(nextBytes),
	})
	if err != nil {
		return err
	}
	second, err := registryUploadPlugin(s, reg, rec, next, nextBytes)
	if err != nil {
		return fmt.Errorf("update Plugin upload: %w", err)
	}
	if err := s.Assert("plugin.update.file", second.Data.Version.VersionId != first.Data.Version.VersionId); err != nil {
		return err
	}

	reused, err := registryCall(s, ctx, "registry record update", map[string]any{
		"RegistryId": reg, "RecordId": rec,
		"FileSource":               map[string]string{"Type": "VERSION", "SourceVersionId": first.Data.Version.VersionId},
		"Metadata":                 []registryMetadataEntry{},
		"SupportedHarnessVersions": []registryHarnessVersion{},
	})
	if err != nil {
		return err
	}
	reusedID := reused.Data.Version.VersionId
	if reusedID == "" || reusedID == first.Data.Version.VersionId || reusedID == second.Data.Version.VersionId || reused.Data.UploadURL != "" {
		return fmt.Errorf("reuse did not return a distinct version without an upload")
	}
	got, err := registryReadyPlugin(s, reg, rec, reusedID)
	if err != nil {
		return err
	}
	version := got.Data.Version
	if err := s.Assert("plugin.update.reuse", version.ContentSHA256 == first.Data.Version.ContentSHA256 &&
		version.ContentSizeBytes == int64(len(firstBytes)) &&
		version.Metadata != nil && len(version.Metadata) == 0 &&
		version.SupportedHarnessVersions != nil && len(version.SupportedHarnessVersions) == 0); err != nil {
		return err
	}

	for _, source := range []map[string]any{
		{"Type": "FILE", "SizeBytes": 0},
		{"Type": "FILE", "SizeBytes": 54525953},
		{"Type": "FILE", "SizeBytes": 1, "SHA256": strings.Repeat("A", 64)},
		{"Type": "VERSION", "SourceVersionId": first.Data.Version.VersionId, "SizeBytes": 1},
	} {
		result, err := registryCall(s, ctx, "registry record update", map[string]any{"RegistryId": reg, "RecordId": rec, "FileSource": source})
		if err == nil || result.Failure == nil || !strings.HasPrefix(result.Failure.Code, "InvalidParameter") {
			return fmt.Errorf("invalid Plugin file source was not rejected with InvalidParameter")
		}
	}
	return s.Assert("plugin.input.boundaries", true)
}

func registryPluginFile(body []byte) map[string]any {
	return map[string]any{"Type": "FILE", "SizeBytes": len(body), "SHA256": fmt.Sprintf("%x", sha256.Sum256(body))}
}

func registryUploadPlugin(s *patchtest.Session, reg, rec string, upload registryResponse, body []byte) (registryResponse, error) {
	ver := upload.Data.Version.VersionId
	expiry, err := time.Parse(time.RFC3339, upload.Data.ExpireTime)
	validExpiry := err == nil && time.Until(expiry) > 0
	if ver == "" || upload.Data.UploadURL == "" || !validExpiry || upload.Data.ContentStatus != "UPLOADING" {
		return registryResponse{}, fmt.Errorf("incomplete upload session (version ID present: %t, URL present: %t, future expiry: %t, status: %q)",
			ver != "", upload.Data.UploadURL != "", validExpiry, upload.Data.ContentStatus)
	}
	if _, err := registryHTTP(s.Context, http.MethodPut, upload.Data.UploadURL, "application/octet-stream", body); err != nil {
		return registryResponse{}, err
	}
	ready, err := registryReadyPlugin(s, reg, rec, ver)
	if err != nil {
		return registryResponse{}, err
	}
	if ready.Data.Version.ContentSHA256 != fmt.Sprintf("%x", sha256.Sum256(body)) || ready.Data.Version.ContentSizeBytes != int64(len(body)) {
		return registryResponse{}, fmt.Errorf("uploaded Plugin size or digest does not match readback")
	}
	return ready, nil
}

func registryReadyPlugin(s *patchtest.Session, reg, rec, ver string) (registryResponse, error) {
	ctx, cancel := context.WithTimeout(s.Context, 90*time.Second)
	defer cancel()
	for {
		got, err := registryCall(s, ctx, "registry record get", map[string]any{"RegistryId": reg, "RecordId": rec, "VersionId": ver})
		if err != nil {
			return registryResponse{}, err
		}
		if got.Data.Version.VersionId != ver {
			return registryResponse{}, fmt.Errorf("readback did not resolve the requested Plugin version")
		}
		switch got.Data.Version.ContentStatus {
		case "READY":
			return got, nil
		case "FAILED", "INVALID", "EXPIRED":
			return registryResponse{}, fmt.Errorf("content for the Plugin version did not become READY")
		}
		select {
		case <-ctx.Done():
			return registryResponse{}, fmt.Errorf("wait for Plugin content: %w", ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
}
