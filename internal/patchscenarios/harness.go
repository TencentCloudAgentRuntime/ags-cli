package patchscenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// harnessAssertions cover every Harness and ModelProvider Action through the
// candidate CLI. Each one is asserted only after checking the readback.
var harnessAssertions = []string{
	"engine.list",
	"provider.create", "provider.get", "provider.list", "provider.update", "provider.revisions", "provider.in-use", "provider.delete",
	"harness.create", "harness.publish.first", "harness.get", "harness.list", "harness.update", "harness.revision.create",
	"harness.revision.get", "harness.revision.list", "harness.publish", "harness.publish.idempotent", "harness.rollback",
	"harness.diff", "harness.status", "harness.delete",
	"input.validation",
}

// harnessFixture names reviewed, pre-existing inputs the scenario cannot invent:
// a role the platform accepts, a VPC subnet and security group, and a TokenHub
// API key with a registered model.
type harnessFixture struct {
	roleArn, subnetID, securityGroupID, apiKeyID, model, engine, engineVersion string
	initializeConfiguration                                                    string
}

var harnessFixtureVars = []string{"AGR_HARNESS_ROLE_ARN", "AGR_HARNESS_SUBNET_ID", "AGR_HARNESS_SECURITY_GROUP_ID", "AGR_HARNESS_TOKENHUB_API_KEY_ID", "AGR_HARNESS_MODEL"}

func harnessFixtureFromEnv() (harnessFixture, error) {
	fixture := harnessFixture{
		roleArn:                 os.Getenv("AGR_HARNESS_ROLE_ARN"),
		subnetID:                os.Getenv("AGR_HARNESS_SUBNET_ID"),
		securityGroupID:         os.Getenv("AGR_HARNESS_SECURITY_GROUP_ID"),
		apiKeyID:                os.Getenv("AGR_HARNESS_TOKENHUB_API_KEY_ID"),
		model:                   os.Getenv("AGR_HARNESS_MODEL"),
		engine:                  harnessEnvOr("AGR_HARNESS_ENGINE", "pi"),
		engineVersion:           os.Getenv("AGR_HARNESS_ENGINE_VERSION"),
		initializeConfiguration: os.Getenv("AGR_HARNESS_INITIALIZE_CONFIGURATION"),
	}
	for _, name := range harnessFixtureVars {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			return harnessFixture{}, fmt.Errorf("%s must identify a reviewed harness fixture", name)
		}
	}
	return fixture, nil
}

func harnessEnvOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

type harnessEnvelope struct {
	Status  string
	Data    map[string]any
	Failure *struct {
		Code, Kind, Message string
		Details             map[string]any
	}
}

// harnessCLI runs one command and returns the envelope even on failure, so
// cleanup can recover IDs that were created before a later step failed.
func harnessCLI(s *patchtest.Session, ctx context.Context, args ...string) (harnessEnvelope, error) {
	raw, callErr := s.CLI(ctx, append(args, "-o", "json")...)
	var out harnessEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return out, fmt.Errorf("%s: invalid CLI envelope", strings.Join(args[:min(3, len(args))], " "))
	}
	if callErr != nil || out.Status != "succeeded" || out.Failure != nil {
		if out.Failure != nil {
			return out, fmt.Errorf("%s: %s (%s)", strings.Join(args[:min(3, len(args))], " "), out.Failure.Code, out.Failure.Message)
		}
		return out, fmt.Errorf("%s: CLI call failed", strings.Join(args[:min(3, len(args))], " "))
	}
	return out, nil
}

func harnessObject(value any) map[string]any { out, _ := value.(map[string]any); return out }

func harnessText(value any) string { out, _ := value.(string); return out }

func harnessInt(value any) int64 {
	if n, ok := value.(json.Number); ok {
		out, _ := n.Int64()
		return out
	}
	return 0
}

func harnessRevisionStatus(result harnessEnvelope, key string) string {
	return harnessText(harnessObject(result.Data[key])["Status"])
}

// harnessLastFailure keeps the first failing CLI step (command words, failure
// code and kind, never request values) for opt-in live diagnostics.
var harnessLastFailure string

func runHarnessLifecycle(s *patchtest.Session) (err error) {
	defer func() {
		if err != nil && harnessLastFailure == "" {
			harnessLastFailure = err.Error()
		}
	}()
	fixture, err := harnessFixtureFromEnv()
	if err != nil {
		return err
	}
	ctx := s.Context
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)

	// Validation runs first and must never reach the service as a write.
	for _, args := range [][]string{
		{"harness", "create", "--name", "agr-e2e-invalid", "--engine", fixture.engine, "--model", "not-a-reference", "--role-arn", fixture.roleArn},
		{"harness", "delete", "hns-unused"},
		{"model-provider", "create", "--name", "agr-e2e-invalid", "--model", "hy3"},
	} {
		result, err := harnessCLI(s, ctx, args...)
		if err := s.Assert("input.validation", err != nil && result.Failure != nil && result.Failure.Kind == "usage"); err != nil {
			return err
		}
	}

	engines, err := harnessCLI(s, ctx, "harness", "engine", "list")
	if err != nil {
		return err
	}
	version := fixture.engineVersion
	for _, item := range engines.Data["HarnessEngineSet"].([]any) {
		if harnessText(harnessObject(item)["HarnessEngine"]) == fixture.engine && version == "" {
			versions, _ := harnessObject(item)["HarnessEngineVersion"].([]any)
			if len(versions) > 0 {
				version = harnessText(versions[0])
			}
		}
	}
	if err := s.Assert("engine.list", version != ""); err != nil {
		return err
	}

	providerName := "agr-e2e-mp-" + suffix
	created, err := harnessCLI(s, ctx, "model-provider", "create", "--name", providerName, "--tokenhub-api-key-id", fixture.apiKeyID, "--model", fixture.model, "--tags", "agr-e2e=harness")
	providerID := harnessText(harnessObject(created.Data["ModelProvider"])["ModelProviderId"])
	if providerID != "" {
		s.Cleanup(func(ctx context.Context) error { return harnessDeleteProvider(s, ctx, providerID) })
	}
	if err != nil {
		return err
	}
	spec := harnessObject(harnessObject(created.Data["ModelProvider"])["Spec"])
	models, _ := spec["Models"].([]any)
	if err := s.Assert("provider.create", len(models) == 1 && harnessText(harnessObject(models[0])["ID"]) == fixture.model && harnessText(harnessObject(harnessObject(spec["Source"])["TokenHub"])["ApiKeyId"]) == fixture.apiKeyID); err != nil {
		return err
	}
	got, err := harnessCLI(s, ctx, "model-provider", "get", providerID)
	if err != nil {
		return err
	}
	if err := s.Assert("provider.get", harnessText(harnessObject(got.Data["ModelProvider"])["ModelProviderName"]) == providerName); err != nil {
		return err
	}
	listed, err := harnessCLI(s, ctx, "model-provider", "list", "--name", providerName, "--type", "TOKENHUB")
	if err != nil {
		return err
	}
	set, _ := listed.Data["ModelProviderSet"].([]any)
	if err := s.Assert("provider.list", len(set) == 1 && harnessInt(listed.Data["TotalCount"]) == 1); err != nil {
		return err
	}

	harnessName := "agr-e2e-hns-" + suffix
	createArgs := []string{"harness", "create", "--name", harnessName, "--engine", fixture.engine, "--engine-version", version,
		"--model", providerID + ":" + fixture.model, "--role-arn", fixture.roleArn,
		"--subnet-id", fixture.subnetID, "--security-group-id", fixture.securityGroupID, "--system-prompt", "revision one", "--tags", "agr-e2e=harness", "--wait"}
	if fixture.initializeConfiguration != "" {
		createArgs = append(createArgs, "--initialize-configuration", fixture.initializeConfiguration)
	}
	harness, err := harnessCLI(s, ctx, createArgs...)
	harnessID := harnessText(harnessObject(harness.Data["Harness"])["HarnessId"])
	if harnessID == "" && harness.Failure != nil {
		harnessID = harnessText(harness.Failure.Details["HarnessId"])
	}
	if harnessID != "" {
		// Registered after the provider cleanup, so it runs first.
		s.Cleanup(func(ctx context.Context) error { return harnessDeleteHarness(s, ctx, harnessID) })
	}
	if err := s.Assert("harness.create", harnessID != ""); err != nil {
		return err
	}
	if err != nil {
		return err
	}
	if err := s.Assert("harness.publish.first", harnessRevisionStatus(harness, "Revision") == "PUBLISHED"); err != nil {
		return err
	}

	inUse, err := harnessCLI(s, ctx, "model-provider", "delete", providerID, "--yes")
	if err := s.Assert("provider.in-use", err != nil && inUse.Failure != nil && inUse.Failure.Code == "ResourceInUse"); err != nil {
		return err
	}

	described, err := harnessCLI(s, ctx, "harness", "get", harnessID)
	if err != nil {
		return err
	}
	resources := harnessObject(harnessObject(described.Data["Harness"])["ResourceInfo"])
	if err := s.Assert("harness.get", harnessText(resources["DeploymentId"]) != "" && harnessText(resources["ToolId"]) != ""); err != nil {
		return err
	}
	harnesses, err := harnessCLI(s, ctx, "harness", "list", "--name", harnessName, "--tag", "agr-e2e=harness")
	if err != nil {
		return err
	}
	hset, _ := harnesses.Data["HarnessSet"].([]any)
	if err := s.Assert("harness.list", len(hset) == 1 && harnessText(harnessObject(hset[0])["HarnessId"]) == harnessID); err != nil {
		return err
	}

	// A provider update creates a provider revision; the harness picks it up
	// only when a new harness revision is created.
	if _, err := harnessCLI(s, ctx, "model-provider", "update", providerID, "--model", fixture.model+":TEXT"); err != nil {
		return err
	}
	providerRevisions, err := harnessCLI(s, ctx, "model-provider", "revision", "list", providerID)
	if err != nil {
		return err
	}
	if err := s.Assert("provider.update", harnessInt(providerRevisions.Data["TotalCount"]) == 2); err != nil {
		return err
	}
	providerRevision, err := harnessCLI(s, ctx, "model-provider", "revision", "get", providerID, "2")
	if err := s.Assert("provider.revisions", err == nil && harnessInt(harnessObject(providerRevision.Data["ModelProviderRevision"])["Revision"]) == 2); err != nil {
		return err
	}

	if _, err := harnessCLI(s, ctx, "harness", "update", harnessID, "--system-prompt", "revision two"); err != nil {
		return err
	}
	diff, err := harnessCLI(s, ctx, "harness", "revision", "diff", harnessID, "live", "draft")
	if err != nil {
		return err
	}
	changes, _ := diff.Data["Changes"].([]any)
	if err := s.Assert("harness.update", diff.Data["Changed"] == true && len(changes) == 1 && harnessText(harnessObject(changes[0])["Path"]) == "SystemPrompt"); err != nil {
		return err
	}
	status, err := harnessCLI(s, ctx, "harness", "status", harnessID)
	if err != nil {
		return err
	}
	if err := s.Assert("harness.status", status.Data["DraftChanged"] == true && harnessText(status.Data["Next"]) == fmt.Sprintf("agr harness revision create %s --publish --wait", harnessID)); err != nil {
		return err
	}

	second, err := harnessCLI(s, ctx, "harness", "revision", "create", harnessID, "--publish", "--wait")
	if err != nil {
		return err
	}
	if err := s.Assert("harness.revision.create", harnessInt(harnessObject(second.Data["HarnessRevision"])["Revision"]) == 2); err != nil {
		return err
	}
	if err := s.Assert("harness.publish", harnessRevisionStatus(second, "HarnessRevision") == "PUBLISHED"); err != nil {
		return err
	}
	revision, err := harnessCLI(s, ctx, "harness", "revision", "get", harnessID, "2")
	if err != nil {
		return err
	}
	pinned := harnessObject(harnessObject(harnessObject(revision.Data["HarnessRevision"])["Spec"])["DefaultModel"])
	if err := s.Assert("harness.revision.get", harnessInt(pinned["Revision"]) == 2); err != nil {
		return err
	}
	repeat, err := harnessCLI(s, ctx, "harness", "revision", "publish", harnessID, "2")
	if err := s.Assert("harness.publish.idempotent", err == nil && repeat.Data["AlreadyPublished"] == true); err != nil {
		return err
	}
	revisionDiff, err := harnessCLI(s, ctx, "harness", "revision", "diff", harnessID, "1", "2")
	if err != nil {
		return err
	}
	if err := s.Assert("harness.diff", revisionDiff.Data["Changed"] == true); err != nil {
		return err
	}

	rollback, err := harnessCLI(s, ctx, "harness", "revision", "publish", harnessID, "1", "--wait")
	if err != nil {
		return err
	}
	list, err := harnessCLI(s, ctx, "harness", "revision", "list", harnessID)
	if err != nil {
		return err
	}
	statuses := map[int64]string{}
	for _, item := range list.Data["HarnessRevisionSet"].([]any) {
		statuses[harnessInt(harnessObject(item)["Revision"])] = harnessText(harnessObject(item)["Status"])
	}
	if err := s.Assert("harness.rollback", harnessRevisionStatus(rollback, "HarnessRevision") == "PUBLISHED" && statuses[2] == "SUPERSEDED"); err != nil {
		return err
	}
	if err := s.Assert("harness.revision.list", len(statuses) == 2 && statuses[1] == "PUBLISHED"); err != nil {
		return err
	}

	if err := harnessDeleteHarness(s, ctx, harnessID); err != nil {
		return err
	}
	if err := s.Assert("harness.delete", true); err != nil {
		return err
	}
	if err := harnessDeleteProvider(s, ctx, providerID); err != nil {
		return err
	}
	return s.Assert("provider.delete", true)
}

// harnessDeleteHarness deletes and waits, then removes the Sandbox Tool that
// DeleteHarness intentionally keeps, so the scenario leaves nothing behind.
func harnessDeleteHarness(s *patchtest.Session, ctx context.Context, harnessID string) error {
	described, getErr := harnessCLI(s, ctx, "harness", "get", harnessID)
	if getErr != nil {
		if described.Failure != nil && described.Failure.Kind == "not_found" {
			return nil
		}
		return getErr
	}
	toolID := harnessText(harnessObject(harnessObject(described.Data["Harness"])["ResourceInfo"])["ToolId"])
	if _, err := harnessCLI(s, ctx, "harness", "delete", harnessID, "--yes", "--wait"); err != nil {
		return err
	}
	if after, err := harnessCLI(s, ctx, "harness", "get", harnessID); err == nil || after.Failure == nil || after.Failure.Kind != "not_found" {
		return errors.New("harness still readable after delete")
	}
	if toolID == "" {
		return nil
	}
	if _, err := harnessCLI(s, ctx, "tool", "delete", toolID, "--yes", "--wait"); err != nil {
		return err
	}
	return nil
}

func harnessDeleteProvider(s *patchtest.Session, ctx context.Context, providerID string) error {
	result, err := harnessCLI(s, ctx, "model-provider", "delete", providerID, "--yes")
	if err != nil && (result.Failure == nil || result.Failure.Kind != "not_found") {
		return err
	}
	if after, err := harnessCLI(s, ctx, "model-provider", "get", providerID); err == nil || after.Failure == nil || after.Failure.Kind != "not_found" {
		return errors.New("model provider still readable after delete")
	}
	return nil
}
