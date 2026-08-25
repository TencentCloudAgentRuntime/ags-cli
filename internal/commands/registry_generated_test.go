package commands

import (
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

func TestRegistryIncludesAllKnownCommandModules(t *testing.T) {
	registry, err := Registry()
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	want := []string{
		"api.call",
		"apikey.create",
		"apikey.delete",
		"apikey.list",
		"deployment.create",
		"deployment.delete",
		"deployment.get",
		"deployment.list",
		"deployment.proxy",
		"deployment.update",
		"credential.oauth2.acquire",
		"credential.oauth2.complete",
		"credential.provider.create",
		"credential.provider.delete",
		"credential.provider.get",
		"credential.provider.list",
		"credential.provider.update",
		"credential.secret.delete",
		"credential.secret.get",
		"credential.secret.list",
		"credential.secret.set",
		"identity.create",
		"identity.delete",
		"identity.get",
		"identity.list",
		"identity.token.create",
		"identity.update",
		"pre-cache-image-task.create",
		"pre-cache-image-task.get",
		"instance.browser.vnc",
		"instance.code.run",
		"instance.create",
		"instance.debug",
		"instance.delete",
		"instance.exec",
		"instance.file.download",
		"instance.file.upload",
		"instance.get",
		"instance.list",
		"instance.login",
		"instance.mobile.adb",
		"instance.mobile.connect",
		"instance.mobile.disconnect",
		"instance.mobile.list",
		"instance.mobile.tunnel",
		"instance.pause",
		"instance.proxy",
		"instance.resume",
		"instance.update",
		"tool.create",
		"tool.delete",
		"tool.fork",
		"tool.get",
		"tool.list",
		"tool.update",
	}
	for _, id := range want {
		if _, ok := registry.Lookup(id); !ok {
			t.Fatalf("registry missing %s", id)
		}
	}
	if got := len(registry.Modules()); got != len(want) {
		t.Fatalf("module count = %d, want %d", got, len(want))
	}
}

func TestDeploymentExamplesCoverOperationalScenarios(t *testing.T) {
	registry, err := Registry()
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	wants := map[string][]string{
		"deployment.create": {
			"Example - Scale to zero after five idle minutes",
			"Example - Keep two instances active",
			`"Mode":"BEST_EFFORT"`,
			`"Mode":"STRICT"`,
			`"Mode":"EXCLUSIVE"`,
			"@scaling.json",
		},
		"deployment.delete": {
			"Example - Delete and wait for completion",
			"--wait=false",
			"--timeout 30m",
			"--timeout 0",
		},
		"deployment.get": {
			"Example - Inspect the Deployment",
			"Example - Print the complete API response",
			".Data.Deployment.Status",
		},
		"deployment.list": {
			`"Name":"tool-id"`,
			`"Name":"deployment-name-like"`,
			"DELETE_FAILED",
			".Data.DeploymentSet[].DeploymentId",
		},
		"deployment.update": {
			"Example - Increase active capacity",
			`"IdleAction":"PAUSE"`,
			`"IdleAction":"STOP"`,
			"Example - Clear all tags",
		},
		"deployment.proxy": {
			"Example - Forward the same local and remote port",
			"Example - Avoid a local port conflict",
			"--verbose",
		},
	}
	for id, required := range wants {
		module, ok := registry.Lookup(id)
		if !ok {
			t.Fatalf("registry missing %s", id)
		}
		examples := strings.Join(module.Descriptor.Spec.Examples, "\n")
		for _, want := range required {
			if !strings.Contains(examples, want) {
				t.Errorf("%s examples missing %q:\n%s", id, want, examples)
			}
		}
		seen := map[string]bool{}
		for _, example := range module.Descriptor.Spec.Examples {
			invocation := primaryAgrInvocation(example)
			if invocation == "" {
				t.Errorf("%s example has no agr invocation:\n%s", id, example)
				continue
			}
			if seen[invocation] {
				t.Errorf("%s repeats the same agr invocation in multiple examples:\n%s", id, invocation)
			}
			seen[invocation] = true
		}
	}
}

func primaryAgrInvocation(example string) string {
	var commandLines []string
	for _, line := range strings.Split(example, "\n") {
		line = strings.TrimSpace(line)
		if len(commandLines) == 0 && !strings.Contains(line, "agr deployment ") {
			continue
		}
		commandLines = append(commandLines, line)
		if !strings.HasSuffix(line, `\`) {
			break
		}
	}
	return strings.Join(commandLines, " ")
}

func TestWaitFlagScope(t *testing.T) {
	registry, err := Registry()
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	wantWait := []string{
		"instance.create",
		"instance.delete",
		"instance.get",
		"instance.pause",
		"instance.resume",
		"instance.update",
		"tool.create",
		"tool.delete",
		"tool.fork",
		"tool.get",
		"tool.update",
	}
	for _, id := range wantWait {
		module, ok := registry.Lookup(id)
		if !ok {
			t.Fatalf("registry missing %s", id)
		}
		flag, ok := findFlag(module.Descriptor.Spec.Flags, "wait")
		if !ok || flag.Type != command.FlagBool || !flag.Workflow {
			t.Errorf("%s --wait = %#v, present = %v", id, flag, ok)
		}
		if module.Descriptor.Generated != nil {
			if flag, ok := findFlag(module.Descriptor.Generated.Spec.Flags, "wait"); ok {
				t.Errorf("%s generated API snapshot unexpectedly includes workflow --wait: %#v", id, flag)
			}
		}
	}
	for _, id := range []string{"instance.list", "tool.list"} {
		module, _ := registry.Lookup(id)
		if flag, ok := findFlag(module.Descriptor.Spec.Flags, "wait"); ok {
			t.Errorf("%s unexpectedly exposes --wait: %#v", id, flag)
		}
	}
}

func findFlag(flags []command.FlagSpec, name string) (command.FlagSpec, bool) {
	for _, flag := range flags {
		if flag.Name == name {
			return flag, true
		}
	}
	return command.FlagSpec{}, false
}
