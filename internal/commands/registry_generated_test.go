package commands

import (
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

func TestWaitFlagScope(t *testing.T) {
	registry, err := Registry()
	if err != nil {
		t.Fatalf("Registry returned error: %v", err)
	}
	wantWait := []string{
		"instance.create",
		"instance.get",
		"instance.pause",
		"instance.resume",
		"instance.update",
		"tool.create",
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
	for _, id := range []string{"instance.delete", "instance.list", "tool.delete", "tool.list"} {
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
