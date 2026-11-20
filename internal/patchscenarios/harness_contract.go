package patchscenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

const harnessContractFixtureEnv = "AGR_HARNESS_CONTRACT_FIXTURE"

type harnessContractFixture struct {
	SkillReference, PluginReference, MCPReference, SessionSpaceID       string
	MCPURL, MCPCredential, OpenAIBaseURL, OpenAICredential, OpenAIModel string
}

func loadHarnessContractFixture() (harnessContractFixture, error) {
	var fixture harnessContractFixture
	if err := json.Unmarshal([]byte(os.Getenv(harnessContractFixtureEnv)), &fixture); err != nil {
		return fixture, errors.New("AGR_HARNESS_CONTRACT_FIXTURE must contain reviewed contract fixture JSON")
	}
	for _, value := range []string{fixture.SkillReference, fixture.PluginReference, fixture.MCPReference, fixture.MCPCredential, fixture.OpenAICredential} {
		if parts := strings.Split(value, "/"); len(parts) != 3 || slices.Contains(parts, "") {
			return fixture, errors.New("AGR_HARNESS_CONTRACT_FIXTURE must specify pinned Registry references and complete managed credential references")
		}
	}
	for _, value := range []string{fixture.MCPURL, fixture.OpenAIBaseURL} {
		address, err := url.Parse(value)
		if err != nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" {
			return fixture, errors.New("AGR_HARNESS_CONTRACT_FIXTURE must use HTTPS fixture URLs without embedded credentials or query parameters")
		}
	}
	if strings.TrimSpace(fixture.SessionSpaceID) == "" || strings.TrimSpace(fixture.OpenAIModel) == "" {
		return fixture, errors.New("AGR_HARNESS_CONTRACT_FIXTURE must specify a SessionSpaceID and OpenAIModel")
	}
	return fixture, nil
}

const harnessMCPHeader = "Bearer ${credential}"

func (f harnessContractFixture) attachmentArgs() []string {
	return []string{
		"--skill", f.SkillReference, "--plugin", f.PluginReference,
		"--mcp", "registry=" + f.MCPReference, "--mcp", "remote=" + f.MCPURL,
		"--mcp-header", "remote:Authorization=" + harnessMCPHeader,
		"--mcp-credential", "remote=" + f.MCPCredential, "--session-space-id", f.SessionSpaceID,
	}
}

func registrySourceMatches(source map[string]any, reference string) bool {
	registry := harnessObject(source["Registry"])
	return strings.Join([]string{harnessText(registry["RegistryId"]), harnessText(registry["RecordId"]), harnessText(registry["VersionId"])}, "/") == reference
}

func credentialMatches(value any, reference string) bool {
	credential := harnessObject(value)
	return strings.Join([]string{harnessText(credential["CredentialProviderId"]), harnessText(credential["UserId"]), harnessText(credential["Scope"])}, "/") == reference
}

// Each result has its own assertion so losing one submitted field cannot be
// hidden by successful creation, publication, or the other fields surviving.
func (f harnessContractFixture) checkAttachments(spec map[string]any) []struct {
	field string
	ok    bool
} {
	referenceMatches := func(field, reference string) bool {
		items, _ := spec[field].([]any)
		return len(items) == 1 && registrySourceMatches(harnessObject(harnessObject(items[0])["Source"]), reference)
	}
	servers, _ := spec["MCPServers"].([]any)
	byName := map[string]map[string]any{}
	for _, item := range servers {
		server := harnessObject(item)
		byName[harnessText(server["Name"])] = server
	}
	remote := byName["remote"]
	headers, _ := remote["Headers"].([]any)
	return []struct {
		field string
		ok    bool
	}{
		{"skills", referenceMatches("Skills", f.SkillReference)},
		{"plugins", referenceMatches("Plugins", f.PluginReference)},
		{"session", harnessText(harnessObject(spec["SessionConfiguration"])["SessionSpaceId"]) == f.SessionSpaceID},
		{"mcp-registry", len(servers) == 2 && registrySourceMatches(harnessObject(byName["registry"]["Source"]), f.MCPReference)},
		{"mcp-remote", harnessText(harnessObject(harnessObject(remote["Source"])["Remote"])["URL"]) == f.MCPURL},
		{"mcp-credential", credentialMatches(remote["Credential"], f.MCPCredential)},
		{"mcp-headers", len(headers) == 1 && harnessText(harnessObject(headers[0])["Name"]) == "Authorization" &&
			harnessText(harnessObject(headers[0])["Value"]) == harnessMCPHeader},
	}
}

func harnessAssert(s *patchtest.Session, id string, condition bool) error {
	if err := s.Assert(id, condition); err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}
	return nil
}

func (f harnessContractFixture) assertAttachments(s *patchtest.Session, draft, revision map[string]any) error {
	snapshot := f.checkAttachments(revision)
	for i, check := range f.checkAttachments(draft) {
		if err := harnessAssert(s, "harness.spec."+check.field, check.ok && snapshot[i].ok); err != nil {
			return err
		}
	}
	return nil
}

func assertClearedAttachments(s *patchtest.Session, draft, revision map[string]any) error {
	if draft == nil || revision == nil {
		return errors.New("missing Harness Spec for cleared attachment readback")
	}
	for _, field := range []string{"Skills", "Plugins", "MCPServers"} {
		if err := harnessAssert(s, "harness.clear."+field, emptyHarnessList(draft[field]) && emptyHarnessList(revision[field])); err != nil {
			return err
		}
	}
	return harnessAssert(s, "harness.clear.SessionConfiguration",
		clearedHarnessSession(draft["SessionConfiguration"]) && clearedHarnessSession(revision["SessionConfiguration"]))
}

func emptyHarnessList(value any) bool {
	list, ok := value.([]any)
	return value == nil || (ok && len(list) == 0)
}

func clearedHarnessSession(value any) bool {
	config := harnessObject(value)
	return value == nil || (config != nil && (config["SessionSpaceId"] == nil || config["SessionSpaceId"] == ""))
}

func verifyHarnessVPC(s *patchtest.Session, toolID string, fixture harnessFixture) error {
	tool, err := harnessCLI(s, s.Context, "tool", "get", toolID)
	if err != nil {
		return err
	}
	network := harnessObject(tool.Data["NetworkConfiguration"])
	vpc := harnessObject(network["VpcConfig"])
	subnets, _ := vpc["SubnetIds"].([]any)
	groups, _ := vpc["SecurityGroupIds"].([]any)
	return harnessAssert(s, "harness.initialize.vpc", tool.Data["RoleArn"] == fixture.roleArn &&
		network["NetworkMode"] == "VPC" && len(subnets) == 1 && subnets[0] == fixture.subnetID &&
		len(groups) == 1 && groups[0] == fixture.securityGroupID)
}

// Exercise OpenAI-compatible create and source replacement on a provider that
// does not serve the Harness under test. The running revisions keep TokenHub.
func verifyOpenAIProvider(s *patchtest.Session, f harnessContractFixture, name, tokenHubKey, tokenHubModel string) error {
	parts := strings.Split(f.OpenAICredential, "/")
	args := []string{"--base-url", f.OpenAIBaseURL, "--credential-provider-id", parts[0], "--credential-user-id", parts[1], "--credential-scope", parts[2]}
	ctx := s.Context
	created, err := harnessCLI(s, ctx, append([]string{"model-provider", "create", "--name", name, "--model", f.OpenAIModel}, args...)...)
	id := harnessText(harnessObject(created.Data["ModelProvider"])["ModelProviderId"])
	if id != "" {
		s.Cleanup(func(ctx context.Context) error { return harnessDeleteProvider(s, ctx, id) })
	}
	if err != nil {
		return err
	}
	if err := harnessAssert(s, "provider.openai.create", id != ""); err != nil {
		return err
	}
	initial, err := harnessCLI(s, ctx, "model-provider", "get", id)
	if err != nil {
		return err
	}
	if err := harnessAssert(s, "provider.openai.base-url", f.openAIBaseMatches(harnessProviderSource(initial.Data["ModelProvider"]))); err != nil {
		return err
	}
	if err := harnessAssert(s, "provider.openai.credential", f.openAICredentialMatches(harnessProviderSource(initial.Data["ModelProvider"]))); err != nil {
		return err
	}
	if _, err := harnessCLI(s, ctx, "model-provider", "update", id, "--tokenhub-api-key-id", tokenHubKey, "--model", tokenHubModel); err != nil {
		return err
	}
	old, err := harnessCLI(s, ctx, "model-provider", "revision", "get", id, "2")
	if err != nil {
		return err
	}
	source := harnessProviderSource(old.Data["ModelProviderRevision"])
	if err := harnessAssert(s, "provider.source.update", harnessText(harnessObject(source["TokenHub"])["ApiKeyId"]) == tokenHubKey && source["OpenAICompatible"] == nil); err != nil {
		return err
	}
	models := []map[string]any{{"ID": f.OpenAIModel, "Name": "AGR E2E model", "Input": []string{"TEXT"}, "Reasoning": true, "ContextWindow": 32768, "MaxOutputTokens": 4096}}
	modelJSON, err := json.Marshal(models)
	if err != nil {
		return err
	}
	if _, err := harnessCLI(s, ctx, append([]string{"model-provider", "update", id, "--models", string(modelJSON)}, args...)...); err != nil {
		return err
	}
	current, err := harnessCLI(s, ctx, "model-provider", "revision", "get", id, "3")
	if err != nil {
		return err
	}
	spec := harnessObject(harnessObject(current.Data["ModelProviderRevision"])["Spec"])
	source = harnessObject(spec["Source"])
	if err := harnessAssert(s, "provider.openai.update", f.openAIBaseMatches(source) && f.openAICredentialMatches(source)); err != nil {
		return err
	}
	if err := harnessAssert(s, "provider.model.capabilities", modelCapabilitiesMatch(spec, f.OpenAIModel)); err != nil {
		return err
	}
	return harnessDeleteProvider(s, ctx, id)
}

func modelCapabilitiesMatch(spec map[string]any, modelID string) bool {
	models, _ := spec["Models"].([]any)
	if len(models) != 1 {
		return false
	}
	model := harnessObject(models[0])
	input, _ := model["Input"].([]any)
	return model["ID"] == modelID && model["Name"] == "AGR E2E model" && model["Reasoning"] == true &&
		len(input) == 1 && input[0] == "TEXT" && harnessInt(model["ContextWindow"]) == 32768 && harnessInt(model["MaxOutputTokens"]) == 4096
}

func harnessProviderSource(value any) map[string]any {
	return harnessObject(harnessObject(harnessObject(value)["Spec"])["Source"])
}

func (f harnessContractFixture) openAIBaseMatches(source map[string]any) bool {
	return harnessText(harnessObject(source["OpenAICompatible"])["BaseURL"]) == f.OpenAIBaseURL && source["TokenHub"] == nil
}

func (f harnessContractFixture) openAICredentialMatches(source map[string]any) bool {
	return credentialMatches(harnessObject(source["OpenAICompatible"])["Credential"], f.OpenAICredential)
}
