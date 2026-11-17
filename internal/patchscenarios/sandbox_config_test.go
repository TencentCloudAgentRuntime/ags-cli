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
	"retain-instance", "retain-tool", "invalid-request-internal",
}

var sandboxLogFaults = []string{
	"",
	"ignore-log-format", "drop-raw-default", "log-case", "accept-invalid-log",
	"retain-tool", "fail-tool-provision",
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
		if failure := sandboxValidateCustomConfiguration(req); failure != nil {
			delete(f.tools, id)
			response["Error"] = failure
			break
		}
		f.tools[id] = stored
		if f.fault == "fail-tool-provision" {
			// The service can accept a create and then fail provisioning, for
			// example when it cannot assume the role that delivers the logs.
			stored["Status"] = "FAILED"
			stored["StatusReason"] = "fixture: assume the log delivery role failed"
		}
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
		if f.fault == "invalid-request-internal" {
			// A parameter-level rejection that surfaces as an internal error is not
			// evidence of validation: the scenario must fail instead of counting
			// any failure as a rejected value.
			if value, present := req["KeepaliveConfiguration"]; present {
				if _, failure := f.validatedKeepalive(value, present); failure != nil {
					fail("InternalError.Unknown", "Internal server error")
					break
				}
			}
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
	// The service accepts a CLS topic only on custom tools and rejects it on the
	// built-in types before the topic itself is validated.
	if topic != "" && volumeString(req, "ToolType") != "custom" {
		return map[string]any{
			"Code":    "InvalidParameter",
			"Message": "LogConfiguration with CLSConfig is only supported for custom sandbox type for now",
		}
	}
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
			return map[string]any{"Code": "InvalidParameter", "Message": "LogConfiguration.CLSConfig.TopicId is required when ContentFormat is auto"}
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

// TestRegistryAssertionsAreBoundInManifest guards the live suite contract: the
// live specs require the executed assertion set to equal the set the coverage
// manifest binds to the scenario. A registered assertion that no binding names
// therefore fails a live run even when the backend behaved correctly, so the
// mismatch has to fail offline instead. Only the scenarios this patch binds are
// checked; other scenarios in the shared registry belong to other manifests.
func TestRegistryAssertionsAreBoundInManifest(t *testing.T) {
	plan, err := patchcoverage.Load("../..", Registry.Coverage(), true)
	if err != nil {
		t.Fatalf("load coverage plan: %v", err)
	}
	bound := map[string]map[string]bool{}
	for _, binding := range plan.Bindings {
		if bound[binding.Scenario] == nil {
			bound[binding.Scenario] = map[string]bool{}
		}
		for _, assertion := range binding.Assertions {
			bound[binding.Scenario][assertion] = true
		}
	}
	for _, want := range []struct {
		scenario   string
		assertions []string
	}{
		{"sandbox.keepalive", sandboxKeepaliveAssertions},
		{"sandbox.log-format", sandboxLogAssertions},
	} {
		for _, assertion := range want.assertions {
			if !bound[want.scenario][assertion] {
				t.Errorf("assertion %s/%s is not bound in e2e-coverage.yaml", want.scenario, assertion)
			}
		}
	}
}

// TestBehaviorProseIsBoundToAssertions keeps behavior-bearing prose out of the
// review-only escape hatch. For the members this patch adds, a document
// obligation must name a scenario and assertions: "the behavior is covered by
// the referenced scenario" is true only when a scenario actually references it.
func TestBehaviorProseIsBoundToAssertions(t *testing.T) {
	plan, err := patchcoverage.Load("../..", Registry.Coverage(), true)
	if err != nil {
		t.Fatalf("load coverage plan: %v", err)
	}
	bound := map[string]patchcoverage.Binding{}
	for _, binding := range plan.Bindings {
		bound[binding.Entry] = binding
	}
	checked := 0
	for _, entry := range plan.Entries {
		if !strings.HasSuffix(entry.ID, "/document") {
			continue
		}
		if !sandboxBehaviorMember(entry.ID) {
			continue
		}
		checked++
		binding, ok := bound[entry.ID]
		if !ok || binding.Scenario == "" || len(binding.Assertions) == 0 {
			t.Errorf("%s describes behavior but is bound only for review", entry.ID)
		}
	}
	if checked == 0 {
		t.Fatal("no behavior prose was checked, so this guard proves nothing")
	}
}

// sandboxBehaviorMember reports whether an obligation belongs to a member whose
// prose states behavior this patch has to observe.
func sandboxBehaviorMember(id string) bool {
	for _, member := range []string{"KeepaliveConfiguration", "AutoPause", "AutoResume", "ContentFormat"} {
		if strings.Contains(id, member) {
			return true
		}
	}
	return false
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
		"ToolType":             "custom",
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

// The log scenario must create custom tools: the service rejects a CLS topic on
// the built-in types, so a scenario that still sent -t code-interpreter would
// fail live for a reason the offline fixture now reproduces.
// sandboxValidateCustomConfiguration mirrors the live rule that a custom tool
// must name its start command: without it the service answers MissingParameter
// instead of creating the tool.
func sandboxValidateCustomConfiguration(req map[string]any) map[string]any {
	custom := sandboxObject(req["CustomConfiguration"])
	if custom == nil {
		return nil
	}
	if command, _ := custom["Command"].([]any); len(command) == 0 {
		return map[string]any{"Code": "MissingParameter", "Message": "usage: CustomConfiguration.Command is required"}
	}
	return nil
}

func TestSandboxFixtureRejectsCLSOnBuiltinToolType(t *testing.T) {
	fixture := newSandboxFixture(t, "")
	tool := map[string]any{}
	request := map[string]any{
		"ToolType":             "code-interpreter",
		"NetworkConfiguration": map[string]any{"NetworkMode": "PUBLIC"},
		"LogConfiguration":     map[string]any{"CLSConfig": map[string]any{"TopicId": sandboxFixtureTopic}},
	}
	failure := fixture.storeLogConfiguration(tool, request)
	message, _ := failure["Message"].(string)
	if failure == nil || failure["Code"] != "InvalidParameter" ||
		!strings.Contains(message, "only supported for custom sandbox type") {
		t.Fatalf("built-in tool type with a CLS topic was accepted: %v", failure)
	}
	if _, present := tool["LogConfiguration"]; present {
		t.Fatal("a rejected request stored a log configuration")
	}
	request["ToolType"] = sandboxLogTool.toolType
	if failure := fixture.storeLogConfiguration(tool, request); failure != nil {
		t.Fatalf("custom tool type with a CLS topic was rejected: %v", failure)
	}
}

// The log scenario's custom tool must carry a usable image and registry type;
// the service needs both to bring a custom tool up.
func TestSandboxCustomConfigurationIsToolReady(t *testing.T) {
	var configuration map[string]any
	if err := json.Unmarshal([]byte(sandboxCustomConfiguration), &configuration); err != nil {
		t.Fatalf("custom configuration is not JSON: %v", err)
	}
	for _, name := range []string{"Image", "ImageRegistryType"} {
		if value, _ := configuration[name].(string); value == "" {
			t.Fatalf("custom configuration is missing %s: %v", name, configuration)
		}
	}
	// Two live observations fix the shape: omitting Command is a MissingParameter
	// error, and a custom configuration the service accepts was observed to end at
	// FAILED with FailedOperation.DependencyUnavailable, naming no further cause.
	// The scenario therefore pins the whole known-working shape - the agent
	// started through a shell, its health endpoint declared as the probe and as a
	// port - like the custom tools already running here.
	command, _ := configuration["Command"].([]any)
	if len(command) != 3 || command[0] != "/bin/bash" || command[1] != "-c" || command[2] != "/usr/bin/envd" {
		t.Fatalf("custom configuration must start the sandbox agent, got %v", configuration["Command"])
	}
	if _, ok := configuration["Args"]; ok {
		t.Fatalf("custom configuration passes args next to the shell command: %v", configuration)
	}
	if value, _ := configuration["ImageRegistryType"].(string); value == "system" {
		t.Fatalf("custom configuration uses an unpublished registry type: %v", configuration)
	}
	ports, _ := configuration["Ports"].([]any)
	if len(ports) != 1 {
		t.Fatalf("custom configuration must declare the agent port: %v", configuration["Ports"])
	}
	port, _ := ports[0].(map[string]any)
	probe, _ := configuration["Probe"].(map[string]any)
	health, _ := probe["HttpGet"].(map[string]any)
	if port["Port"] != health["Port"] || health["Path"] != "/health" {
		t.Fatalf("the agent port and the probe health check disagree: port=%v probe=%v", port, health)
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

// TestSandboxFailedToolDiagnosticNamesTheReason keeps the live diagnostic
// honest: a tool that stops at FAILED must report the service's StatusReason and
// the preconditions the scenario chose, because the report itself carries no raw
// response and "reached FAILED" alone cannot be triaged.
func TestSandboxFailedToolDiagnosticNamesTheReason(t *testing.T) {
	binary := sandboxBuildCandidate(t)
	sandboxShortenWaits(t)
	sandboxLogEnv(t)
	fixture := newSandboxFixture(t, "fail-tool-provision")
	server, caFile := sandboxTLSServer(t, fixture)
	defer server.Close()
	plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{
		{Scenario: "sandbox.log-format", Assertions: sandboxLogAssertions},
	}}
	var (
		results []patchtest.Result
		runErr  error
	)
	logged := sandboxCapturedStderr(t, func() {
		results, runErr = patchtest.Run(t.Context(), plan, Registry, binary, sandboxEnv(t, server.URL, caFile))
	})
	if runErr == nil {
		t.Fatalf("a tool that failed provisioning produced a passing run: %+v", results)
	}
	for _, want := range []string{
		`reached FAILED`,
		`reason="fixture: assume the log delivery role failed"`,
		`ToolType="custom"`,
		`logContentFormat=`,
		`image="ccr.ccs.tencentyun.com/ags-image/envd:v0.6.13"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("diagnostic log %q does not carry %q", logged, want)
		}
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.tools) != 0 {
		t.Fatalf("owned tools leaked after the failed wait: %d", len(fixture.tools))
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

// TestSandboxFixtureRequiresCustomStartCommand keeps the fixture honest about
// the second live rejection: a custom tool without CustomConfiguration.Command
// is answered with MissingParameter, so the scenario cannot silently drop the
// member that makes the sandbox agent start.
func TestSandboxFixtureRequiresCustomStartCommand(t *testing.T) {
	request := map[string]any{
		"ToolType":            "custom",
		"CustomConfiguration": map[string]any{"Image": "ccr.ccs.tencentyun.com/ags-image/envd:v0.6.13"},
	}
	failure := sandboxValidateCustomConfiguration(request)
	if failure == nil || failure["Code"] != "MissingParameter" {
		t.Fatalf("a custom tool without a start command was accepted: %v", failure)
	}
	request["CustomConfiguration"] = map[string]any{"Image": "ccr.ccs.tencentyun.com/ags-image/envd:v0.6.13", "Command": []any{"/usr/bin/envd"}}
	if failure := sandboxValidateCustomConfiguration(request); failure != nil {
		t.Fatalf("a custom tool with the agent command was rejected: %v", failure)
	}
}

// TestSandboxKeepaliveInvalidRequestsAreAllRejected pins the invalid-value matrix
// itself: an explicit null object or null member must be asserted as rejected,
// not treated as a no-op, and every entry must be a value the contract rejects.
func TestSandboxKeepaliveInvalidRequestsAreAllRejected(t *testing.T) {
	fixture := newSandboxFixture(t, "")
	nullShapes := 0
	for _, request := range sandboxKeepaliveInvalidRequests() {
		value, present := request["KeepaliveConfiguration"]
		if len(request) != 1 || !present {
			t.Fatalf("invalid request %v must carry exactly KeepaliveConfiguration", request)
		}
		if _, failure := fixture.validatedKeepalive(value, present); failure == nil {
			t.Fatalf("invalid request %v is accepted by the contract", request)
		}
		if value == nil {
			nullShapes++
			continue
		}
		members, _ := value.(map[string]any)
		for _, member := range members {
			if member == nil {
				nullShapes++
			}
		}
	}
	if nullShapes != 3 {
		t.Fatalf("null shapes = %d, want the null object and both null members", nullShapes)
	}
}

// TestSandboxForkFailureStillCleansUpTheReturnedTool drives both keepalive fork
// call sites against a fake CLI that fails with a resource ID. A fork that
// creates a tool and then fails must still delete it and confirm the deletion,
// so the scenario cannot leave a copy behind when it reports the failure.
func TestSandboxForkFailureStillCleansUpTheReturnedTool(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra []string
	}{
		{name: "copy fork"},
		{name: "override fork", extra: []string{"--keepalive-configuration", `{"KeepaliveTimeout":"1800s"}`, "--auto-pause=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "calls.log")
			binary := filepath.Join(t.TempDir(), "fake-agr")
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$CALL_LOG"
case "$*" in
  "tool fork "*)
    printf '%s\n' '{"Status":"failed","Failure":{"Code":"WAIT_TIMEOUT","Kind":"timeout","Message":"tool did not become ACTIVE","Details":{"ResourceId":"tool-fork-leaked"}}}'
    exit 1
    ;;
  "tool delete "*)
    : > "$CALL_LOG.deleted"
    printf '%s\n' '{"Status":"succeeded","Data":{"ToolId":"tool-fork-leaked"}}'
    ;;
  "tool list "*)
    if [ -f "$CALL_LOG.deleted" ]; then
      printf '%s\n' '{"Status":"succeeded","Data":{"Items":[],"Pagination":{"Total":0}}}'
    else
      printf '%s\n' '{"Status":"succeeded","Data":{"Items":[{"ToolId":"tool-fork-leaked","Status":"CREATING"}],"Pagination":{"Total":1}}}'
    fi
    ;;
  *)
    printf '%s\n' '{"Status":"failed","Failure":{"Code":"UnexpectedCall","Kind":"internal","Message":"unexpected fake CLI call"}}'
    exit 1
    ;;
esac
`
			if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			registry := patchtest.Registry{
				"fork-failure-cleanup": {
					Assertions: []string{"fork.failed"},
					Run: sandboxScenario("fork-failure-cleanup", func(s *patchtest.Session) error {
						_, err := sandboxForkTool(s, s.Context, "tool-source", "copy", tc.extra...)
						return s.Assert("fork.failed", err != nil)
					}),
				},
			}
			plan := patchcoverage.Plan{Bindings: []patchcoverage.Binding{
				{Scenario: "fork-failure-cleanup", Assertions: []string{"fork.failed"}},
			}}
			results, err := patchtest.Run(t.Context(), plan, registry, binary, []string{"CALL_LOG=" + logPath})
			if err != nil {
				t.Fatalf("the failing fork must still be a reported run: %v; results=%+v", err, results)
			}
			if len(results) != 1 || results[0].Status != "pass" || results[0].Cleanup != "pass" {
				t.Fatalf("results = %+v, want one passing scenario with a passing cleanup", results)
			}
			log, logErr := os.ReadFile(logPath)
			if logErr != nil {
				t.Fatal(logErr)
			}
			if !strings.Contains(string(log), "tool fork tool-source -n copy "+strings.Join(tc.extra, " ")) {
				t.Fatalf("the scenario did not fork the source tool as declared:\n%s", log)
			}
			if !strings.Contains(string(log), "tool delete tool-fork-leaked") {
				t.Fatalf("the tool returned with the failure was not deleted:\n%s", log)
			}
			if !strings.Contains(string(log), "tool list --tool-ids tool-fork-leaked") {
				t.Fatalf("the deletion was not confirmed with an independent query:\n%s", log)
			}
		})
	}
}

// sandboxTestFailure builds the failure object a CLI envelope carries.
func sandboxTestFailure(code, message string) *struct {
	Code, Kind, Message string
	Details             map[string]any
} {
	return &struct {
		Code, Kind, Message string
		Details             map[string]any
	}{Code: code, Message: message}
}

// TestSandboxParameterRejectionIgnoresServerFailures keeps "the call failed" from
// standing in for "the service rejected the value": only a parameter-validation
// answer is evidence, an internal or credential failure is not.
func TestSandboxParameterRejectionIgnoresServerFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		want       bool
	}{
		{"value rejection", "InvalidParameterValue.CLSConfig", true},
		{"range rejection", "InvalidParameter", true},
		{"missing member", "MissingParameter", true},
		{"internal error", "InternalError.Unknown", false},
		{"credential failure", "AuthFailure.SignatureFailure", false},
	} {
		envelope := sandboxEnvelope{Status: "failed", Failure: sandboxTestFailure(tc.code, "message")}
		if got := sandboxParameterRejection(errors.New("call failed"), envelope); got != tc.want {
			t.Errorf("%s: %s counted as a parameter rejection=%v, want %v", tc.name, tc.code, got, tc.want)
		}
	}
	if sandboxParameterRejection(nil, sandboxEnvelope{Status: "succeeded"}) {
		t.Error("a successful call counted as a rejection")
	}
	if sandboxParameterRejection(errors.New("transport failure"), sandboxEnvelope{}) {
		t.Error("a failure without a failure envelope counted as a rejection")
	}
}
