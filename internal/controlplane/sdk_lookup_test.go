package controlplane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apimeta"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

func TestSDKCallUsesTypedInstanceLookup(t *testing.T) {
	config.SetSecretID("sid")
	config.SetSecretKey("skey")
	// The full contract requires lossless JSON for fields the SDK cannot
	// represent. Use a compatible response subset to exercise typed dispatch.
	spec := testContract(t)
	spec.Objects["SandboxInstance"] = &apimeta.Object{Members: []apimeta.Member{
		{Name: "InstanceId", Type: "string", Member: "string"},
		{Name: "Status", Type: "string", Member: "string"},
	}}
	if needsDynamic(spec, "DescribeSandboxInstances") {
		t.Fatal("SDK-compatible lookup contract selected dynamic transport")
	}
	apiClient, err := ags.NewClient(common.NewCredential("sid", "skey"), "ap-test", profile.NewClientProfile())
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"ins-b", "ins-a", "ins-b"}
	calls := 0
	apiClient.WithHttpTransport(lookupRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Header.Get("X-TC-Action") != "DescribeSandboxInstances" || req.Header.Get("X-TC-Version") != "2025-09-20" || req.Header.Get("Authorization") == "" {
			t.Fatalf("unsigned request or incorrect API identity: %v", req.Header)
		}
		var payload struct{ InstanceIds []string }
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(payload.InstanceIds, wantIDs) {
			t.Fatalf("SDK changed IDs: got %v, want %v", payload.InstanceIds, wantIDs)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"Response":{"InstanceSet":[{"InstanceId":"ins-b","Status":"PAUSED"},{"InstanceId":"ins-a","Status":"RUNNING"}],"RequestId":"lookup-request"}}`)),
		}, nil
	}))
	sdk := &SDK{
		Contract: spec,
		Client:   apiClient,
		RawSender: func(context.Context, string, string, []byte) ([]byte, error) {
			t.Fatal("SDK-compatible lookup used raw transport")
			return nil, nil
		},
	}
	value, err := sdk.Call(t.Context(), "DescribeSandboxInstances", map[string]any{"InstanceIds": wantIDs})
	if err != nil {
		t.Fatal(err)
	}
	response, ok := value.(*ags.DescribeSandboxInstancesResponseParams)
	if !ok || response == nil || len(response.InstanceSet) != 2 || response.RequestId == nil || *response.RequestId != "lookup-request" || calls != 1 {
		t.Fatalf("invalid typed lookup result: value=%#v calls=%d", value, calls)
	}
	for i, id := range wantIDs[:2] {
		instance := response.InstanceSet[i]
		if instance == nil || instance.InstanceId == nil || *instance.InstanceId != id {
			t.Fatalf("SDK changed response order: %#v", response.InstanceSet)
		}
	}
}

type lookupRoundTripper func(*http.Request) (*http.Response, error)

func (f lookupRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
