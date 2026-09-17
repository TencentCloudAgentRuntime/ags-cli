// Package cloudapi exposes a thin wrapper around the Tencent Cloud
// common client. It is the runtime backend for `agr api call`, which
// sends JSON payloads to AGS without typed SDK models. Resource commands
// also use this transport when the SDK cannot express their active contract.
//
// The wrapper deliberately keeps the surface small: caller passes an
// Action name and a raw JSON byte slice, the package returns the raw
// JSON response. No field validation is applied beyond confirming the
// payload's top-level value is a JSON object - resource commands stay
// in charge of channel contract validation.
package cloudapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tchttp "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/http"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

// Service is the AGS service name in TencentCloud's signing scheme.
const Service = "ags"

// Version is the wire / API version sent to TencentCloud's common
// client (NextPlan §9.4). The repository organises the API metadata
// under api/ags/<sourceVersion>/, so the on-disk source directory and
// the wire version are kept as separate constants:
//
//	SourceVersion = "v20250920"  (filesystem layout)
//	Version       = "2025-09-20" (wire / X-TC-Version header)
const (
	Version       = "2025-09-20"
	SourceVersion = "v20250920"
)

// Caller invokes a raw API action.
type Caller struct {
	client *common.Client
}

// New constructs a Caller bound to a specific control-plane endpoint.
func New(secretID, secretKey, region, cloudEndpoint string) (*Caller, error) {
	return NewWithToken(secretID, secretKey, "", region, cloudEndpoint)
}

// NewWithToken supports both permanent and temporary Tencent Cloud credentials.
func NewWithToken(secretID, secretKey, token, region, cloudEndpoint string) (*Caller, error) {
	if cloudEndpoint == "" {
		return nil, fmt.Errorf("cloud endpoint must not be empty")
	}
	credential := common.NewTokenCredential(secretID, secretKey, token)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = cloudEndpoint
	cpf.NetworkFailureMaxRetries = 0
	cpf.RateLimitExceededMaxRetries = 0
	cpf.DisableRegionBreaker = true

	client := common.NewCommonClient(credential, region, cpf)

	// Support AGR_INSECURE_SKIP_VERIFY=1 for integration tests with self-signed certs.
	// Only effective when endpoint is localhost to prevent misuse in production.
	if os.Getenv("AGR_INSECURE_SKIP_VERIFY") == "1" && isLoopbackEndpoint(cloudEndpoint) {
		client.WithHttpTransport(&http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only, localhost guard
		})
	}

	return &Caller{client: client}, nil
}

// isLoopbackEndpoint checks if the endpoint is localhost/127.0.0.1/[::1].
func isLoopbackEndpoint(endpoint string) bool {
	host := endpoint
	// Strip port if present.
	if idx := strings.LastIndex(endpoint, ":"); idx > 0 {
		host = endpoint[:idx]
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Call executes the given action with a raw JSON request payload and
// returns the raw JSON response bytes. The caller is responsible for
// JSON-unmarshalling the response.
func (c *Caller) Call(ctx context.Context, action string, request []byte) ([]byte, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("cloudapi caller is not initialised")
	}
	if action == "" {
		return nil, fmt.Errorf("action must not be empty")
	}
	if !isJSONObject(request) {
		return nil, fmt.Errorf("request payload must be a JSON object")
	}

	req := &wireRequest{BaseRequest: &tchttp.BaseRequest{}, payload: request}
	req.Init().WithApiInfo(Service, Version, action)
	req.SetContext(ctx)
	resp := &wireResponse{}
	if err := c.client.Send(req, resp); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return resp.body, nil
}

// isJSONObject reports whether data is a JSON object. Used to reject
// arrays / scalars at the api-call boundary.
func isJSONObject(data []byte) bool {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw.(map[string]any)
	return ok
}

// CommonResponse decodes into float64 maps. Keep the original JSON instead so
// extension fields and integer precision survive the SDK transport.
type wireResponse struct {
	tchttp.BaseResponse
	body []byte
}

func (r *wireResponse) UnmarshalJSON(data []byte) error {
	r.body = append(r.body[:0], data...)
	return nil
}

// Marshal through the SDK's request interface without its map decoder, whose
// Number type differs from encoding/json.Number and can stringify numbers.
type wireRequest struct {
	*tchttp.BaseRequest
	payload []byte
}

func (r *wireRequest) MarshalJSON() ([]byte, error) { return r.payload, nil }
