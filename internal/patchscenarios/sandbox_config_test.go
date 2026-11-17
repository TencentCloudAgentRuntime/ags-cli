package patchscenarios

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

// Every fault below must break its registered scenario. A fault that still
// passes means the corresponding assertion is not real evidence.
var sandboxKeepaliveFaults = []string{
	"",
	"drop-keepalive-create", "drop-keepalive-tool-update",
	"drop-instance-create-config", "drop-instance-update-config",
	"drop-auto-flags", "ignore-false", "normalize-timeout",
	"freeze-inheritance", "fabricate-default", "accept-invalid",
	"retain-instance", "retain-tool",
}

var sandboxLogFaults = []string{
	"",
	"ignore-log-format", "drop-raw-default", "log-case", "accept-invalid-log",
	"retain-tool",
}

const (
	sandboxFixtureTopic = "topic-fixture"
	sandboxFixtureRole  = "qcs::cam::uin/100000000001:roleName/AGSFixtureRole"
	sandboxFixtureTime  = "2026-11-13T00:00:00Z"
)

var sandboxFixtureLogVars = []string{"AGR_TEST_CLS_TOPIC_ID", "AGR_TEST_ROLE_ARN"}

func sandboxLogEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGR_TEST_CLS_TOPIC_ID", sandboxFixtureTopic)
	t.Setenv("AGR_TEST_ROLE_ARN", sandboxFixtureRole)
}

func sandboxEnv(t *testing.T, serverURL, caFile string) []string {
	t.Helper()
	return []string{
		"HOME=" + t.TempDir(),
		"PATH=" + os.Getenv("PATH"),
		"TENCENTCLOUD_SECRET_ID=fake",
		"TENCENTCLOUD_SECRET_KEY=fake",
		"TENCENTCLOUD_TOKEN=fake-session",
		"AGR_REGION=ap-guangzhou",
		"AGR_CLOUD_ENDPOINT=" + strings.TrimPrefix(serverURL, "https://"),
		// The raw pass-through accepts a loopback certificate override, but the
		// typed SDK client only trusts the system pool. A fixture CA in
		// SSL_CERT_FILE keeps both channels on the same TLS server.
		"SSL_CERT_FILE=" + caFile,
		"AGR_INSECURE_SKIP_VERIFY=1",
	}
}

// sandboxTLSServer serves the fixture with a certificate signed by a CA the
// candidate CLI trusts through SSL_CERT_FILE, so typed SDK actions are exercised
// too instead of only the dynamic pass-through.
func sandboxTLSServer(t *testing.T, handler http.Handler) (*httptest.Server, string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	notBefore, notAfter := time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "agr-fixture-ca"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("ca certificate: %v", err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "agr-fixture"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("leaf certificate: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}}}
	server.StartTLS()
	caFile := filepath.Join(t.TempDir(), "fixture-ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return server, caFile
}

// sandboxBuildCandidate compiles the preview channel once per test binary run.
func sandboxBuildCandidate(t *testing.T) string {
	t.Helper()
	root := filepath.Clean("../..")
	binary := filepath.Join(t.TempDir(), "agr-preview")
	build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-tags=preview", "-o", binary, "./cmd/agr")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return binary
}

type sandboxFixtureInstance struct {
	id, toolID, status string
	// keepalive holds only the members the instance set explicitly; inheritance
	// is resolved on every read, like the service contract describes.
	keepalive map[string]any
	pause     *bool
	resume    *bool
}

type sandboxFixture struct {
	t         *testing.T
	mu        sync.Mutex
	fault     string
	calls     int
	sequence  int
	tools     map[string]map[string]any
	instances map[string]*sandboxFixtureInstance
	// deferred holds the configuration members of a tool the service has not
	// published yet; pending counts the observations left before publication.
	deferred map[string]map[string]any
	pending  map[string]int
}

func newSandboxFixture(t *testing.T, fault string) *sandboxFixture {
	return &sandboxFixture{
		t:         t,
		fault:     fault,
		tools:     map[string]map[string]any{},
		instances: map[string]*sandboxFixtureInstance{},
		deferred:  map[string]map[string]any{},
		pending:   map[string]int{},
	}
}

// holdToolPending models the real asynchronous create/fork: the tool exists
// before its configuration is published.
func (f *sandboxFixture) holdToolPending(id string) {
	if f.fault != "pending-tool-config" {
		return
	}
	stored := f.tools[id]
	held := map[string]any{}
	for _, key := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume", "LogConfiguration"} {
		if value, ok := stored[key]; ok {
			held[key] = value
			delete(stored, key)
		}
	}
	stored["Status"] = "CREATING"
	f.deferred[id] = held
	f.pending[id] = 2
}

// observeTool publishes a deferred tool after the configured number of reads.
func (f *sandboxFixture) observeTool(id string) {
	if f.pending[id] == 0 {
		return
	}
	f.pending[id]--
	if f.pending[id] > 0 {
		return
	}
	for key, value := range f.deferred[id] {
		f.tools[id][key] = value
	}
	f.tools[id]["Status"] = "ACTIVE"
	delete(f.deferred, id)
	delete(f.pending, id)
}

func (f *sandboxFixture) nextID(prefix string) string {
	f.sequence++
	return fmt.Sprintf("%s-fixture%d", prefix, f.sequence)
}

func (f *sandboxFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Token") != "fake-session" {
		f.t.Error("missing signed temporary credentials")
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Error(err)
		return
	}
	action := r.Header.Get("X-TC-Action")
	response := map[string]any{"RequestId": "fixture"}
	fail := func(code, message string) {
		response["Error"] = map[string]any{"Code": code, "Message": message}
	}
	switch action {
	case "CreateSandboxTool":
		id := f.nextID("tool")
		stored := map[string]any{"ToolId": id, "Status": "ACTIVE"}
		if failure := f.copyToolFields(stored, req, f.fault != "drop-keepalive-create", f.fault != "drop-auto-flags"); failure != nil {
			response["Error"] = failure
			break
		}
		if failure := f.storeLogConfiguration(stored, req); failure != nil {
			delete(f.tools, id)
			response["Error"] = failure
			break
		}
		f.tools[id] = stored
		f.holdToolPending(id)
		response["ToolId"] = id
	case "DescribeSandboxToolList":
		var set []any
		ids := fixtureStrings(req["ToolIds"])
		for _, id := range slices.Sorted(maps.Keys(f.tools)) {
			if f.fault != "drop-tool-list-filter" && len(ids) > 0 && !slices.Contains(ids, id) {
				continue
			}
			f.observeTool(id)
			set = append(set, f.tools[id])
		}
		response["SandboxToolSet"], response["TotalCount"] = set, len(set)
	case "UpdateSandboxTool":
		id, _ := req["ToolId"].(string)
		stored, ok := f.tools[id]
		if !ok {
			fail("ResourceNotFound.ToolNotExist", "missing fixture tool")
			break
		}
		if failure := f.copyToolFields(stored, req, f.fault != "drop-keepalive-tool-update", f.fault != "drop-auto-flags"); failure != nil {
			response["Error"] = failure
			break
		}
		response["ToolId"] = id
	case "DeleteSandboxTool":
		id, _ := req["ToolId"].(string)
		if f.fault != "retain-tool" {
			delete(f.tools, id)
		}
		response["ToolId"] = id
	case "StartSandboxInstance":
		id := f.nextID("ins")
		toolID, _ := req["ToolId"].(string)
		if _, ok := f.tools[toolID]; !ok {
			fail("ResourceNotFound.ToolNotExist", "missing fixture tool")
			break
		}
		instance := &sandboxFixtureInstance{id: id, toolID: toolID, status: "STARTING"}
		if failure := f.applyInstanceConfiguration(instance, req, f.fault != "drop-instance-create-config"); failure != nil {
			response["Error"] = failure
			break
		}
		if f.fault == "freeze-inheritance" {
			// The service would keep the resolved values instead of following a
			// later tool update.
			mode, timeout := f.resolvedKeepalive(instance)
			instance.keepalive = map[string]any{}
			if mode != "" {
				instance.keepalive["KeepaliveMode"] = mode
			}
			if timeout != "" {
				instance.keepalive["KeepaliveTimeout"] = timeout
			}
		}
		f.instances[id] = instance
		response["Instance"] = f.instanceObject(instance, true)
	case "AcquireSandboxInstanceToken":
		instanceID, _ := req["InstanceId"].(string)
		if _, ok := f.instances[instanceID]; !ok {
			fail("ResourceNotFound.InstanceNotExist", "missing fixture instance")
			break
		}
		response["Token"] = "fixture-instance-token"
		response["ExpiresAt"] = "2026-11-13T01:00:00Z"
	case "DescribeSandboxInstanceList", "DescribeSandboxInstances":
		ids := fixtureStrings(req["InstanceIds"])
		var set []any
		for _, id := range slices.Sorted(maps.Keys(f.instances)) {
			if len(ids) > 0 && !slices.Contains(ids, id) {
				continue
			}
			instance := f.instances[id]
			if instance.status == "STARTING" {
				instance.status = "RUNNING"
			}
			set = append(set, f.instanceObject(instance, false))
		}
		response["InstanceSet"], response["TotalCount"] = set, len(set)
	case "UpdateSandboxInstance":
		id, _ := req["InstanceId"].(string)
		instance, ok := f.instances[id]
		if !ok {
			fail("ResourceNotFound.InstanceNotExist", "missing fixture instance")
			break
		}
		if f.fault != "drop-instance-update-config" {
			if failure := f.applyInstanceConfiguration(instance, req, true); failure != nil {
				response["Error"] = failure
				break
			}
		}
		response["InstanceId"] = id
	case "StopSandboxInstance":
		id, _ := req["InstanceId"].(string)
		instance, ok := f.instances[id]
		if !ok {
			fail("ResourceNotFound.InstanceNotExist", "missing fixture instance")
			break
		}
		if f.fault != "retain-instance" {
			instance.status = "STOPPED"
		}
		response["InstanceId"] = id
	default:
		f.t.Errorf("unexpected action %q", action)
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"Response": response}); err != nil {
		f.t.Error(err)
	}
}

// copyToolFields applies the request's keepalive, boolean and description fields
// to the stored tool. keepalive and booleans are gated separately so a fault can
// drop one family without hiding the other.
func (f *sandboxFixture) copyToolFields(stored, req map[string]any, keepalive, booleans bool) map[string]any {
	if keepalive {
		if value, present := req["KeepaliveConfiguration"]; present {
			members, failure := f.validatedKeepalive(value, present)
			if failure != nil {
				return failure
			}
			delete(stored, "KeepaliveConfiguration")
			if len(members) > 0 {
				stored["KeepaliveConfiguration"] = members
			}
		}
	}
	if booleans {
		for _, name := range []string{"AutoPause", "AutoResume"} {
			value, present := req[name]
			if !present {
				// The real service may echo a stored false even when the
				// request omitted the member; the scenario must not read that
				// as a client-side default. An update that omits the member
				// keeps the stored value, so only a fresh tool echoes.
				if _, stored0 := stored[name]; f.fault == "echo-server-bools" && !stored0 {
					stored[name] = false
				}
				continue
			}
			enabled, _ := value.(bool)
			if f.fault == "ignore-false" {
				enabled = true
			}
			stored[name] = enabled
		}
	}
	for _, name := range []string{"ToolName", "ToolType", "Description", "Persistent", "DefaultTimeout",
		"NetworkConfiguration", "RoleArn", "Tags", "CustomConfiguration", "ComputerConfiguration"} {
		if value, present := req[name]; present {
			stored[name] = value
		}
	}
	stored["CreateTime"], stored["UpdateTime"] = sandboxFixtureTime, sandboxFixtureTime
	return nil
}

// applyInstanceConfiguration records only explicit instance members. Inheritance
// stays a read-time decision.
func (f *sandboxFixture) applyInstanceConfiguration(instance *sandboxFixtureInstance, req map[string]any, keepalive bool) map[string]any {
	if keepalive {
		if value, present := req["KeepaliveConfiguration"]; present {
			members, failure := f.validatedKeepalive(value, present)
			if failure != nil {
				return failure
			}
			instance.keepalive = members
		}
	}
	if f.fault == "drop-auto-flags" {
		return nil
	}
	for name, target := range map[string]**bool{"AutoPause": &instance.pause, "AutoResume": &instance.resume} {
		value, present := req[name]
		if !present {
			continue
		}
		enabled, _ := value.(bool)
		if f.fault == "ignore-false" {
			enabled = true
		}
		enabledCopy := enabled
		*target = &enabledCopy
	}
	return nil
}

var sandboxTimeoutPattern = regexp.MustCompile(`^([0-9]+)(s|m|h)$`)

// validatedKeepalive returns the accepted members or the service error response.
// Presence is separate from value so an explicit null is invalid instead of
// being read as "not sent".
func (f *sandboxFixture) validatedKeepalive(value any, present bool) (map[string]any, map[string]any) {
	invalid := func(message string) (map[string]any, map[string]any) {
		return nil, map[string]any{"Code": "InvalidParameter", "Message": message}
	}
	if !present {
		return nil, nil
	}
	configuration, ok := value.(map[string]any)
	if !ok {
		return invalid("KeepaliveConfiguration must be an object")
	}
	if f.fault == "accept-invalid" {
		return maps.Clone(configuration), nil
	}
	members := map[string]any{}
	for name := range configuration {
		if name != "KeepaliveMode" && name != "KeepaliveTimeout" {
			return invalid("unsupported keepalive member " + name)
		}
	}
	if raw, present := configuration["KeepaliveMode"]; present {
		mode, ok := raw.(string)
		if !ok || !slices.Contains([]string{"NONE", "REQUEST", "CONNECTION", "MIXED"}, mode) {
			return invalid("KeepaliveMode must be NONE, REQUEST, CONNECTION or MIXED")
		}
		members["KeepaliveMode"] = mode
	}
	if raw, present := configuration["KeepaliveTimeout"]; present {
		text, ok := raw.(string)
		if !ok {
			return invalid("KeepaliveTimeout must be a string with a unit")
		}
		match := sandboxTimeoutPattern.FindStringSubmatch(text)
		if match == nil {
			return invalid("KeepaliveTimeout must be an integer with a unit")
		}
		amount, err := strconv.Atoi(match[1])
		if err != nil {
			return invalid("KeepaliveTimeout must be an integer with a unit")
		}
		seconds := amount
		switch match[2] {
		case "m":
			seconds *= 60
		case "h":
			seconds *= 3600
		}
		if seconds < 300 || seconds > 1800 {
			return invalid("KeepaliveTimeout must be between 300s and 1800s")
		}
		if f.fault == "normalize-timeout" {
			text = strconv.Itoa(seconds)
		}
		members["KeepaliveTimeout"] = text
	}
	return members, nil
}

// storeLogConfiguration normalizes ContentFormat the way the service does: the
// omitted or empty value becomes raw, other values are lower-cased and checked.
func (f *sandboxFixture) storeLogConfiguration(tool, req map[string]any) map[string]any {
	value, present := req["LogConfiguration"]
	if !present || value == nil {
		return nil
	}
	configuration, ok := value.(map[string]any)
	if !ok {
		return map[string]any{"Code": "InvalidParameter", "Message": "LogConfiguration must be an object"}
	}
	stored := maps.Clone(configuration)
	if f.fault == "ignore-log-format" {
		delete(stored, "ContentFormat")
		tool["LogConfiguration"] = stored
		return nil
	}
	format := ""
	if raw, present := configuration["ContentFormat"]; present {
		text, ok := raw.(string)
		if !ok {
			return map[string]any{"Code": "InvalidParameter", "Message": "ContentFormat must be a string"}
		}
		if f.fault != "log-case" {
			text = strings.ToLower(strings.TrimSpace(text))
		}
		format = text
	}
	topic := volumeString(volumeNested(configuration, "CLSConfig"), "TopicId")
	// The service needs outbound access for the sidecar that pushes logs, so it
	// rejects a CLS topic on the sandbox network.
	if topic != "" && volumeString(volumeNested(req, "NetworkConfiguration"), "NetworkMode") == "SANDBOX" {
		return map[string]any{
			"Code": "InvalidParameterValue.CLSConfig",
			"Message": "LogConfiguration.CLSConfig cannot be used when NetworkMode is SANDBOX " +
				"(sidecar requires network access to push logs to CLS)",
		}
	}
	if f.fault != "accept-invalid-log" {
		if format != "" && format != "raw" && format != "auto" {
			return map[string]any{"Code": "InvalidParameter", "Message": "ContentFormat must be raw or auto"}
		}
		if format == "auto" && topic == "" {
			return map[string]any{"Code": "InvalidParameter.ContentFormatUnsupported", "Message": "auto requires a CLS topic"}
		}
	}
	switch {
	case format == "" && f.fault == "drop-raw-default":
		delete(stored, "ContentFormat")
	case format == "":
		stored["ContentFormat"] = "raw"
	default:
		stored["ContentFormat"] = format
	}
	tool["LogConfiguration"] = stored
	return nil
}

func (f *sandboxFixture) toolKeepalive(toolID string) (mode, timeout string) {
	return sandboxToolKeepalive(f.tools[toolID])
}

func sandboxToolKeepalive(tool map[string]any) (mode, timeout string) {
	if tool == nil {
		return "", ""
	}
	configuration := volumeNested(tool, "KeepaliveConfiguration")
	return volumeString(configuration, "KeepaliveMode"), volumeString(configuration, "KeepaliveTimeout")
}

// resolvedKeepalive follows instance value -> tool current value -> platform
// default, member by member.
func (f *sandboxFixture) resolvedKeepalive(instance *sandboxFixtureInstance) (mode, timeout string) {
	mode = volumeString(instance.keepalive, "KeepaliveMode")
	timeout = volumeString(instance.keepalive, "KeepaliveTimeout")
	toolMode, toolTimeout := f.toolKeepalive(instance.toolID)
	if mode == "" {
		mode = toolMode
	}
	if timeout == "" {
		timeout = toolTimeout
	}
	return mode, timeout
}

func (f *sandboxFixture) instanceObject(instance *sandboxFixtureInstance, created bool) map[string]any {
	tool := f.tools[instance.toolID]
	object := map[string]any{"InstanceId": instance.id, "ToolId": instance.toolID, "Status": instance.status}
	if tool != nil {
		object["ToolName"], object["ToolType"] = tool["ToolName"], tool["ToolType"]
	}
	object["CreateTime"] = sandboxFixtureTime
	mode, timeout := f.resolvedKeepalive(instance)
	pause, pauseSet := false, false
	if instance.pause != nil {
		pause, pauseSet = *instance.pause, true
	} else if tool != nil {
		pause, pauseSet = tool["AutoPause"].(bool)
	}
	resume, resumeSet := false, false
	if instance.resume != nil {
		resume, resumeSet = *instance.resume, true
	} else if tool != nil {
		resume, resumeSet = tool["AutoResume"].(bool)
	}
	if f.fault == "fabricate-default" {
		if mode == "" {
			mode = "REQUEST"
		}
		if timeout == "" {
			timeout = "10m"
		}
	}
	if mode == "" && timeout == "" && pause {
		// The platform default only applies to an instance that opts into
		// automatic pausing.
		mode, timeout = "REQUEST", "10m"
	}
	if mode != "" || timeout != "" {
		if mode == "" {
			mode = "REQUEST"
		}
		if timeout == "" {
			timeout = "10m"
		}
		object["KeepaliveConfiguration"] = map[string]any{"KeepaliveMode": mode, "KeepaliveTimeout": timeout}
	}
	if pauseSet {
		object["AutoPause"] = pause
	}
	if resumeSet {
		object["AutoResume"] = resume
	}
	if created {
		object["Status"] = instance.status
	}
	return object
}

func TestSandboxKeepaliveCandidate(t *testing.T) {
	binary := sandboxBuildCandidate(t)
	sandboxShortenWaits(t)
	for _, fault := range sandboxKeepaliveFaults {
		t.Run("fault="+fault, func(t *testing.T) {
			fixture := newSandboxFixture(t, fault)
			server, caFile := sandboxTLSServer(t, fixture)
			defer server.Close()
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{
				{Scenario: "sandbox.keepalive", Assertions: sandboxKeepaliveAssertions},
			}}
			results, err := patchtest.Run(t.Context(), plan, Registry, binary, sandboxEnv(t, server.URL, caFile))
			if (err != nil) != (fault != "") {
				t.Fatalf("fault %q: err=%v results=%+v", fault, err, results)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			// A failing fault stops the scenario early, so only the fault-free run
			// creates the full set. Every other run must still leave nothing
			// running and no tool behind.
			if fault == "" {
				if len(fixture.instances) != 7 {
					t.Fatalf("fixture instances=%d, want the 7 created by the scenario", len(fixture.instances))
				}
			}
			if fault != "retain-tool" && len(fixture.tools) != 0 {
				t.Fatalf("fixture tools=%d, want every created tool deleted", len(fixture.tools))
			}
			if fault != "retain-instance" {
				for id, instance := range fixture.instances {
					if instance.status != "STOPPED" {
						t.Fatalf("instance %s was not stopped: %s", id, instance.status)
					}
				}
			}
			if fault == "" {
				if len(results) != 1 || results[0].Cleanup != "pass" || results[0].Calls < 30 {
					t.Fatalf("insufficient execution: %+v", results)
				}
			}
		})
	}
}

// TestSandboxKeepaliveAcceptsServerEchoedFalseBools covers the live failure the
// old expectation caused: the service echoes a stored false for a member the
// request never sent, which is allowed. #172 only forbids the client inventing
// the member, and the request side stays covered by the control-plane tests.
func TestSandboxKeepaliveAcceptsServerEchoedFalseBools(t *testing.T) {
	binary := sandboxBuildCandidate(t)
	sandboxShortenWaits(t)
	fixture := newSandboxFixture(t, "echo-server-bools")
	server, caFile := sandboxTLSServer(t, fixture)
	defer server.Close()
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{
		{Scenario: "sandbox.keepalive", Assertions: sandboxKeepaliveAssertions},
	}}
	results, err := patchtest.Run(t.Context(), plan, Registry, binary, sandboxEnv(t, server.URL, caFile))
	if err != nil {
		t.Fatalf("server-echoed false booleans broke the scenario: %v results=%+v", err, results)
	}
	if len(results) != 1 || results[0].Status != "pass" || results[0].Cleanup != "pass" {
		t.Fatalf("results=%+v", results)
	}
}

// TestSandboxFixtureRejectsCLSOnSandboxNetwork keeps the offline fixture honest
// about the precondition the live service enforced: a CLS topic needs a network
// mode with egress, and the sandbox network must be rejected.
func TestSandboxFixtureRejectsCLSOnSandboxNetwork(t *testing.T) {
	fixture := newSandboxFixture(t, "")
	tool := map[string]any{}
	request := map[string]any{
		"NetworkConfiguration": map[string]any{"NetworkMode": "SANDBOX"},
		"LogConfiguration":     map[string]any{"CLSConfig": map[string]any{"TopicId": sandboxFixtureTopic}},
	}
	failure := fixture.storeLogConfiguration(tool, request)
	if failure == nil || failure["Code"] != "InvalidParameterValue.CLSConfig" {
		t.Fatalf("sandbox network with a CLS topic was accepted: %v", failure)
	}
	if _, present := tool["LogConfiguration"]; present {
		t.Fatal("a rejected request stored a log configuration")
	}

	request["NetworkConfiguration"] = map[string]any{"NetworkMode": "PUBLIC"}
	if failure := fixture.storeLogConfiguration(tool, request); failure != nil {
		t.Fatalf("public network with a CLS topic was rejected: %v", failure)
	}
	topic := volumeString(volumeNested(tool, "LogConfiguration", "CLSConfig"), "TopicId")
	if topic != sandboxFixtureTopic {
		t.Fatalf("stored topic = %q, want %q", topic, sandboxFixtureTopic)
	}
}

// sandboxCapturedStderr runs fn with os.Stderr redirected and returns what it
// wrote, so a test can assert on the diagnostics the live suite log carries.
func sandboxCapturedStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	previous := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = previous }()
	fn()
	os.Stderr = previous
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close writer: %v", closeErr)
	}
	captured, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read captured stderr: %v", readErr)
	}
	return string(captured)
}

// TestSandboxScenarioReportsCause keeps the suite log diagnosable: patchtest only
// reports "scenario X did not pass", so the wrapper has to name the cause.
func TestSandboxScenarioReportsCause(t *testing.T) {
	failure := errors.New("tool sdt-1 reached FAILED instead of ACTIVE")
	var returned error
	logged := sandboxCapturedStderr(t, func() {
		returned = sandboxScenario("sandbox.keepalive", func(*patchtest.Session) error {
			return failure
		})(nil)
	})
	if !errors.Is(returned, failure) {
		t.Fatalf("wrapper returned %v, want the scenario error", returned)
	}
	if !strings.Contains(logged, "sandbox.keepalive returned: tool sdt-1 reached FAILED instead of ACTIVE") {
		t.Fatalf("log %q does not name the scenario error", logged)
	}

	var panicked error
	logged = sandboxCapturedStderr(t, func() {
		panicked = sandboxScenario("sandbox.keepalive", func(*patchtest.Session) error {
			panic("nil failure object")
		})(nil)
	})
	if panicked == nil {
		t.Fatal("wrapper swallowed the panic")
	}
	if !strings.Contains(logged, "sandbox.keepalive panicked: nil failure object") {
		t.Fatalf("log %q does not name the panic", logged)
	}

	logged = sandboxCapturedStderr(t, func() {
		if err := sandboxScenario("sandbox.keepalive", func(*patchtest.Session) error {
			return nil
		})(nil); err != nil {
			t.Fatalf("passing scenario returned %v", err)
		}
	})
	if logged != "" {
		t.Fatalf("passing scenario logged %q", logged)
	}
}

// TestSandboxFailureDetailToleratesAbsentFailure covers the panic that hid the
// first live failure: a successful response carries no failure object.
func TestSandboxFailureDetailToleratesAbsentFailure(t *testing.T) {
	if got := sandboxFailureDetail(sandboxEnvelope{}, "ResourceId"); got != "" {
		t.Fatalf("sandboxFailureDetail = %q, want empty", got)
	}
}

// TestSandboxLogFixtureNamesEveryMissingVariable keeps the fixture gate
// actionable: the log must name each variable the environment has to provide.
func TestSandboxLogFixtureNamesEveryMissingVariable(t *testing.T) {
	t.Setenv("AGR_TEST_CLS_TOPIC_ID", "")
	t.Setenv("AGR_TEST_ROLE_ARN", "")
	logged := sandboxCapturedStderr(t, func() {
		if _, err := sandboxLogFixtureFromEnv(); err == nil {
			t.Fatal("missing fixtures did not fail the scenario")
		}
	})
	for _, name := range []string{"AGR_TEST_CLS_TOPIC_ID", "AGR_TEST_ROLE_ARN"} {
		if !strings.Contains(logged, name) {
			t.Fatalf("log %q does not name %s", logged, name)
		}
	}
}

// TestSandboxKeepaliveWaitsForDeferredToolConfiguration reproduces the live
// failure the readiness wait fixes: create and fork return before the service
// publishes the copied configuration, so an immediate readback has no members.
func TestSandboxKeepaliveWaitsForDeferredToolConfiguration(t *testing.T) {
	binary := sandboxBuildCandidate(t)
	sandboxShortenWaits(t)
	fixture := newSandboxFixture(t, "pending-tool-config")
	server, caFile := sandboxTLSServer(t, fixture)
	defer server.Close()
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{
		{Scenario: "sandbox.keepalive", Assertions: sandboxKeepaliveAssertions},
	}}
	results, err := patchtest.Run(t.Context(), plan, Registry, binary, sandboxEnv(t, server.URL, caFile))
	if err != nil {
		t.Fatalf("readiness wait did not absorb the deferred publish: %v results=%+v", err, results)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.tools) != 0 || len(fixture.deferred) != 0 || len(fixture.pending) != 0 {
		t.Fatalf("tools=%d deferred=%d pending=%d, want every tool published and deleted",
			len(fixture.tools), len(fixture.deferred), len(fixture.pending))
	}
}

func TestSandboxLogFormatCandidate(t *testing.T) {
	binary := sandboxBuildCandidate(t)
	sandboxShortenWaits(t)
	for _, fault := range sandboxLogFaults {
		t.Run("fault="+fault, func(t *testing.T) {
			sandboxLogEnv(t)
			fixture := newSandboxFixture(t, fault)
			server, caFile := sandboxTLSServer(t, fixture)
			defer server.Close()
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{
				{Scenario: "sandbox.log-format", Assertions: sandboxLogAssertions},
			}}
			results, err := patchtest.Run(t.Context(), plan, Registry, binary, sandboxEnv(t, server.URL, caFile))
			if (err != nil) != (fault != "") {
				t.Fatalf("fault %q: err=%v results=%+v", fault, err, results)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fault != "retain-tool" && len(fixture.tools) != 0 {
				t.Fatalf("owned tools leaked: %d", len(fixture.tools))
			}
			if fault == "" {
				if len(results) != 1 || results[0].Cleanup != "pass" || results[0].Calls < 15 {
					t.Fatalf("insufficient execution: %+v", results)
				}
			}
		})
	}
	for _, missing := range sandboxFixtureLogVars {
		t.Run("missing="+missing, func(t *testing.T) {
			sandboxLogEnv(t)
			t.Setenv(missing, "")
			fixture := newSandboxFixture(t, "")
			server, caFile := sandboxTLSServer(t, fixture)
			defer server.Close()
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{
				{Scenario: "sandbox.log-format", Assertions: sandboxLogAssertions},
			}}
			if _, err := patchtest.Run(t.Context(), plan, Registry, binary, sandboxEnv(t, server.URL, caFile)); err == nil {
				t.Fatalf("missing %s produced a passing run", missing)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.calls != 0 {
				t.Fatalf("scenario contacted the service without %s: %d calls", missing, fixture.calls)
			}
		})
	}
}

// sandboxShortenWaits keeps the assertion-real waits while making a cleanup
// fault fail in seconds instead of the production deadline.
func sandboxShortenWaits(t *testing.T) {
	t.Helper()
	previousGone, previousActive, previousPoll := sandboxGoneDeadline, sandboxToolActiveDeadline, sandboxPollInterval
	sandboxGoneDeadline, sandboxToolActiveDeadline, sandboxPollInterval = 2*time.Second, 2*time.Second, 10*time.Millisecond
	t.Cleanup(func() {
		sandboxGoneDeadline, sandboxToolActiveDeadline, sandboxPollInterval = previousGone, previousActive, previousPoll
	})
}
