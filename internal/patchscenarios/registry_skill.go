package patchscenarios

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

const registrySkillMD = "---\nname: skill-fixture\ndescription: Disposable CLI regression fixture\n---\n# Skill fixture\nRun no operations.\n"

func registrySkillLifecycle(s *patchtest.Session) error {
	ctx, cancel := context.WithTimeout(s.Context, 3*time.Minute)
	defer cancel()
	created, err := registryCall(s, ctx, "registry create", map[string]any{"Name": fmt.Sprintf("cli-skill-%d", time.Now().UnixNano()), "ApprovalMode": "AUTO"})
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
	created, err = registryCall(s, ctx, "registry record create", map[string]any{"RegistryId": reg, "Name": "skill-fixture", "DescriptorType": "AGENT_SKILLS", "SkillSource": map[string]any{"Type": "TAR_PACKAGE"}})
	rec := created.Data.RecordId
	ver := created.Data.Version.VersionId
	if rec != "" {
		registryCleanup(s, "registry record", map[string]any{"RegistryId": reg, "RecordId": rec})
	}
	if err != nil {
		return err
	}
	if rec == "" || ver == "" {
		return fmt.Errorf("missing skill identity")
	}
	request := map[string]any{"RegistryId": reg, "RecordId": rec, "VersionId": ver}
	upload, err := registryCall(s, ctx, "registry skill-package upload-url", request)
	if err != nil {
		return err
	}
	if upload.Data.UploadURL == "" || upload.Data.ExpireTime == "" {
		return fmt.Errorf("missing upload URL/expiry")
	}
	var buf bytes.Buffer
	zip := gzip.NewWriter(&buf)
	writer := tar.NewWriter(zip)
	if err := writer.WriteHeader(&tar.Header{Name: "SKILL.md", Size: int64(len(registrySkillMD)), Mode: 0600}); err != nil {
		return err
	}
	if _, err := writer.Write([]byte(registrySkillMD)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := zip.Close(); err != nil {
		return err
	}
	if _, err := registryHTTP(ctx, http.MethodPut, upload.Data.UploadURL, buf.Bytes()); err != nil {
		return err
	}
	var ready registryResponse
	for {
		ready, err = registryCall(s, ctx, "registry record get", request)
		if err != nil {
			return err
		}
		if ready.Data.Version.ContentStatus == "READY" {
			break
		}
		switch ready.Data.Version.ContentStatus {
		case "FAILED", "INVALID", "EXPIRED":
			return fmt.Errorf("skill content rejected")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(buf.Bytes()))
	download, err := registryCall(s, ctx, "registry skill-package download-url", request)
	if err != nil {
		return err
	}
	data, err := registryHTTP(ctx, http.MethodGet, download.Data.DownloadURL, nil)
	if err != nil {
		return err
	}
	if err = s.Assert("skill.package.roundtrip", download.Data.ResolvedVersionId == ver && download.Data.SHA256 == digest && fmt.Sprintf("%x", sha256.Sum256(data)) == digest && ready.Data.Version.ContentSizeBytes == int64(buf.Len())); err != nil {
		return err
	}
	// Inline Markdown is a separate source union branch and can also replace content.
	updated, err := registryCall(s, ctx, "registry record update", map[string]any{"RegistryId": reg, "RecordId": rec, "SkillSource": map[string]any{"Type": "MANUAL", "SkillMd": registrySkillMD}, "ChangeLog": "inline replacement"})
	if err != nil {
		return err
	}
	next := updated.Data.Version.VersionId
	if next == "" || next == ver {
		return fmt.Errorf("inline content did not create version")
	}
	request["VersionId"] = next
	readback, err := registryCall(s, ctx, "registry record get", request)
	if err != nil {
		return err
	}
	var descriptors struct {
		AgentSkills struct {
			SkillMd struct{ InlineContent string }
		}
	}
	if err = json.Unmarshal([]byte(readback.Data.Version.Descriptors), &descriptors); err != nil {
		return fmt.Errorf("invalid skill descriptors")
	}
	return s.Assert("skill.inline.readback", readback.Data.Version.VersionId == next && descriptors.AgentSkills.SkillMd.InlineContent == registrySkillMD)
}

func registryHTTP(ctx context.Context, method, address string, body []byte) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("expected HTTPS transfer URL")
	}
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("invalid transfer request")
	}
	if method == http.MethodPut {
		req.Header.Set("Content-Type", "application/gzip")
	}
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fixture transfer failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fixture transfer HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}
