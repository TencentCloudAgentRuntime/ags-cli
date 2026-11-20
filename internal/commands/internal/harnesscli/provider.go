//go:build preview

package harnesscli

import (
	"fmt"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

var credentialFlags = []string{"credential-provider-id", "credential-user-id", "credential-scope"}

// ProviderShortcuts are the model provider workflow flags.
var ProviderShortcuts = append([]string{"base-url", "tokenhub-api-key-id", "model", "models", "tags"}, credentialFlags...)

// ProviderFlags returns source, credential, model and tag shortcuts.
func ProviderFlags(update bool) []command.FlagSpec {
	replace := ""
	if update {
		replace = " Replaces the whole model list."
	}
	sourceNote := ""
	if update {
		sourceNote = " Replaces the current source, including its credential."
	}
	tags := "Custom tag as key=value; repeat for more."
	if update {
		tags += " Replaces custom tags; pass '' once to clear them. Changing only tags does not create a revision."
	}
	return []command.FlagSpec{
		StringFlag("base-url", "OpenAI-compatible service URL, such as https://llm.example.com/v1."+sourceNote),
		StringFlag("credential-provider-id", "SecretMultiUser credential provider for --base-url; use with --credential-user-id and --credential-scope"),
		StringFlag("credential-user-id", "Credential user ID for --base-url"),
		StringFlag("credential-scope", "Credential scope for --base-url"),
		StringFlag("tokenhub-api-key-id", "TokenHub API key ID. At publish time its value is embedded in the Sandbox Tool NetworkPolicy header-injection rule; identities allowed to describe the Tool can read the key."+sourceNote),
		StringArrayFlag("model", "Registered model as <model-id>[:TEXT,IMAGE]; repeat for more. Without a suffix the model is text-only."+replace, "--model hy3", "--model qwen-vl:TEXT,IMAGE"),
		{Name: "models", Usage: "Registered models as a JSON array, @file, or - for stdin, for capabilities such as Reasoning, ContextWindow and MaxOutputTokens." + replace, Type: command.FlagString, Workflow: true,
			Format: `[{"ID":"<model-id>","Name":"<display name>","Input":["TEXT"],"Reasoning":true,"ContextWindow":32768,"MaxOutputTokens":4096}]`},
		StringArrayFlag("tags", tags),
	}
}

// BuildProviderRequest assembles Create/ModifyModelProvider from shortcuts or --request.
func BuildProviderRequest(req command.Request, env Env, update bool) (map[string]any, error) {
	if RequestMode(req) {
		if err := RejectWithRequest(req, ProviderShortcuts...); err != nil {
			return nil, err
		}
		return env.Builder.Build(WithoutFlags(req, append(ProviderShortcuts, "dry-run")...))
	}
	request, err := env.Builder.Build(WithoutFlags(req, append(ProviderShortcuts, "dry-run")...))
	if err != nil {
		return nil, err
	}
	if err := ApplyProviderShortcuts(req, request, update); err != nil {
		return nil, err
	}
	if update && len(request) <= 1 {
		return nil, output.NewUsageError("NOTHING_TO_UPDATE", "no changes were specified",
			"Pass at least one flag, for example: agr model-provider update <model-provider-id> --model hy3 --model hy3-lite")
	}
	return request, nil
}

// ApplyProviderShortcuts merges source, models and tags into the request.
func ApplyProviderShortcuts(req command.Request, request map[string]any, update bool) error {
	if err := SingleStdin(req, "models"); err != nil {
		return err
	}
	baseURL, tokenHub := Changed(req, "base-url"), Changed(req, "tokenhub-api-key-id")
	if baseURL && tokenHub {
		return output.NewUsageError("FLAG_CONFLICT", "--base-url and --tokenhub-api-key-id cannot be combined", "Choose exactly one model source.")
	}
	if name := ChangedAny(req, credentialFlags...); name != "" && !baseURL {
		return output.NewUsageError("FLAG_CONFLICT", fmt.Sprintf("--%s requires --base-url", name), "Credentials apply only to an OpenAI-compatible source.")
	}
	switch {
	case baseURL:
		source := map[string]any{"BaseURL": strings.TrimSpace(String(req, "base-url"))}
		if ChangedAny(req, credentialFlags...) != "" {
			credential := map[string]any{}
			for flag, field := range map[string]string{"credential-provider-id": "CredentialProviderId", "credential-user-id": "UserId", "credential-scope": "Scope"} {
				value := strings.TrimSpace(String(req, flag))
				if value == "" {
					return output.NewUsageError("MISSING_REQUIRED_FLAG", fmt.Sprintf("--%s is required with a credential", flag),
						"Provide --credential-provider-id, --credential-user-id and --credential-scope together.")
				}
				credential[field] = value
			}
			source["Credential"] = credential
		}
		request["Source"] = map[string]any{"OpenAICompatible": source}
	case tokenHub:
		request["Source"] = map[string]any{"TokenHub": map[string]any{"ApiKeyId": strings.TrimSpace(String(req, "tokenhub-api-key-id"))}}
	case !update:
		return output.NewUsageError("MISSING_REQUIRED_FLAG", "a model source is required",
			"Provide --tokenhub-api-key-id <id>, or --base-url <url> for an OpenAI-compatible service.")
	}
	if Changed(req, "model") && Changed(req, "models") {
		return output.NewUsageError("FLAG_CONFLICT", "--model and --models cannot be combined", "Use --model for simple models or --models for full capabilities.")
	}
	if Changed(req, "model") {
		models, err := RegisteredModels(Strings(req, "model"))
		if err != nil {
			return err
		}
		request["Models"] = models
	}
	if Changed(req, "models") {
		var models []any
		if err := JSONFlag(req, "models", &models); err != nil {
			return err
		}
		request["Models"] = models
	}
	if Changed(req, "tags") {
		tags, err := Tags(Strings(req, "tags"))
		if err != nil {
			return err
		}
		request["Tags"] = tags
	}
	return nil
}
