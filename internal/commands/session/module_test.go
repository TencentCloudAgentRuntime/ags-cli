package session

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apicli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
)

type fixtureControlPlane struct{ response any }

func (f fixtureControlPlane) Call(context.Context, string, map[string]any) (any, error) {
	return f.response, nil
}

func TestSessionModuleOwnsTextWithoutChangingResponse(t *testing.T) {
	data := map[string]any{"Session": map[string]any{"SessionId": "session-test", "CreateTime": "created", "UpdateTime": "updated"}, "RequestId": "request-test"}
	module := NewModule(apicli.APIDescriptor{Spec: command.Spec{ID: "session.get", Path: []string{"session", "get"}}, API: apicli.APISpec{Action: "DescribeSession"}})
	if module.Descriptor.Source != command.SourceMixedAPI || module.Descriptor.Generated == nil {
		t.Fatal("module lost generated API identity")
	}
	runtime, err := module.Build(command.Deps{ControlPlane: fixtureControlPlane{data}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Handler.Run(t.Context(), command.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Data, data) || result.Text == nil {
		t.Fatal("response changed or text renderer absent")
	}
	var text bytes.Buffer
	result.Text(&text)
	if text.Len() == 0 {
		t.Fatal("module did not render text")
	}
}
