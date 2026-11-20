//go:build preview

package harnesscli

import (
	"fmt"
	"io"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/textview"
)

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

// EngineLabel renders engine@version.
func EngineLabel(spec apivalue.Object) string {
	if spec.String("HarnessEngine") == "" {
		return "-"
	}
	return spec.String("HarnessEngine") + "@" + spec.String("HarnessEngineVersion")
}

// ModelsLabel renders provider:model selections, marking the default with *.
func ModelsLabel(spec apivalue.Object) string {
	defaultModel := spec.Object("DefaultModel")
	var parts []string
	for _, provider := range spec.Objects("ModelProviders") {
		id := provider.String("ModelProviderId")
		if revision := provider.Int64("Revision"); revision > 0 {
			id = fmt.Sprintf("%s@%d", id, revision)
		}
		models, _ := provider["Models"].([]any)
		for _, model := range models {
			name := fmt.Sprint(model)
			label := id + ":" + name
			if defaultModel != nil && defaultModel.String("ModelProviderId") == provider.String("ModelProviderId") && defaultModel.String("Model") == name {
				label += " (default)"
			}
			parts = append(parts, label)
		}
	}
	return orDash(strings.Join(parts, ", "))
}

func registryLabel(items []apivalue.Object) string {
	var parts []string
	for _, item := range items {
		ref := item.Object("Source").Object("Registry")
		parts = append(parts, ref.String("RegistryId")+"/"+ref.String("RecordId")+"/"+ref.String("VersionId"))
	}
	return orDash(strings.Join(parts, ", "))
}

func mcpLabel(items []apivalue.Object) string {
	var parts []string
	for _, item := range items {
		source := item.Object("Source")
		target := ""
		if remote := source.Object("Remote"); remote != nil {
			target = remote.String("URL")
		} else if ref := source.Object("Registry"); ref != nil {
			target = ref.String("RegistryId") + "/" + ref.String("RecordId") + "/" + ref.String("VersionId")
		}
		parts = append(parts, item.String("Name")+" ("+target+")")
	}
	return orDash(strings.Join(parts, ", "))
}

func promptLabel(prompt string) string {
	line, _, multi := strings.Cut(strings.TrimSpace(prompt), "\n")
	if len(line) > 72 {
		line = line[:72] + "..."
	} else if multi {
		line += " ..."
	}
	return orDash(line)
}

func tagsLabel(tags []apivalue.Object) string {
	var parts []string
	for _, tag := range tags {
		parts = append(parts, tag.String("Key")+"="+tag.String("Value"))
	}
	return orDash(strings.Join(parts, ", "))
}

// SpecRows renders the shared Harness and HarnessRevision spec fields.
func SpecRows(spec apivalue.Object) []textview.KeyValue {
	rows := []textview.KeyValue{
		{Key: "Engine", Value: EngineLabel(spec)},
		{Key: "Models", Value: ModelsLabel(spec)},
		{Key: "System prompt", Value: promptLabel(spec.String("SystemPrompt"))},
		{Key: "Skills", Value: registryLabel(spec.Objects("Skills"))},
		{Key: "Plugins", Value: registryLabel(spec.Objects("Plugins"))},
		{Key: "MCP servers", Value: mcpLabel(spec.Objects("MCPServers"))},
	}
	if session := spec.Object("SessionConfiguration"); session != nil {
		rows = append(rows, textview.KeyValue{Key: "Session space", Value: orDash(session.String("SessionSpaceId"))})
	}
	return rows
}

// RenderHarness prints the harness draft and resources.
func RenderHarness(w io.Writer, harness apivalue.Object) {
	resources := harness.Object("ResourceInfo")
	rows := []textview.KeyValue{
		{Key: "Harness", Value: harness.String("HarnessId")},
		{Key: "Name", Value: harness.String("HarnessName")},
		{Key: "Status", Value: harness.String("Status")},
	}
	if reason := harness.String("StatusReason"); reason != "" {
		rows = append(rows, textview.KeyValue{Key: "Status reason", Value: reason})
	}
	rows = append(rows, SpecRows(harness.Object("Spec"))...)
	rows = append(rows,
		textview.KeyValue{Key: "Sandbox tool", Value: orDash(resources.String("ToolId"))},
		textview.KeyValue{Key: "Deployment", Value: orDash(resources.String("DeploymentId"))},
		textview.KeyValue{Key: "Tags", Value: tagsLabel(harness.Objects("Tags"))},
		textview.KeyValue{Key: "Updated", Value: harness.String("UpdatedTime")},
	)
	textview.PrintKV(w, rows)
}

// RenderRevision prints one harness revision.
func RenderRevision(w io.Writer, revision apivalue.Object) {
	rows := []textview.KeyValue{
		{Key: "Harness", Value: revision.String("HarnessId")},
		{Key: "Revision", Value: fmt.Sprintf("%d (%s)", revision.Int64("Revision"), revision.String("HarnessRevisionName"))},
		{Key: "Status", Value: revision.String("Status")},
	}
	if reason := revision.String("StatusReason"); reason != "" {
		rows = append(rows, textview.KeyValue{Key: "Status reason", Value: reason})
	}
	rows = append(rows, SpecRows(revision.Object("Spec"))...)
	rows = append(rows, textview.KeyValue{Key: "Updated", Value: revision.String("UpdatedTime")})
	textview.PrintKV(w, rows)
}

// RenderHarnessList prints the list table.
func RenderHarnessList(w io.Writer, harnesses []apivalue.Object, total int) {
	if len(harnesses) == 0 {
		fmt.Fprintln(w, "No harnesses found")
		return
	}
	rows := make([][]string, 0, len(harnesses))
	for _, harness := range harnesses {
		rows = append(rows, []string{
			harness.String("HarnessId"), harness.String("HarnessName"), EngineLabel(harness.Object("Spec")),
			harness.String("Status"), orDash(harness.Object("ResourceInfo").String("DeploymentId")), harness.String("UpdatedTime"),
		})
	}
	textview.PrintTableWithPagination(w, []string{"ID", "NAME", "ENGINE", "STATUS", "DEPLOYMENT", "UPDATED"}, rows, len(harnesses), total)
}

// RenderRevisionList prints the revision history table.
func RenderRevisionList(w io.Writer, revisions []apivalue.Object, total int) {
	if len(revisions) == 0 {
		fmt.Fprintln(w, "No revisions found")
		return
	}
	rows := make([][]string, 0, len(revisions))
	for _, revision := range revisions {
		rows = append(rows, []string{
			fmt.Sprint(revision.Int64("Revision")), revision.String("HarnessRevisionName"), revision.String("Status"),
			EngineLabel(revision.Object("Spec")), revision.String("UpdatedTime"), revision.String("StatusReason"),
		})
	}
	textview.PrintTableWithPagination(w, []string{"REVISION", "NAME", "STATUS", "ENGINE", "UPDATED", "REASON"}, rows, len(revisions), total)
}

// SourceLabel renders the one configured model provider source.
func SourceLabel(spec apivalue.Object) string {
	source := spec.Object("Source")
	if tokenHub := source.Object("TokenHub"); tokenHub != nil {
		return "TokenHub (ApiKeyId " + tokenHub.String("ApiKeyId") + ")"
	}
	if openAI := source.Object("OpenAICompatible"); openAI != nil {
		label := "OpenAI-compatible " + openAI.String("BaseURL")
		if credential := openAI.Object("Credential"); credential != nil {
			label += fmt.Sprintf(" (credential %s/%s/%s)", credential.String("CredentialProviderId"), credential.String("UserId"), credential.String("Scope"))
		}
		return label
	}
	return "-"
}

// RegisteredModelsLabel renders id[inputs] for registered models.
func RegisteredModelsLabel(spec apivalue.Object) string {
	var parts []string
	for _, model := range spec.Objects("Models") {
		inputs, _ := model["Input"].([]any)
		var names []string
		for _, input := range inputs {
			names = append(names, fmt.Sprint(input))
		}
		label := model.String("ID") + "[" + strings.Join(names, ",") + "]"
		if reasoning, ok := model["Reasoning"].(bool); ok && reasoning {
			label += " reasoning"
		}
		parts = append(parts, label)
	}
	return orDash(strings.Join(parts, ", "))
}

// RenderProviderList prints the model provider table.
func RenderProviderList(w io.Writer, providers []apivalue.Object, total int) {
	if len(providers) == 0 {
		fmt.Fprintln(w, "No model providers found")
		return
	}
	rows := make([][]string, 0, len(providers))
	for _, provider := range providers {
		spec := provider.Object("Spec")
		rows = append(rows, []string{provider.String("ModelProviderId"), provider.String("ModelProviderName"), sourceType(spec), RegisteredModelsLabel(spec), provider.String("UpdatedTime")})
	}
	textview.PrintTableWithPagination(w, []string{"ID", "NAME", "SOURCE", "MODELS", "UPDATED"}, rows, len(providers), total)
}

// RenderProviderRevisionList prints model provider revision history.
func RenderProviderRevisionList(w io.Writer, revisions []apivalue.Object, total int) {
	if len(revisions) == 0 {
		fmt.Fprintln(w, "No revisions found")
		return
	}
	rows := make([][]string, 0, len(revisions))
	for _, revision := range revisions {
		spec := revision.Object("Spec")
		rows = append(rows, []string{fmt.Sprint(revision.Int64("Revision")), revision.String("ModelProviderRevisionName"), sourceType(spec), RegisteredModelsLabel(spec), revision.String("CreatedTime")})
	}
	textview.PrintTableWithPagination(w, []string{"REVISION", "NAME", "SOURCE", "MODELS", "CREATED"}, rows, len(revisions), total)
}

func sourceType(spec apivalue.Object) string {
	switch source := spec.Object("Source"); {
	case source.Object("TokenHub") != nil:
		return "TOKENHUB"
	case source.Object("OpenAICompatible") != nil:
		return "OPENAI_COMPATIBLE"
	}
	return "-"
}

// RenderModelProvider prints a provider or provider revision.
func RenderModelProvider(w io.Writer, provider apivalue.Object) {
	spec := provider.Object("Spec")
	rows := []textview.KeyValue{{Key: "Model provider", Value: provider.String("ModelProviderId")}}
	if name := provider.String("ModelProviderName"); name != "" {
		rows = append(rows, textview.KeyValue{Key: "Name", Value: name})
	}
	if revision := provider.Int64("Revision"); revision > 0 {
		rows = append(rows, textview.KeyValue{Key: "Revision", Value: fmt.Sprintf("%d (%s)", revision, provider.String("ModelProviderRevisionName"))})
	}
	rows = append(rows,
		textview.KeyValue{Key: "Source", Value: SourceLabel(spec)},
		textview.KeyValue{Key: "Models", Value: RegisteredModelsLabel(spec)},
	)
	if _, ok := provider["Tags"]; ok {
		rows = append(rows, textview.KeyValue{Key: "Tags", Value: tagsLabel(provider.Objects("Tags"))})
	}
	rows = append(rows, textview.KeyValue{Key: "Updated", Value: provider.String("UpdatedTime")})
	textview.PrintKV(w, rows)
}
