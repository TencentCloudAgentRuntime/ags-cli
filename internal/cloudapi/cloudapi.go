// Package cloudapi exposes a thin wrapper around the Tencent Cloud
// common client. It is the runtime backend for `agr api call`, which
// sends arbitrary JSON payloads to AGS without going through the typed
// SDK requests.
//
// The wrapper deliberately keeps the surface small: caller passes an
// Action name and a raw JSON byte slice, the package returns the raw
// JSON response. No field validation is applied beyond confirming the
// payload's top-level value is a JSON object - resource commands stay
// in charge of strict typed validation.
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
	if cloudEndpoint == "" {
		return nil, fmt.Errorf("cloud endpoint must not be empty")
	}
	credential := common.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = cloudEndpoint

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

	req := tchttp.NewCommonRequest(Service, Version, action)
	if err := req.SetActionParameters(request); err != nil {
		return nil, fmt.Errorf("failed to set action parameters: %w", err)
	}
	resp := tchttp.NewCommonResponse()
	if err := c.client.SendOctetStream(req, resp); err != nil {
		// fall back to ordinary Send when octet-stream is not supported
		_ = err
		req2 := tchttp.NewCommonRequest(Service, Version, action)
		if err := req2.SetActionParameters(request); err != nil {
			return nil, fmt.Errorf("failed to set action parameters: %w", err)
		}
		resp2 := tchttp.NewCommonResponse()
		if err := c.client.Send(req2, resp2); err != nil {
			return nil, err
		}
		return resp2.GetBody(), nil
	}
	return resp.GetBody(), nil
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
