//go:build preview

package harnesscli

import (
	"encoding/json"
	"maps"
	"strings"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/apivalue"
)

const redactedMCPHeaderValue = "[REDACTED]"

// MarshalJSON redacts only the presentation of changes. DiffSpecs compares the
// original values so rotating a secret still produces a change.
func (change Change) MarshalJSON() ([]byte, error) {
	type wireChange Change
	return json.Marshal(wireChange(change.redacted()))
}

func (change Change) redacted() Change {
	var redact func(any) any
	switch {
	case change.Path == "MCPServers":
		redact = redactMCPServers
	case strings.HasPrefix(change.Path, "MCPServers["):
		switch {
		case strings.HasSuffix(change.Path, "].Headers"):
			redact = redactMCPHeaders
		case strings.Contains(change.Path, "].Headers."):
			redact = func(any) any { return redactedMCPHeaderValue }
		case strings.HasSuffix(change.Path, "]"):
			redact = redactMCPServer
		}
	}
	if redact != nil {
		if change.Old != nil {
			change.Old = redact(change.Old)
		}
		if change.New != nil {
			change.New = redact(change.New)
		}
	}
	return change
}

// RedactHarnessForDiagnostics copies a Harness or revision for status output.
// Configuration reads, request bodies and stored objects retain their values.
func RedactHarnessForDiagnostics(resource apivalue.Object) apivalue.Object {
	if resource == nil {
		return nil
	}
	out := maps.Clone(resource)
	if spec := resource.Object("Spec"); spec != nil {
		if servers, present := spec["MCPServers"]; present {
			spec["MCPServers"] = redactMCPServers(servers)
		}
		out["Spec"] = spec
	}
	return out
}

func redactMCPServers(value any) any {
	servers, err := (apivalue.Object{"MCPServers": value}).ReadObjects("MCPServers")
	if err != nil {
		return redactedMCPHeaderValue
	}
	for _, server := range servers {
		redactMCPServerHeaders(server)
	}
	return servers
}

func redactMCPServer(value any) any {
	server, err := apivalue.Decode(value)
	if err != nil {
		return redactedMCPHeaderValue
	}
	redactMCPServerHeaders(server)
	return server
}

func redactMCPServerHeaders(server apivalue.Object) {
	if headers, present := server["Headers"]; present {
		server["Headers"] = redactMCPHeaders(headers)
	}
}

func redactMCPHeaders(value any) any {
	headers, err := (apivalue.Object{"Headers": value}).ReadObjects("Headers")
	if err != nil {
		return redactedMCPHeaderValue
	}
	for _, header := range headers {
		if _, present := header["Value"]; present {
			header["Value"] = redactedMCPHeaderValue
		}
	}
	return headers
}
