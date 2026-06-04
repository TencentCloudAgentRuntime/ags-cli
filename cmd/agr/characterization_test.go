package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

func TestCharacterization_PublicCommandSurface(t *testing.T) {
	root := contractRoot()
	cases := []struct {
		command string
		aliases []string
		use     string
		flags   map[string]flagExpectation
	}{
		{command: "instance", aliases: []string{"i"}, use: "instance"},
		{command: "instance.create", aliases: []string{"c"}, use: "create", flags: map[string]flagExpectation{
			"tool-name":            {typ: "string", shorthand: "t"},
			"tool-id":              {typ: "string"},
			"timeout":              {typ: "string"},
			"mount-options":        {typ: "string"},
			"custom-configuration": {typ: "string"},
			"metadata":             {typ: "string"},
			"auth-mode":            {typ: "string"},
			"client-token":         {typ: "string"},
			"request":              {typ: "string"},
		}},
		{command: "instance.list", aliases: []string{"ls"}, use: "list", flags: map[string]flagExpectation{
			"tool-id":      {typ: "string"},
			"instance-ids": {typ: "stringArray"},
			"filters":      {typ: "string"},
			"offset":       {typ: "int", def: "0"},
			"limit":        {typ: "int"},
			"request":      {typ: "string"},
		}},
		{command: "instance.update", use: "update <instance-id>", flags: map[string]flagExpectation{
			"timeout":  {typ: "string"},
			"metadata": {typ: "string"},
			"request":  {typ: "string"},
		}},
		{command: "instance.debug", use: "debug", flags: map[string]flagExpectation{
			"tool-id":              {typ: "string"},
			"tool-name":            {typ: "string", shorthand: "t"},
			"timeout":              {typ: "string", def: "1h"},
			"auth-mode":            {typ: "string"},
			"mount-options":        {typ: "string"},
			"custom-configuration": {typ: "string"},
			"metadata":             {typ: "string"},
			"client-token":         {typ: "string"},
		}},
		{command: "instance.delete", aliases: []string{"rm", "del"}, use: "delete <instance-id> [instance-id...]", flags: map[string]flagExpectation{
			"ignore-not-found": {typ: "bool", def: "false"},
			"request":          {typ: "string"},
		}},
		{command: "instance.code.run", use: "run [instance-id]", flags: map[string]flagExpectation{
			"code":                 {typ: "string", shorthand: "c"},
			"file":                 {typ: "stringArray", shorthand: "f"},
			"language":             {typ: "string", shorthand: "l", def: "python"},
			"stream":               {typ: "bool", shorthand: "s", def: "false"},
			"create-temp-instance": {typ: "bool", def: "false"},
			"tool-name":            {typ: "string", shorthand: "t"},
			"tool-id":              {typ: "string"},
		}},
		{command: "instance.file.upload", use: "upload <instance-id> <local-path|-> <remote-path>"},
		{command: "instance.file.download", use: "download <instance-id> <remote-path> <local-path|->"},
		{command: "instance.mobile.adb", use: "adb <instance-id> -- <adb-args...>"},
		{command: "tool", aliases: []string{"t"}, use: "tool"},
		{command: "tool.create", use: "create", flags: map[string]flagExpectation{
			"tool-name":             {typ: "string", shorthand: "n"},
			"tool-type":             {typ: "string", shorthand: "t"},
			"description":           {typ: "string", shorthand: "d"},
			"default-timeout":       {typ: "string"},
			"network-configuration": {typ: "string"},
			"tags":                  {typ: "string"},
			"role-arn":              {typ: "string"},
			"storage-mounts":        {typ: "string"},
			"custom-configuration":  {typ: "string"},
			"log-configuration":     {typ: "string"},
			"persistent":            {typ: "bool", def: "false"},
			"client-token":          {typ: "string"},
			"request":               {typ: "string"},
		}},
		{command: "tool.list", aliases: []string{"ls"}, use: "list", flags: map[string]flagExpectation{
			"tool-ids": {typ: "stringArray"},
			"filters":  {typ: "string"},
			"offset":   {typ: "int", def: "0"},
			"limit":    {typ: "int"},
			"request":  {typ: "string"},
		}},
		{command: "tool.update", use: "update <tool-id>", flags: map[string]flagExpectation{
			"description":           {typ: "string", shorthand: "d"},
			"network-configuration": {typ: "string"},
			"tags":                  {typ: "string"},
			"custom-configuration":  {typ: "string"},
			"request":               {typ: "string"},
		}},
		{command: "tool.delete", aliases: []string{"rm", "del"}, use: "delete <tool-id> [tool-id...]", flags: map[string]flagExpectation{
			"request": {typ: "string"},
		}},
		{command: "apikey", aliases: []string{"ak", "key"}, use: "apikey"},
		{command: "apikey.create", use: "create", flags: map[string]flagExpectation{
			"name":    {typ: "string", shorthand: "n"},
			"request": {typ: "string"},
		}},
		{command: "apikey.list", aliases: []string{"ls"}, use: "list"},
		{command: "apikey.delete", aliases: []string{"rm", "del"}, use: "delete <key-id>", flags: map[string]flagExpectation{
			"request": {typ: "string"},
		}},
		{command: "pre-cache-image-task.create", use: "create --image <image> --image-registry-type <type>", flags: map[string]flagExpectation{
			"image":               {typ: "string"},
			"image-registry-type": {typ: "string"},
			"request":             {typ: "string"},
		}},
		{command: "pre-cache-image-task.get", use: "get <image-digest> --image <image> --image-registry-type <type>", flags: map[string]flagExpectation{
			"image":               {typ: "string"},
			"image-registry-type": {typ: "string"},
			"request":             {typ: "string"},
		}},
		{command: "api.call", use: "call <Action>", flags: map[string]flagExpectation{
			"request": {typ: "string"},
		}},
		{command: "config.path", use: "path"},
		{command: "config.show", use: "show"},
		{command: "config.set", use: "set <key> <value>"},
	}

	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			cmd, ok := findCobraCommand(root, tc.command)
			if !ok {
				t.Fatalf("command %q not found", tc.command)
			}
			if got := cmd.Use; got != tc.use {
				t.Fatalf("Use = %q, want %q", got, tc.use)
			}
			for _, alias := range tc.aliases {
				if !containsString(cmd.Aliases, alias) {
					t.Fatalf("aliases %v missing %q", cmd.Aliases, alias)
				}
			}
			for name, want := range tc.flags {
				flag := cmd.Flags().Lookup(name)
				if flag == nil {
					t.Fatalf("missing flag --%s", name)
				}
				if got := flag.Value.Type(); got != want.typ {
					t.Fatalf("--%s type = %q, want %q", name, got, want.typ)
				}
				if got := flag.Shorthand; got != want.shorthand {
					t.Fatalf("--%s shorthand = %q, want %q", name, got, want.shorthand)
				}
				if want.def != "" && flag.DefValue != want.def {
					t.Fatalf("--%s default = %q, want %q", name, flag.DefValue, want.def)
				}
			}
		})
	}
}

func TestCharacterization_HelpAndSchemaExcerpts(t *testing.T) {
	root := contractRoot()
	helpCases := []struct {
		command  string
		contains []string
	}{
		{
			command: "instance.create",
			contains: []string{
				"Create a new sandbox instance",
				"agr instance create -t my-tool --timeout 5m",
				"--timeout string",
				"--mount-options string",
				"--metadata string",
			},
		},
		{
			command: "tool.create",
			contains: []string{
				"Create a new sandbox tool",
				"--network-configuration string",
				"Format:",
				"NetworkMode",
				"--tags string",
				"--storage-mounts string",
				"agr tool create -n my-tool -t custom --network-configuration",
				"--persistent",
			},
		},
		{
			command: "instance.list",
			contains: []string{
				"List sandbox instances with optional filters.",
				"--filters string",
				"Format:",
				`[{"Name":"<field>","Values":["<value1>","<value2>"]}]`,
				"Status: STARTING, RUNNING, STOPPING, STOPPED, STOP_FAILED, FAILED",
			},
		},
		{
			command: "tool.update",
			contains: []string{
				"Update a sandbox tool",
				"--network-configuration string",
				"--tags string",
			},
		},
		{
			command: "instance.debug",
			contains: []string{
				"Create a temporary debug tool from an existing tool",
				"--tool-id string",
				"--auth-mode string",
				"--timeout string",
				"--client-token string",
			},
		},
		{
			command: "pre-cache-image-task.create",
			contains: []string{
				"Create an image pre-cache task",
				"--image string",
				"--image-registry-type string",
				"Image registry type: enterprise or personal (required)",
			},
		},
		{
			command: "pre-cache-image-task.get",
			contains: []string{
				"Describe an image pre-cache task",
				"--image string",
				"--image-registry-type string",
				"Image registry type: enterprise or personal (required)",
			},
		},
	}
	for _, tc := range helpCases {
		t.Run(tc.command, func(t *testing.T) {
			cmd, ok := findCobraCommand(root, tc.command)
			if !ok {
				t.Fatalf("command %q not found", tc.command)
			}
			help := commandHelp(t, cmd)
			for _, want := range tc.contains {
				if !strings.Contains(help, want) {
					t.Fatalf("help for %s missing %q\n%s", tc.command, want, help)
				}
			}
			if strings.Contains(help, "Run "+tc.command) {
				t.Fatalf("help for %s still uses generated placeholder short description\n%s", tc.command, help)
			}
		})
	}

	schemaCases := []struct {
		command  string
		want     map[string]schemaFlagExpectation
		wantArgs []string
	}{
		{
			command: "instance.create",
			want: map[string]schemaFlagExpectation{
				"timeout":       {typ: "string"},
				"mount-options": {typ: "json"},
				"metadata":      {typ: "json"},
				"request":       {typ: "string"},
			},
		},
		{
			command: "tool.create",
			want: map[string]schemaFlagExpectation{
				"default-timeout":       {typ: "string"},
				"network-configuration": {typ: "json"},
				"tags":                  {typ: "json"},
				"storage-mounts":        {typ: "json"},
			},
		},
		{
			command: "tool.update",
			want: map[string]schemaFlagExpectation{
				"network-configuration": {typ: "json"},
				"tags":                  {typ: "json"},
			},
		},
		{
			command: "pre-cache-image-task.create",
			want: map[string]schemaFlagExpectation{
				"request":             {typ: "string"},
				"generate-skeleton":   {typ: "bool"},
				"image":               {typ: "string"},
				"image-registry-type": {typ: "string"},
			},
		},
		{
			command: "pre-cache-image-task.get",
			want: map[string]schemaFlagExpectation{
				"request":             {typ: "string"},
				"generate-skeleton":   {typ: "bool"},
				"image":               {typ: "string"},
				"image-registry-type": {typ: "string"},
			},
		},
		{
			command: "instance.list",
			want: map[string]schemaFlagExpectation{
				"request":           {typ: "string"},
				"generate-skeleton": {typ: "bool"},
			},
		},
		{
			command: "instance.pause",
			want: map[string]schemaFlagExpectation{
				"request":           {typ: "string"},
				"generate-skeleton": {typ: "bool"},
			},
		},
		{
			command: "instance.resume",
			want: map[string]schemaFlagExpectation{
				"request":           {typ: "string"},
				"generate-skeleton": {typ: "bool"},
			},
		},
		{
			command: "instance.delete",
			want: map[string]schemaFlagExpectation{
				"request":           {typ: "string"},
				"generate-skeleton": {typ: "bool"},
			},
		},
		{
			command: "tool.list",
			want: map[string]schemaFlagExpectation{
				"request":           {typ: "string"},
				"generate-skeleton": {typ: "bool"},
			},
		},
		{
			command: "tool.delete",
			want: map[string]schemaFlagExpectation{
				"request":           {typ: "string"},
				"generate-skeleton": {typ: "bool"},
			},
		},
		{
			command: "apikey.create",
			want: map[string]schemaFlagExpectation{
				"request":           {typ: "string"},
				"generate-skeleton": {typ: "bool"},
			},
		},
		{
			command: "apikey.delete",
			want: map[string]schemaFlagExpectation{
				"request":           {typ: "string"},
				"generate-skeleton": {typ: "bool"},
			},
		},
		{
			command: "instance.exec",
			want: map[string]schemaFlagExpectation{
				"stream": {typ: "bool"},
			},
		},
		{
			command: "instance.debug",
			want: map[string]schemaFlagExpectation{
				"tool-id":              {typ: "string"},
				"tool-name":            {typ: "string"},
				"timeout":              {typ: "string"},
				"auth-mode":            {typ: "string"},
				"mount-options":        {typ: "string"},
				"custom-configuration": {typ: "string"},
				"metadata":             {typ: "string"},
				"client-token":         {typ: "string"},
			},
		},
		{
			command: "api.call",
			want: map[string]schemaFlagExpectation{
				"request": {typ: "string"},
			},
			wantArgs: []string{"Action"},
		},
		{
			command: "config.set",
			want:    map[string]schemaFlagExpectation{},
			wantArgs: []string{
				"Key",
				"Value",
			},
		},
		{
			command:  "completion",
			want:     map[string]schemaFlagExpectation{},
			wantArgs: []string{"Shell"},
		},
		{
			command:  "schema",
			want:     map[string]schemaFlagExpectation{},
			wantArgs: []string{"CommandName"},
		},
	}
	for _, tc := range schemaCases {
		t.Run("schema "+tc.command, func(t *testing.T) {
			schema := schemaForCommand(t, tc.command)
			if tc.command == "instance.create" {
				requirePropertyType(t, schema, "AuthMode", "enum")
				if !containsString(schema.Examples, "agr instance create --tool-id sdt-xxxx") {
					t.Fatalf("schema %s examples %v missing tool-id example", tc.command, schema.Examples)
				}
				flag := schema.Flags["mount-options"]
				if flag.Type != "json" || len(flag.Examples) == 0 {
					t.Fatalf("schema %s mount-options metadata incomplete: %#v", tc.command, flag)
				}
			}
			if tc.command == "pre-cache-image-task.create" {
				if !schema.RequiresAuth {
					t.Fatalf("schema %s RequiresAuth = false, want true", tc.command)
				}
				if !schema.SupportsRequest {
					t.Fatalf("schema %s SupportsRequest = false, want true", tc.command)
				}
				requireCliFlag(t, schema, "Image", "image")
				requireCliFlag(t, schema, "ImageRegistryType", "image-registry-type")
			}
			if tc.command == "pre-cache-image-task.get" {
				requireFlagAbsent(t, schema, "image-digest")
			}
			if tc.command == "tool.create" {
				requireFailureCode(t, schema, "INVALID_JSON_FLAG")
			}
			if tc.command == "instance.exec" {
				requireIncompatibleWith(t, schema, "stream", "output=json")
				requireAllowsOutput(t, schema, "stream", "text", "ndjson")
			}
			if len(tc.wantArgs) > 0 {
				requireArgNames(t, schema, tc.wantArgs...)
			}
			for name, want := range tc.want {
				got, ok := schema.Flags[name]
				if !ok {
					t.Fatalf("schema for %s missing flag %q", tc.command, name)
				}
				if got.Type != want.typ {
					t.Fatalf("schema %s flag %s type = %q, want %q", tc.command, name, got.Type, want.typ)
				}
				for _, value := range want.values {
					if !containsString(got.Values, value) {
						t.Fatalf("schema %s flag %s values %v missing %q", tc.command, name, got.Values, value)
					}
				}
			}
		})
	}
}

func TestCharacterization_SchemaListsEveryPublicLeafCommand(t *testing.T) {
	root := contractRoot()
	output, err := runAGR(t, "schema", "-o", "json")
	if err != nil {
		t.Fatalf("schema list failed: %v\n%s", err, output)
	}
	var env struct {
		Data struct {
			Commands []struct {
				Name string `json:"Name"`
			} `json:"Commands"`
		} `json:"Data"`
	}
	jsonStart := strings.Index(output, "{")
	if jsonStart < 0 {
		t.Fatalf("schema output did not contain JSON\n%s", output)
	}
	if err := json.Unmarshal([]byte(output[jsonStart:]), &env); err != nil {
		t.Fatalf("decode schema JSON: %v\n%s", err, output)
	}
	got := map[string]bool{}
	for _, command := range env.Data.Commands {
		got[command.Name] = true
	}
	for _, want := range leafCommandIDs(root) {
		if !got[want] {
			t.Fatalf("schema list is missing public leaf command %q", want)
		}
	}
}

func TestCharacterization_RequestModeAllowsRequestOnlyForCommandsWithRequiredPositionals(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "apikey.delete", args: []string{"apikey", "delete", "--request", `{"KeyId":"ak-test"}`, "-o", "json"}},
		{name: "instance.delete", args: []string{"instance", "delete", "--request", `{"InstanceId":"ins-test"}`, "-o", "json"}},
		{name: "instance.pause", args: []string{"instance", "pause", "--request", `{"InstanceId":"ins-test"}`, "-o", "json"}},
		{name: "instance.resume", args: []string{"instance", "resume", "--request", `{"InstanceId":"ins-test"}`, "-o", "json"}},
		{name: "instance.update", args: []string{"instance", "update", "--request", `{"InstanceId":"ins-test","Timeout":"5m"}`, "-o", "json"}},
		{name: "pre-cache-image-task.get", args: []string{"pre-cache-image-task", "get", "--request", `{"ImageDigest":"sha256:test"}`, "-o", "json"}},
		{name: "tool.delete", args: []string{"tool", "delete", "--request", `{"ToolId":"sdt-test"}`, "-o", "json"}},
		{name: "tool.update", args: []string{"tool", "update", "--request", `{"ToolId":"sdt-test","Description":"demo"}`, "-o", "json"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, _ := runAGR(t, tc.args...)
			if strings.Contains(output, "requires at least 1 arg(s), got 0") {
				t.Fatalf("%s still rejected request-only mode\n%s", tc.name, output)
			}
			if !strings.Contains(output, `"Command":"`) {
				t.Fatalf("%s did not execute through the JSON envelope path\n%s", tc.name, output)
			}
		})
	}
}

func TestCharacterization_ConfigCommandsWorkWithBrokenConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "broken.toml")
	if err := os.WriteFile(cfgPath, []byte("not = [valid\n"), 0o600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}

	output, err := runAGR(t, "--config", cfgPath, "config", "path", "-o", "json")
	if err != nil {
		t.Fatalf("config path failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, `"Status":"succeeded"`) || !strings.Contains(output, cfgPath) {
		t.Fatalf("config path output did not expose config path\n%s", output)
	}

	output, err = runAGR(t, "--config", cfgPath, "config", "set", "output", "json", "-o", "json")
	if err != nil {
		t.Fatalf("config set failed: %v\n%s", err, output)
	}
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read rewritten config: %v", err)
	}
	if !strings.Contains(string(content), "output = 'json'") {
		t.Fatalf("config set did not rewrite broken config\n%s", string(content))
	}
}

func TestCharacterization_DoctorContinuesWhenConfiguredOutputIsInvalid(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("output = 'ndjson'\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	output, err := runAGR(t, "--config", cfgPath, "doctor")
	if err == nil {
		t.Fatalf("doctor unexpectedly succeeded\n%s", output)
	}
	if !strings.Contains(output, "error output: ndjson is only supported for streaming when passed explicitly with -o ndjson") {
		t.Fatalf("doctor output missing invalid-output check\n%s", output)
	}
	for _, want := range []string{"SecretId", "SecretKey", "TokenCache", "Connectivity"} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor stopped before rendering %s\n%s", want, output)
		}
	}
}

func TestCharacterization_ConfigSetRejectsInvalidValue(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatalf("create config file: %v", err)
	}

	output, err := runAGR(t, "--config", cfgPath, "config", "set", "output", "banana", "-o", "json")
	if err == nil {
		t.Fatalf("config set unexpectedly succeeded\n%s", output)
	}
	if !strings.Contains(output, `"Code":"INVALID_CONFIG"`) {
		t.Fatalf("config set did not report INVALID_CONFIG\n%s", output)
	}
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if strings.TrimSpace(string(content)) != "" {
		t.Fatalf("config set wrote invalid value to disk\n%s", string(content))
	}
}

type flagExpectation struct {
	typ       string
	shorthand string
	def       string
}

type schemaFlagExpectation struct {
	typ    string
	values []string
}

type commandSchemaSnapshot struct {
	Name            string
	Kind            string
	ResolvedFrom    string
	Aliases         []string
	Subcommands     []string
	SupportsJSON    bool
	RequiresAuth    bool
	SupportsRequest bool
	Args            []struct {
		Name     string
		Required bool
	}
	RequestSchema map[string]struct {
		Type    string
		CliFlag *string
	}
	Failures []string
	Flags    map[string]struct {
		Type             string
		Format           string
		Examples         []string
		Values           []string
		IncompatibleWith []string
		AllowsOutput     []string
	}
	Examples []string
}

func commandHelp(t *testing.T, cmd *cobra.Command) string {
	t.Helper()
	var buf bytes.Buffer
	oldOut, oldErr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	defer func() {
		cmd.SetOut(oldOut)
		cmd.SetErr(oldErr)
	}()
	if err := cmd.Help(); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	return buf.String()
}

func TestCharacterization_LeafHelpIncludesGroupedExamples(t *testing.T) {
	root := contractRoot()
	for _, commandID := range leafCommandIDs(root) {
		cmd, ok := findCobraCommand(root, commandID)
		if !ok {
			t.Fatalf("command %s not found", commandID)
		}
		help := commandHelp(t, cmd)
		if !strings.Contains(help, "Example - ") {
			t.Fatalf("help for %s missing grouped examples:\n%s", commandID, help)
		}
	}
}

func schemaForCommand(t *testing.T, command string) commandSchemaSnapshot {
	t.Helper()
	snapshot := schemaSnapshotForCommand(t, command)
	if snapshot.Name != command {
		t.Fatalf("schema name = %q, want %q", snapshot.Name, command)
	}
	return snapshot
}

func schemaSnapshotForCommand(t *testing.T, command string) commandSchemaSnapshot {
	t.Helper()
	output, err := runAGR(t, "schema", command, "-o", "json")
	if err != nil {
		t.Fatalf("schema command failed: %v\n%s", err, output)
	}
	var env struct {
		Data struct {
			Name            string   `json:"Name"`
			Kind            string   `json:"Kind"`
			ResolvedFrom    string   `json:"ResolvedFrom"`
			Aliases         []string `json:"Aliases"`
			Subcommands     []string `json:"Subcommands"`
			SupportsJson    bool     `json:"SupportsJson"`
			RequiresAuth    bool     `json:"RequiresAuth"`
			SupportsRequest bool     `json:"SupportsRequest"`
			Args            []struct {
				Name     string `json:"Name"`
				Required bool   `json:"Required"`
			} `json:"Args"`
			RequestSchema struct {
				Properties map[string]struct {
					Type    string  `json:"Type"`
					CliFlag *string `json:"CliFlag"`
				} `json:"Properties"`
			} `json:"RequestSchema"`
			Failures []string `json:"Failures"`
			Flags    []struct {
				Name             string   `json:"Name"`
				Type             string   `json:"Type"`
				Format           string   `json:"Format"`
				Examples         []string `json:"Examples"`
				Values           []string `json:"Values"`
				IncompatibleWith []string `json:"IncompatibleWith"`
				AllowsOutput     []string `json:"AllowsOutput"`
			} `json:"Flags"`
			Examples []string `json:"Examples"`
		} `json:"Data"`
	}
	jsonStart := strings.Index(output, "{")
	if jsonStart < 0 {
		t.Fatalf("schema output did not contain JSON\n%s", output)
	}
	if err := json.Unmarshal([]byte(output[jsonStart:]), &env); err != nil {
		t.Fatalf("decode schema JSON: %v\n%s", err, output)
	}
	snapshot := commandSchemaSnapshot{
		Name:            env.Data.Name,
		Kind:            env.Data.Kind,
		ResolvedFrom:    env.Data.ResolvedFrom,
		Aliases:         append([]string(nil), env.Data.Aliases...),
		Subcommands:     append([]string(nil), env.Data.Subcommands...),
		SupportsJSON:    env.Data.SupportsJson,
		RequiresAuth:    env.Data.RequiresAuth,
		SupportsRequest: env.Data.SupportsRequest,
		Args: make([]struct {
			Name     string
			Required bool
		}, 0, len(env.Data.Args)),
		RequestSchema: map[string]struct {
			Type    string
			CliFlag *string
		}{},
		Failures: env.Data.Failures,
		Examples: append([]string(nil), env.Data.Examples...),
		Flags: map[string]struct {
			Type             string
			Format           string
			Examples         []string
			Values           []string
			IncompatibleWith []string
			AllowsOutput     []string
		}{},
	}
	for name, prop := range env.Data.RequestSchema.Properties {
		snapshot.RequestSchema[name] = struct {
			Type    string
			CliFlag *string
		}{Type: prop.Type, CliFlag: prop.CliFlag}
	}
	for _, arg := range env.Data.Args {
		snapshot.Args = append(snapshot.Args, struct {
			Name     string
			Required bool
		}{Name: arg.Name, Required: arg.Required})
	}
	for _, flag := range env.Data.Flags {
		snapshot.Flags[flag.Name] = struct {
			Type             string
			Format           string
			Examples         []string
			Values           []string
			IncompatibleWith []string
			AllowsOutput     []string
		}{
			Type:             flag.Type,
			Format:           flag.Format,
			Examples:         append([]string(nil), flag.Examples...),
			Values:           flag.Values,
			IncompatibleWith: flag.IncompatibleWith,
			AllowsOutput:     flag.AllowsOutput,
		}
	}
	return snapshot
}

func TestCharacterization_SchemaAliasesResolveToCanonicalCommands(t *testing.T) {
	cases := []struct {
		query          string
		wantName       string
		wantArgNames   []string
		wantAlias      string
		wantRequest    bool
		wantSubcommand string
	}{
		{query: "tool.rm", wantName: "tool.delete", wantArgNames: []string{"ToolId"}, wantRequest: true, wantAlias: "rm"},
		{query: "i.ls", wantName: "instance.list", wantRequest: true, wantAlias: "ls"},
		{query: "i", wantName: "instance", wantAlias: "i", wantSubcommand: "instance.list"},
		{query: "key", wantName: "apikey", wantAlias: "key", wantSubcommand: "apikey.delete"},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			schema := schemaSnapshotForCommand(t, tc.query)
			if schema.Name != tc.wantName {
				t.Fatalf("schema name = %q, want %q", schema.Name, tc.wantName)
			}
			if schema.ResolvedFrom != tc.query {
				t.Fatalf("schema resolvedFrom = %q, want %q", schema.ResolvedFrom, tc.query)
			}
			if tc.wantRequest && !schema.SupportsRequest {
				t.Fatalf("schema %s SupportsRequest = false, want true", tc.wantName)
			}
			if tc.wantAlias != "" && !containsString(schema.Aliases, tc.wantAlias) {
				t.Fatalf("schema aliases %v missing %q", schema.Aliases, tc.wantAlias)
			}
			if tc.wantSubcommand != "" && !containsString(schema.Subcommands, tc.wantSubcommand) {
				t.Fatalf("schema subcommands %v missing %q", schema.Subcommands, tc.wantSubcommand)
			}
			if len(tc.wantArgNames) > 0 {
				requireArgNames(t, schema, tc.wantArgNames...)
			}
		})
	}
}

func TestCharacterization_SchemaListIncludesPublicGroupsHelpAndAliases(t *testing.T) {
	helpSchema := schemaForCommand(t, "help")
	requireArgNames(t, helpSchema, "Command")
	if helpSchema.Kind != "command" {
		t.Fatalf("help kind = %q, want %q", helpSchema.Kind, "command")
	}

	output, err := runAGR(t, "schema", "-o", "json")
	if err != nil {
		t.Fatalf("schema list failed: %v\n%s", err, output)
	}
	var env struct {
		Data struct {
			Commands []struct {
				Name        string   `json:"Name"`
				Kind        string   `json:"Kind"`
				Aliases     []string `json:"Aliases"`
				Subcommands []string `json:"Subcommands"`
			} `json:"Commands"`
		} `json:"Data"`
	}
	jsonStart := strings.Index(output, "{")
	if jsonStart < 0 {
		t.Fatalf("schema output did not contain JSON\n%s", output)
	}
	if err := json.Unmarshal([]byte(output[jsonStart:]), &env); err != nil {
		t.Fatalf("decode schema JSON: %v\n%s", err, output)
	}

	commands := map[string]struct {
		Kind        string
		Aliases     []string
		Subcommands []string
	}{}
	for _, command := range env.Data.Commands {
		commands[command.Name] = struct {
			Kind        string
			Aliases     []string
			Subcommands []string
		}{Kind: command.Kind, Aliases: command.Aliases, Subcommands: command.Subcommands}
	}

	for _, want := range []string{"help", "instance", "tool", "api", "config", "apikey", "pre-cache-image-task"} {
		if _, ok := commands[want]; !ok {
			t.Fatalf("schema list missing public command/group %q", want)
		}
	}
	if !containsString(commands["instance"].Aliases, "i") {
		t.Fatalf("instance aliases %v missing %q", commands["instance"].Aliases, "i")
	}
	if commands["instance"].Kind != "group" {
		t.Fatalf("instance kind = %q, want %q", commands["instance"].Kind, "group")
	}
	if !containsString(commands["instance"].Subcommands, "instance.list") {
		t.Fatalf("instance subcommands %v missing %q", commands["instance"].Subcommands, "instance.list")
	}
	if !containsString(commands["tool.delete"].Aliases, "rm") {
		t.Fatalf("tool.delete aliases %v missing %q", commands["tool.delete"].Aliases, "rm")
	}
	if _, ok := commands["instance.mobile.tunnel"]; ok {
		t.Fatalf("schema list leaked hidden command instance.mobile.tunnel")
	}
}

func TestCharacterization_HiddenCommandsAreNotResolvableThroughSchema(t *testing.T) {
	output, err := runAGR(t, "schema", "instance.mobile.tunnel", "-o", "json")
	if err == nil {
		t.Fatalf("hidden command unexpectedly resolved through schema\n%s", output)
	}
	if !strings.Contains(output, `"Code":"INVALID_USAGE"`) {
		t.Fatalf("hidden command schema failure did not report INVALID_USAGE\n%s", output)
	}
}

func TestCharacterization_JSONHelpMatchesSchemaForEveryPublicJSONCommand(t *testing.T) {
	root := contractRoot()
	for _, commandID := range allPublicCommandIDs(root) {
		schema := schemaForCommand(t, commandID)
		if !schema.SupportsJSON {
			continue
		}
		schemaData := mustCommandDataJSON(t, "schema", commandID, "-o", "json")
		helpData := mustCommandDataJSON(t, append(strings.Split(commandID, "."), "-o", "json", "--help")...)
		if !reflect.DeepEqual(schemaData, helpData) {
			t.Fatalf("schema/help JSON mismatch for %s\nschema=%s\nhelp=%s", commandID, schemaData, helpData)
		}
		for _, aliasPath := range aliasPathsForCommand(root, commandID) {
			aliasData := mustCommandDataJSON(t, "schema", aliasPath, "-o", "json")
			var aliasEnv struct {
				Name         string `json:"Name"`
				ResolvedFrom string `json:"ResolvedFrom"`
			}
			if err := json.Unmarshal([]byte(aliasData), &aliasEnv); err != nil {
				t.Fatalf("decode alias schema data for %s: %v\n%s", aliasPath, err, aliasData)
			}
			if aliasEnv.Name != commandID {
				t.Fatalf("alias schema name for %s = %q, want %q", aliasPath, aliasEnv.Name, commandID)
			}
			if aliasEnv.ResolvedFrom != aliasPath {
				t.Fatalf("alias resolvedFrom for %s = %q, want %q", aliasPath, aliasEnv.ResolvedFrom, aliasPath)
			}
			aliasHelpData := mustCommandDataJSON(t, append([]string{"help"}, append(strings.Split(aliasPath, "."), "-o", "json")...)...)
			if !reflect.DeepEqual(schemaData, aliasHelpData) {
				t.Fatalf("schema/help JSON mismatch for alias %s\nschema=%s\nhelp=%s", aliasPath, schemaData, aliasHelpData)
			}
		}
	}
}

func TestCharacterization_JSONHelpUsesHelpEnvelopeCommand(t *testing.T) {
	for _, args := range [][]string{
		{"-o", "json", "--help"},
		{"help", "instance", "-o", "json"},
		{"instance", "list", "--help", "-o", "json"},
	} {
		output, err := runAGR(t, args...)
		if err != nil {
			t.Fatalf("help command %v failed: %v\n%s", args, err, output)
		}
		var env struct {
			Command string `json:"Command"`
		}
		jsonStart := strings.Index(output, "{")
		if jsonStart < 0 {
			t.Fatalf("output did not contain JSON\n%s", output)
		}
		if err := json.Unmarshal([]byte(output[jsonStart:]), &env); err != nil {
			t.Fatalf("decode help envelope for %v: %v\n%s", args, err, output)
		}
		if env.Command != "help" {
			t.Fatalf("help envelope command for %v = %q, want %q\n%s", args, env.Command, "help", output)
		}
	}
}

func TestCharacterization_ExplainCoversCLIOwnedFailureCodes(t *testing.T) {
	cases := []struct {
		code     string
		contains []string
		excludes []string
	}{
		{
			code:     "INVALID_JSON_FLAG",
			contains: []string{"INVALID_JSON_FLAG (kind: usage, exit: 2", "tool.create", "tool.update"},
			excludes: []string{"Unknown error code."},
		},
		{
			code:     "REQUEST_ARG_CONFLICT",
			contains: []string{"REQUEST_ARG_CONFLICT (kind: usage, exit: 2", "tool.delete", "instance.delete"},
			excludes: []string{"Unknown error code."},
		},
		{
			code:     "ResourceNotFound.SandboxTool",
			contains: []string{"RESOURCENOTFOUND.SANDBOXTOOL (kind: not_found, exit: 1", "agr tool list", "tool.delete"},
			excludes: []string{"agr instance list"},
		},
		{
			code:     "CONFIG_EXISTS",
			contains: []string{"CONFIG_EXISTS (kind: usage, exit: 2", "agr config path"},
			excludes: []string{"Unknown error code."},
		},
		{
			code:     "255",
			contains: []string{"255 (kind: remote_execution_failed, exit: 255", "instance.code.run", "instance.exec"},
			excludes: []string{"Unknown error code."},
		},
		{
			code:     "4",
			contains: []string{"4 (kind: auth, exit: 4", "agr init --secret-id <id> --secret-key <key>"},
			excludes: []string{"Unknown error code."},
		},
	}

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			output, err := runAGR(t, "explain", tc.code)
			if err != nil {
				t.Fatalf("explain %s failed: %v\n%s", tc.code, err, output)
			}
			for _, want := range tc.contains {
				if !strings.Contains(output, want) {
					t.Fatalf("explain %s missing %q\n%s", tc.code, want, output)
				}
			}
			for _, bad := range tc.excludes {
				if strings.Contains(output, bad) {
					t.Fatalf("explain %s unexpectedly contained %q\n%s", tc.code, bad, output)
				}
			}
		})
	}
}

func TestCharacterization_SchemaFailuresCoverLocalValidationErrors(t *testing.T) {
	cases := []struct {
		command string
		codes   []string
	}{
		{command: "instance.browser.vnc", codes: []string{"INVALID_PORT"}},
		{command: "tool.list", codes: []string{"INVALID_PAGINATION"}},
		{command: "instance.code.run", codes: []string{"CONFLICTING_INPUTS", "MISSING_CODE", "UNSUPPORTED_LANGUAGE"}},
		{command: "tool.get", codes: []string{"MISSING_REQUIRED_ARG", "TOOL_NOT_FOUND"}},
	}

	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			schema := schemaForCommand(t, tc.command)
			for _, code := range tc.codes {
				requireFailureCode(t, schema, code)
			}
		})
	}
}

func requireCliFlag(t *testing.T, schema commandSchemaSnapshot, property, want string) {
	t.Helper()
	prop, ok := schema.RequestSchema[property]
	if !ok {
		t.Fatalf("schema RequestSchema missing property %q", property)
	}
	if prop.CliFlag == nil || *prop.CliFlag != want {
		t.Fatalf("schema property %s CliFlag = %#v, want %q", property, prop.CliFlag, want)
	}
}

func requireFlagAbsent(t *testing.T, schema commandSchemaSnapshot, name string) {
	t.Helper()
	if _, ok := schema.Flags[name]; ok {
		t.Fatalf("schema flags unexpectedly include %q: %#v", name, schema.Flags[name])
	}
}

func requirePropertyType(t *testing.T, schema commandSchemaSnapshot, property, want string) {
	t.Helper()
	prop, ok := schema.RequestSchema[property]
	if !ok {
		t.Fatalf("schema RequestSchema missing property %q", property)
	}
	if prop.Type != want {
		t.Fatalf("schema property %s Type = %q, want %q", property, prop.Type, want)
	}
}

func requireArgNames(t *testing.T, schema commandSchemaSnapshot, want ...string) {
	t.Helper()
	if len(schema.Args) != len(want) {
		t.Fatalf("schema args = %#v, want names %v", schema.Args, want)
	}
	for i, item := range want {
		if schema.Args[i].Name != item {
			t.Fatalf("schema arg %d = %q, want %q", i, schema.Args[i].Name, item)
		}
	}
}

func requireFailureCode(t *testing.T, schema commandSchemaSnapshot, want string) {
	t.Helper()
	if !containsString(schema.Failures, want) {
		t.Fatalf("schema failures %v missing %q", schema.Failures, want)
	}
}

func requireIncompatibleWith(t *testing.T, schema commandSchemaSnapshot, flagName, want string) {
	t.Helper()
	flag, ok := schema.Flags[flagName]
	if !ok {
		t.Fatalf("schema flags missing %q", flagName)
	}
	if !containsString(flag.IncompatibleWith, want) {
		t.Fatalf("schema flag %s incompatibleWith %v missing %q", flagName, flag.IncompatibleWith, want)
	}
}

func requireAllowsOutput(t *testing.T, schema commandSchemaSnapshot, flagName string, want ...string) {
	t.Helper()
	flag, ok := schema.Flags[flagName]
	if !ok {
		t.Fatalf("schema flags missing %q", flagName)
	}
	for _, item := range want {
		if !containsString(flag.AllowsOutput, item) {
			t.Fatalf("schema flag %s allowsOutput %v missing %q", flagName, flag.AllowsOutput, item)
		}
	}
}

func runAGR(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestCharacterizationHelperProcess", "--"}, args...)...)
	cmd.Env = append(os.Environ(),
		"AGR_CHARACTERIZATION_HELPER=1",
		"HOME="+t.TempDir(),
		"USERPROFILE="+t.TempDir(),
		"TENCENTCLOUD_SECRET_ID=",
		"TENCENTCLOUD_SECRET_KEY=",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCharacterizationHelperProcess(t *testing.T) {
	if os.Getenv("AGR_CHARACTERIZATION_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	ios := &iostreams.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	cli.SetIOStreams(ios)
	root := cli.RootCmd()
	if err := attachCommands(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	root.SetArgs(args)
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	if err := root.Execute(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func leafCommandIDs(root *cobra.Command) []string {
	var ids []string
	var walk func(cmd *cobra.Command, prefix []string)
	walk = func(cmd *cobra.Command, prefix []string) {
		for _, child := range cmd.Commands() {
			if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() || child.Name() == "help" {
				continue
			}
			next := append(append([]string{}, prefix...), child.Name())
			if child.Runnable() && !child.HasAvailableSubCommands() {
				ids = append(ids, strings.Join(next, "."))
			}
			if child.HasAvailableSubCommands() {
				walk(child, next)
			}
		}
	}
	walk(root, nil)
	return ids
}

func allPublicCommandIDs(root *cobra.Command) []string {
	var ids []string
	var walk func(cmd *cobra.Command, prefix []string)
	walk = func(cmd *cobra.Command, prefix []string) {
		for _, child := range cmd.Commands() {
			if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
				continue
			}
			next := append(append([]string{}, prefix...), child.Name())
			ids = append(ids, strings.Join(next, "."))
			if child.HasAvailableSubCommands() {
				walk(child, next)
			}
		}
	}
	walk(root, nil)
	return ids
}

func aliasPathsForCommand(root *cobra.Command, commandID string) []string {
	cmd, ok := findCobraCommand(root, commandID)
	if !ok {
		return nil
	}
	segments := []*cobra.Command{}
	for current := cmd; current != nil && current.Parent() != nil; current = current.Parent() {
		segments = append([]*cobra.Command{current}, segments...)
	}
	var paths []string
	var walk func(idx int, parts []string, usedAlias bool)
	walk = func(idx int, parts []string, usedAlias bool) {
		if idx == len(segments) {
			if usedAlias {
				paths = append(paths, strings.Join(parts, "."))
			}
			return
		}
		current := segments[idx]
		names := append([]string{current.Name()}, current.Aliases...)
		for j, name := range names {
			walk(idx+1, append(parts, name), usedAlias || j > 0)
		}
	}
	walk(0, nil, false)
	return paths
}

func mustCommandDataJSON(t *testing.T, args ...string) string {
	t.Helper()
	output, err := runAGR(t, args...)
	if err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, output)
	}
	var env struct {
		Data json.RawMessage `json:"Data"`
	}
	jsonStart := strings.Index(output, "{")
	if jsonStart < 0 {
		t.Fatalf("output did not contain JSON\n%s", output)
	}
	if err := json.Unmarshal([]byte(output[jsonStart:]), &env); err != nil {
		t.Fatalf("decode envelope for %v: %v\n%s", args, err, output)
	}
	return string(env.Data)
}
