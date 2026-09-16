package request

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	var cli *output.CLIError
	if errors.As(err, &cli) && cli.Failure != nil {
		return cli.Failure.Code
	}
	return ""
}

func TestParseFlagAcceptsObject(t *testing.T) {
	got, err := ParseFlag(`{"Limit":1}`)
	if err != nil {
		t.Fatalf("ParseFlag error: %v", err)
	}
	if got["Limit"] != json.Number("1") {
		t.Fatalf("Limit=%v", got["Limit"])
	}
}

func TestParseFlagRejectsArray(t *testing.T) {
	_, err := ParseFlag(`[1]`)
	if code := errorCode(err); code != "INVALID_REQUEST_JSON" {
		t.Fatalf("code=%s err=%v", code, err)
	}
}

func TestValidatePayloadRejectsEmpty(t *testing.T) {
	err := ValidatePayload("instance.list", nil)
	if code := errorCode(err); code != "INVALID_REQUEST_JSON" {
		t.Fatalf("code=%s err=%v", code, err)
	}
}

func TestMergePositionalAddsField(t *testing.T) {
	raw, err := MergePositional(`{"Timeout":"5m"}`, "InstanceId", "ins-1")
	if err != nil {
		t.Fatalf("MergePositional error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["InstanceId"] != "ins-1" || got["Timeout"] != "5m" {
		t.Fatalf("merged=%v", got)
	}
}

func TestMergePositionalRejectsMismatch(t *testing.T) {
	_, err := MergePositional(`{"InstanceId":"ins-other"}`, "InstanceId", "ins-1")
	if code := errorCode(err); code != "REQUEST_ARG_CONFLICT" {
		t.Fatalf("code=%s err=%v", code, err)
	}
}
