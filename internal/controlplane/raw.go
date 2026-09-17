package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cloudapi"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/config"
)

// RawCallResult is the normalized response from a raw Cloud API call.
type RawCallResult struct {
	Response  any
	Warnings  []string
	MetaExtra map[string]any
}

// RawAPISender sends a raw signed API request and returns the response body.
type RawAPISender func(ctx context.Context, action, cloudEndpoint string, payload []byte) ([]byte, error)

// RawAPIClient executes raw Cloud API calls for the `agr api call` command.
type RawAPIClient struct {
	Sender RawAPISender
}

// RawCall invokes action with raw JSON bytes and returns parsed response metadata.
func (c RawAPIClient) RawCall(ctx context.Context, action string, raw []byte) (*RawCallResult, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	cloudEndpoint := config.GetCloudEndpoint()
	var (
		respBody []byte
		err      error
	)
	if c.Sender != nil {
		respBody, err = c.Sender(ctx, action, cloudEndpoint, raw)
	} else {
		caller, cerr := cloudapi.NewWithToken(config.GetSecretID(), config.GetSecretKey(), config.GetToken(), config.GetRegion(), cloudEndpoint)
		if cerr != nil {
			return nil, cerr
		}
		respBody, err = caller.Call(ctx, action, raw)
	}
	if err != nil {
		return nil, fmt.Errorf("api call %s: %w", action, err)
	}

	var parsed any
	decoder := json.NewDecoder(bytes.NewReader(respBody))
	decoder.UseNumber()
	if uerr := decoder.Decode(&parsed); uerr != nil {
		parsed = string(respBody)
	}
	return &RawCallResult{
		Response: parsed,
		Warnings: []string{
			fmt.Sprintf("agr api call: %s sent as raw payload via %s (%s/%s); this bypasses resource command mapping.", action, cloudEndpoint, cloudapi.Service, cloudapi.Version),
		},
		MetaExtra: map[string]any{
			"Action":        action,
			"Service":       cloudapi.Service,
			"ApiVersion":    cloudapi.Version,
			"RequestMode":   "raw_action_call",
			"CloudEndpoint": cloudEndpoint,
		},
	}, nil
}
