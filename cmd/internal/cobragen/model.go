package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
)

func inputsFor(m apimeta.Member, fm *apimeta.FieldMapping) []apimeta.InputMapping {
	return apimeta.FieldInputs(m, fm)
}

func inputHelp(h *apimeta.Help, commandID, fieldName, flagName, fallback string) apimeta.InputHelp {
	if h != nil {
		if ch, ok := h.Commands[commandID]; ok {
			if fh, ok := ch.Fields[fieldName]; ok {
				if ih, ok := fh.Inputs[flagName]; ok {
					if ih.Usage == "" {
						ih.Usage = fh.Description
					}
					if ih.Usage == "" {
						ih.Usage = fallback
					}
					return ih
				}
				if fh.Description != "" {
					return apimeta.InputHelp{Usage: fh.Description}
				}
			}
		}
	}
	return apimeta.InputHelp{Usage: fallback}
}

func commandShortFromHelp(help *apimeta.Help, commandID string) string {
	if help != nil {
		if h := help.Commands[commandID]; h.Short != "" {
			return h.Short
		}
	}
	if help == nil {
		if h := apimeta.CommandHelpFor(commandID); h.Short != "" {
			return h.Short
		}
	}
	return "Run " + commandID
}

func commandLongFromHelp(help *apimeta.Help, commandID string) string {
	h := apimeta.CommandHelpFor(commandID)
	if help != nil {
		h = help.Commands[commandID]
	}
	return h.Long
}

func commandPreserveFlagOrderFromHelp(help *apimeta.Help, commandID string) bool {
	h := apimeta.CommandHelpFor(commandID)
	if help != nil {
		h = help.Commands[commandID]
	}
	return h.PreserveFlagOrder
}

func commandExamplesFromHelp(help *apimeta.Help, commandID string) []string {
	h := apimeta.CommandHelpFor(commandID)
	if help != nil {
		h = help.Commands[commandID]
	}
	return append([]string(nil), h.Examples...)
}

func groupShort(group string) string {
	switch group {
	case "instance":
		return "Manage sandbox instances"
	case "tool":
		return "Manage sandbox tools"
	case "apikey":
		return "Manage API keys"
	case "image":
		return "Manage images"
	case "pre-cache-image-task":
		return "Manage image pre-cache tasks"
	case "deployment":
		return "Manage deployments"
	case "session":
		return "Manage session spaces, sessions, and events"
	case "volume":
		return "Manage sandbox volumes"
	case "volume-template":
		return "Manage sandbox volume templates"
	case "harness":
		return "Manage managed agent harnesses and their revisions"
	case "model-provider":
		return "Manage model connections used by harnesses"
	}
	return "Manage " + group
}

func groupLong(group string) string {
	switch group {
	case "instance":
		return "Manage sandbox instances and related data-plane workflows."
	case "tool":
		return "Manage sandbox tools (templates). Tools define the type and capabilities of sandbox instances."
	case "apikey":
		return "Manage API keys for AGR sandbox access."
	case "deployment":
		return "Manage deployments and related data-plane workflows."
	case "session":
		return "Manage session spaces, persistent conversations, and their event history.\n\nStart by choosing or creating a space with 'agr session space list/create', then create a session and append events. SpaceId scopes the business data; UserId identifies a user in your application. Keep the same SpaceId, UserId, and SessionId when reading or updating a conversation. Use 'agr session event list' with pagination for the full history."
	case "volume":
		return "Manage sandbox volumes. A volume binds one concrete storage location that sandbox instances can mount."
	case "volume-template":
		return "Manage sandbox volume templates. A template derives or reuses a concrete volume per reuse key instead of binding one fixed path."
	case "harness":
		return "Manage managed agent harnesses. A harness is an editable draft configuration (Harness Engine, models, prompt, Skills, Plugins, MCP servers) plus immutable revisions that are published to a platform-managed Sandbox Tool and Deployment.\n\nLifecycle:\n  1. create      saves the draft, creates revision 1, and starts publishing it\n  2. update      edits the draft only; running instances are unchanged\n  3. revision create --publish   freezes the draft and publishes it\n  4. revision publish <id> <rev>  republishes any earlier revision (rollback)\n\nPublishing affects only instances created afterwards. PUBLISHED means the platform resources are active, not that an instance is ready to chat; verify with 'agr harness endpoint --check'. Delete removes the Deployment and harness records but keeps the Sandbox Tool, model providers, and session spaces."
	case "model-provider":
		return "Manage model connections. A model provider records one model service source (OpenAI-compatible endpoint or TokenHub) and its registered models. Every create or update saves a new immutable revision; harness revisions pin the latest provider revision when they are created, so provider changes reach a harness only after 'agr harness revision create'."
	}
	return ""
}

func parserForField(m apimeta.Member, fm *apimeta.FieldMapping) string {
	if fm != nil && fm.Parser != "" {
		return fm.Parser
	}
	inputType := apimeta.ScalarFlagType(m)
	if fm != nil && len(fm.Inputs) > 0 {
		inputType = fm.Inputs[0].Type
		if inputType == "" {
			inputType = apimeta.ScalarFlagType(m)
		}
	}
	switch inputType {
	case "bool":
		return "common.default_bool"
	case "int", "int64", "integer", "uint", "uint64":
		return "common.default_int"
	case "string_array":
		return "common.default_string_array"
	case "json":
		return "common.default_json"
	default:
		return "common.default_string"
	}
}

func subgroupShort(group, sub string) string {
	if group == "instance" && sub == "snapshot" {
		return "Manage sandbox snapshots (Beta)"
	}
	if group == "image" && sub == "precache" {
		return "Manage image pre-cache tasks"
	}
	if group == "session" && sub == "event" {
		return "Manage session events"
	}
	if group == "session" && sub == "space" {
		return "Manage session spaces"
	}
	if group == "harness" && sub == "revision" {
		return "Manage immutable harness revisions and publishing"
	}
	if group == "harness" && sub == "engine" {
		return "Discover available Harness Engines and versions"
	}
	if group == "model-provider" && sub == "revision" {
		return "Inspect immutable model provider revisions"
	}
	return "Manage " + sub
}

func subgroupLong(group, sub string) string {
	if group == "instance" && sub == "snapshot" {
		return "Create, list, and delete sandbox snapshots. The Runtime Snapshot API is Beta and may change incompatibly or be discontinued. Use --memory=false for disk-only snapshots. Restore an ACTIVE saved snapshot into a new instance with instance create --snapshot-id. To clone an existing instance instead, use instance create --source-instance-id with an instance ID."
	}
	if group == "harness" && sub == "revision" {
		return "A revision is an immutable snapshot of the harness draft. Creating a revision pins the latest revision of every referenced model provider. Status: UNPUBLISHED, PUBLISHING, PUBLISHED (at most one per harness), PUBLISH_FAILED (retry the same number), SUPERSEDED (republish to roll back)."
	}
	if group != "session" {
		return ""
	}
	switch sub {
	case "space":
		return "Manage spaces that isolate users, sessions, events, and state by business purpose. Use the server-returned SpaceId with session and event commands. Only spaces with no sessions, events, or user state can be deleted; default spaces are protected."
	case "event":
		return "Append interactions and read a session's event history. Identify the session with --space-id, --user-id, and --session-id. List results are paginated; a session snapshot is not the complete event history."
	}
	return ""
}

func groupExamples(group string) []string {
	switch group {
	case "instance.snapshot":
		return []string{"Example - Create and inspect a disk-only snapshot:\n  agr instance snapshot create --instance-id ins-xxxx --memory=false -o text\n  agr instance snapshot list -o text", "Example - Restore an ACTIVE snapshot into a new instance:\n  agr instance create --snapshot-id snapshot-xxxx -o text", "Example - Request snapshot deletion:\n  agr instance snapshot delete snapshot-xxxx -o text"}
	case "session":
		return []string{
			"Example - Choose or create a business space:\n  agr session space list -o text\n  agr session space create --name customer-support -o text",
			"Example - Create a conversation using the returned SpaceId:\n  agr session create --space-id space-a1b2c3d4 --user-id user-123 --session-id session-123 --title 'Support request' -o text",
			"Example - Append an event and read the conversation:\n  agr session event append --space-id space-a1b2c3d4 --user-id user-123 --session-id session-123 --event @event.json -o text\n  agr session get --space-id space-a1b2c3d4 --user-id user-123 --session-id session-123 -o text\n  agr session event list --space-id space-a1b2c3d4 --user-id user-123 --session-id session-123 --limit 50 -o text",
		}
	case "session.space":
		return []string{"Example - Choose a space and inspect its scope:\n  agr session space list -o text\n  agr session space get --space-id space-a1b2c3d4 -o text", "Example - Create a business space:\n  agr session space create --name customer-support --description 'Customer support sessions' -o text"}
	case "harness":
		return []string{
			"Example - Create a harness and wait for revision 1 to publish:\n  agr harness engine list -o text\n  agr harness create --name my-agent --engine pi --engine-version 1.3.0 \\\n    --model mp-a1b2c3d4:hy3 --role-arn qcs::cam::uin/100000000001:roleName/AgentRole \\\n    --subnet-id subnet-a1b2c3d4 --security-group-id sg-a1b2c3d4 --wait -o text",
			"Example - Change the draft, then publish it:\n  agr harness update hns-a1b2c3d4 --system-prompt @prompt.md -o text\n  agr harness revision diff hns-a1b2c3d4 live draft -o text\n  agr harness revision create hns-a1b2c3d4 --publish --wait -o text",
			"Example - Check what is live and connect:\n  agr harness status hns-a1b2c3d4 -o text\n  agr harness prompt hns-a1b2c3d4 'Say hello' -o text",
			"Example - Add an official MCP server from the tool market:\n  agr harness tool list --type mcp -o text\n  agr harness tool get rec-a1b2c3d4 -o text\n  agr harness update hns-a1b2c3d4 --mcp deepwiki=MARKETPLACE/rec-a1b2c3d4/rv-a1b2c3d4 -o text",
		}
	case "session.event":
		return []string{"Example - Append a prepared event:\n  agr session event append --space-id space-a1b2c3d4 --user-id user-123 --session-id session-123 --event @event.json -o text", "Example - Read the next page of history:\n  agr session event list --space-id space-a1b2c3d4 --user-id user-123 --session-id session-123 --offset 50 --limit 50 -o text"}
	}
	return nil
}

func aliasesForGroup(group string) []string {
	switch group {
	case "instance":
		return []string{"i"}
	case "tool":
		return []string{"t"}
	case "apikey":
		return []string{"ak", "key"}
	case "pre-cache-image-task":
		return []string{"pre_cache_image_task"}
	case "model-provider":
		return []string{"mp"}
	}
	return nil
}

func aliasesForCommand(commandID string) []string {
	switch commandID {
	case "instance.create":
		return []string{"c"}
	case "instance.list", "instance.snapshot.list", "tool.list", "apikey.list", "session.list", "session.space.list", "session.event.list", "volume.list", "volume-template.list",
		"harness.list", "harness.engine.list", "harness.revision.list", "model-provider.list", "model-provider.revision.list":
		return []string{"ls"}
	case "instance.delete", "instance.snapshot.delete", "tool.delete", "apikey.delete", "session.delete", "session.space.delete", "volume.delete", "volume-template.delete",
		"harness.delete", "model-provider.delete":
		return []string{"rm", "del"}
	}
	return nil
}

func argsForCommand(commandID string) string {
	switch commandID {
	case "instance.update", "instance.pause", "instance.resume", "tool.update":
		return "cli.ArgsExactUnlessSkeleton(1)"
	case "apikey.delete", "pre-cache-image-task.get", "deployment.get", "deployment.update", "deployment.delete":
		return "cli.ArgsExactUnlessSkeleton(1)"
	case "instance.delete", "tool.delete":
		return "func(cmd *cobra.Command, args []string) error { if cli.GenerateSkeletonEnabled() { return nil }; return cobra.MinimumNArgs(1)(cmd, args) }"
	default:
		return "cobra.NoArgs"
	}
}

func useForCommand(commandID string) string {
	switch commandID {
	case "instance.snapshot.delete":
		return "delete [snapshot-id]"
	case "instance.update":
		return "update <instance-id>"
	case "instance.pause":
		return "pause <instance-id>"
	case "instance.resume":
		return "resume <instance-id>"
	case "instance.delete":
		return "delete <instance-id> [instance-id...]"
	case "tool.update":
		return "update <tool-id>"
	case "tool.delete":
		return "delete <tool-id> [tool-id...]"
	case "apikey.delete":
		return "delete <key-id>"
	case "pre-cache-image-task.create":
		return "create --image <image> --image-registry-type <type>"
	case "pre-cache-image-task.get":
		return "get [image-digest] [flags]"
	case "deployment.get":
		return "get <deployment-id>"
	case "deployment.update":
		return "update <deployment-id>"
	case "deployment.delete":
		return "delete <deployment-id>"
	case "harness.get", "harness.update", "harness.delete":
		return strings.TrimPrefix(commandID, "harness.") + " <harness-id>"
	case "harness.revision.create", "harness.revision.list":
		return strings.TrimPrefix(commandID, "harness.revision.") + " <harness-id>"
	case "harness.revision.get", "harness.revision.publish":
		return strings.TrimPrefix(commandID, "harness.revision.") + " <harness-id> <revision>"
	case "model-provider.get", "model-provider.update", "model-provider.delete":
		return strings.TrimPrefix(commandID, "model-provider.") + " <model-provider-id>"
	case "model-provider.revision.list":
		return "list <model-provider-id>"
	case "model-provider.revision.get":
		return "get <model-provider-id> <revision>"
	}
	parts := strings.Split(commandID, ".")
	return parts[len(parts)-1]
}

func hookForCommand(commandID string) string {
	switch commandID {
	case "instance.create":
		return "instanceCreateFn"
	case "instance.list":
		return "instanceListFn"
	case "instance.update":
		return "instanceUpdateFn"
	case "instance.pause":
		return "instancePauseFn"
	case "instance.resume":
		return "instanceResumeFn"
	case "instance.delete":
		return "instanceDeleteFn"
	case "tool.create":
		return "toolCreateFn"
	case "tool.list":
		return "toolListFn"
	case "tool.update":
		return "toolUpdateFn"
	case "tool.delete":
		return "toolDeleteFn"
	case "apikey.create":
		return "apikeyCreateFn"
	case "apikey.list":
		return "apikeyListFn"
	case "apikey.delete":
		return "apikeyDeleteFn"
	case "pre-cache-image-task.create":
		return "imagePrecacheCreateFn"
	case "pre-cache-image-task.get":
		return "imagePrecacheGetFn"
	}
	return "cli.UnsupportedGeneratedCommand(" + quote(commandID) + ")"
}

func goTypeForInput(t string) string {
	switch t {
	case "string_array":
		return "[]string"
	case "bool":
		return "bool"
	case "int", "int64", "integer":
		return "int"
	default:
		return "string"
	}
}

func cobraTypeForInput(t string) string {
	switch t {
	case "string_array":
		return "stringArray"
	case "bool":
		return "bool"
	case "int", "int64", "integer":
		return "int"
	default:
		return "string"
	}
}

func packageForCommand(parts []string) string {
	return strings.ReplaceAll(parts[len(parts)-1], "-", "")
}

func dirForCommand(parts []string) string {
	cleaned := make([]string, len(parts)-1)
	for i, p := range parts[:len(parts)-1] {
		cleaned[i] = strings.ReplaceAll(p, "-", "")
	}
	return strings.Join(cleaned, "/")
}

func exported(s string) string {
	parts := regexp.MustCompile(`[^A-Za-z0-9]+`).Split(s, -1)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	return b.String()
}

func boolDefault(s string) string {
	if s == "true" {
		return "true"
	}
	return "false"
}

func intDefault(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func strip(s string) string {
	r := strings.NewReplacer("<p>", "", "</p>", "", "<ul>", "", "</ul>", "", "<li>", "", "</li>", "")
	return r.Replace(s)
}

func quote(s string) string { return strconv.Quote(s) }
