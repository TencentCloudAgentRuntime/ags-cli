//go:build preview

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// harnessFake is a stateful control plane for the Harness and ModelProvider
// Actions. Revisions publish after a configurable number of reads so --wait
// observes PUBLISHING before the final state.
type harnessFake struct {
	t            *testing.T
	mu           sync.Mutex
	calls        []fakeCall
	harness      map[string]any
	revisions    map[int64]map[string]any
	publishReads map[int64]int
	failPublish  map[int64]bool
	deleteReads  int
	deleteFails  bool
	providers    map[string]map[string]any
	inUse        bool
}

type fakeCall struct {
	Action  string
	Request map[string]any
}

func newHarnessFake(t *testing.T) *harnessFake {
	return &harnessFake{t: t, revisions: map[int64]map[string]any{}, publishReads: map[int64]int{}, failPublish: map[int64]bool{}, providers: map[string]map[string]any{}}
}

func (f *harnessFake) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, call := range f.calls {
		out = append(out, call.Action)
	}
	return out
}

func (f *harnessFake) last(action string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Action == action {
			return f.calls[i].Request
		}
	}
	return nil
}

func (f *harnessFake) seedHarness(revisions ...string) {
	f.harness = map[string]any{
		"HarnessId": "hns-test", "HarnessName": "agent", "Status": "ACTIVE", "StatusReason": "",
		"Spec":         map[string]any{"HarnessEngine": "pi", "HarnessEngineVersion": "1.3.0", "SystemPrompt": "draft prompt", "ModelProviders": []any{map[string]any{"ModelProviderId": "mp-a", "Models": []any{"hy3"}}}, "DefaultModel": map[string]any{"ModelProviderId": "mp-a", "Model": "hy3"}},
		"ResourceInfo": map[string]any{"ToolId": "sdt-test", "DeploymentId": "dpl-test"}, "Tags": []any{},
	}
	for i, status := range revisions {
		f.revisions[int64(i+1)] = f.revision(int64(i+1), status, "live prompt")
	}
}

func (f *harnessFake) revision(number int64, status, prompt string) map[string]any {
	return map[string]any{
		"HarnessId": "hns-test", "Revision": number, "HarnessRevisionName": fmt.Sprintf("v%d", number), "Status": status, "StatusReason": "",
		"Spec": map[string]any{"HarnessEngine": "pi", "HarnessEngineVersion": "1.3.0", "SystemPrompt": prompt,
			"ModelProviders": []any{map[string]any{"ModelProviderId": "mp-a", "Revision": 1, "Models": []any{"hy3"}}}, "DefaultModel": map[string]any{"ModelProviderId": "mp-a", "Revision": 1, "Model": "hy3"}},
	}
}

func (f *harnessFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action := r.Header.Get("X-TC-Action")
	var request map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&request); err != nil {
		f.t.Errorf("%s: %v", action, err)
	}
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{action, request})
	response, failure := f.handle(action, request)
	f.mu.Unlock()
	if failure != "" {
		code, message, _ := strings.Cut(failure, ":")
		response = map[string]any{"Error": map[string]any{"Code": code, "Message": message}}
	}
	response["RequestId"] = "req-" + action
	_ = json.NewEncoder(w).Encode(map[string]any{"Response": response})
}

func number(value any) int64 {
	n, _ := value.(json.Number).Int64()
	return n
}

func (f *harnessFake) handle(action string, req map[string]any) (map[string]any, string) {
	switch action {
	case "DescribeHarnessEngineList":
		return map[string]any{"HarnessEngineSet": []any{map[string]any{"HarnessEngine": "pi", "HarnessEngineVersion": []any{"1.3.0", "1.2.0"}}}}, ""
	case "CreateHarness":
		f.seedHarness()
		f.harness["HarnessName"] = req["HarnessName"]
		f.harness["ResourceInfo"] = map[string]any{}
		f.revisions[1] = f.revision(1, "PUBLISHING", "p")
		f.publishReads[1] = 1
		return map[string]any{"Harness": f.harness}, ""
	case "DescribeHarness":
		if f.harness == nil {
			return nil, "ResourceNotFound:harness not found"
		}
		if f.harness["Status"] == "DELETING" {
			if f.deleteReads == 0 {
				if f.deleteFails {
					f.harness["Status"], f.harness["StatusReason"] = "DELETE_FAILED", "deployment delete failed"
				} else {
					f.harness = nil
					return nil, "ResourceNotFound:harness not found"
				}
			}
			f.deleteReads--
		}
		return map[string]any{"Harness": f.harness}, ""
	case "ModifyHarness":
		spec := f.harness["Spec"].(map[string]any)
		for key, value := range req {
			if key != "HarnessId" && key != "Tags" && key != "HarnessName" {
				spec[key] = value
			}
		}
		return map[string]any{"Harness": f.harness}, ""
	case "DescribeHarnessRevision":
		n := number(req["Revision"])
		revision, ok := f.revisions[n]
		if !ok {
			return nil, "ResourceNotFound:revision not found"
		}
		if revision["Status"] == "PUBLISHING" {
			if f.publishReads[n] == 0 {
				if f.failPublish[n] {
					revision["Status"], revision["StatusReason"] = "PUBLISH_FAILED", "registry version not readable"
				} else {
					for _, other := range f.revisions {
						if other["Status"] == "PUBLISHED" {
							other["Status"] = "SUPERSEDED"
						}
					}
					revision["Status"] = "PUBLISHED"
					if f.harness != nil {
						f.harness["ResourceInfo"] = map[string]any{"ToolId": "sdt-test", "DeploymentId": "dpl-test"}
					}
				}
			} else {
				f.publishReads[n]--
			}
		}
		return map[string]any{"HarnessRevision": revision}, ""
	case "DescribeHarnessRevisionList":
		var set []any
		for n := int64(len(f.revisions)); n >= 1; n-- {
			set = append(set, f.revisions[n])
		}
		return map[string]any{"HarnessRevisionSet": set, "TotalCount": len(set)}, ""
	case "CreateHarnessRevision":
		n := int64(len(f.revisions) + 1)
		f.revisions[n] = f.revision(n, "UNPUBLISHED", f.harness["Spec"].(map[string]any)["SystemPrompt"].(string))
		return map[string]any{"HarnessRevision": f.revisions[n]}, ""
	case "PublishHarnessRevision":
		n := number(req["Revision"])
		if f.revisions[n]["Status"] == "PUBLISHED" {
			return nil, "InvalidParameter:revision is already published"
		}
		f.revisions[n]["Status"] = "PUBLISHING"
		f.publishReads[n] = 1
		return map[string]any{"HarnessRevision": f.revisions[n]}, ""
	case "DeleteHarness":
		f.harness["Status"] = "DELETING"
		f.deleteReads = 1
		return map[string]any{}, ""
	case "DescribeHarnessList":
		return map[string]any{"HarnessSet": []any{f.harness}, "TotalCount": 1}, ""
	case "CreateModelProvider":
		provider := map[string]any{"ModelProviderId": "mp-new", "ModelProviderName": req["ModelProviderName"], "Spec": map[string]any{"Source": req["Source"], "Models": req["Models"]}, "Tags": []any{}}
		f.providers["mp-new"] = provider
		return map[string]any{"ModelProvider": provider}, ""
	case "DeleteModelProvider":
		if f.inUse {
			return nil, "ResourceInUse:model provider is referenced"
		}
		return map[string]any{}, ""
	case "DescribeModelProviderList":
		return map[string]any{"ModelProviderSet": []any{}, "TotalCount": 0}, ""
	}
	f.t.Errorf("unexpected action %s", action)
	return map[string]any{}, ""
}

type harnessRun struct {
	Status  string
	Data    map[string]any
	Failure *struct {
		Code, Kind, Message, Hint, Fix string
		Details                        map[string]any
	}
}

func runHarness(t *testing.T, server *httptest.Server, args ...string) (harnessRun, string, error) {
	t.Helper()
	out, stderr, err := runSnapshotCLI(t, server, "", args...)
	var result harnessRun
	decoder := json.NewDecoder(strings.NewReader(string(out)))
	decoder.UseNumber()
	if decodeErr := decoder.Decode(&result); decodeErr != nil {
		t.Fatalf("%v: stdout is not one JSON envelope: %v\nstdout=%s\nstderr=%s", args, decodeErr, out, stderr)
	}
	return result, stderr, err
}

func failureCode(r harnessRun) string {
	if r.Failure == nil {
		return ""
	}
	return r.Failure.Code
}

func TestHarnessCreateWaitsForRevisionOne(t *testing.T) {
	fake := newHarnessFake(t)
	server := httptest.NewTLSServer(fake)
	defer server.Close()
	result, _, err := runHarness(t, server, "harness", "create", "--name", "agent", "--engine", "pi", "--engine-version", "1.3.0",
		"--model", "mp-a:hy3", "--skill", "MARKETPLACE/rec-1/rv-1", "--role-arn", "qcs::cam::uin/1:roleName/R", "--subnet-id", "subnet-1", "--security-group-id", "sg-1", "--tags", "env=test", "--wait")
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("create: %v %+v", err, result)
	}
	revision, _ := result.Data["Revision"].(map[string]any)
	if revision["Status"] != "PUBLISHED" {
		t.Fatalf("wait result = %v", result.Data)
	}
	req := fake.last("CreateHarness")
	want := `{"DefaultModel":{"Model":"hy3","ModelProviderId":"mp-a"},"HarnessEngine":"pi","HarnessEngineVersion":"1.3.0","HarnessName":"agent","InitializeConfiguration":{"SandboxTool":{"NetworkConfiguration":{"NetworkMode":"VPC","VpcConfig":{"SecurityGroupIds":["sg-1"],"SubnetIds":["subnet-1"]}},"RoleArn":"qcs::cam::uin/1:roleName/R"}},"ModelProviders":[{"ModelProviderId":"mp-a","Models":["hy3"]}],"Skills":[{"Source":{"Registry":{"RecordId":"rec-1","RegistryId":"MARKETPLACE","VersionId":"rv-1"}}}],"Tags":[{"Key":"env","Value":"test"}]}`
	if got, _ := json.Marshal(req); string(got) != want {
		t.Fatalf("request:\n got %s\nwant %s", got, want)
	}
}

func TestHarnessCreateValidationFailsBeforeAnyWrite(t *testing.T) {
	fake := newHarnessFake(t)
	server := httptest.NewTLSServer(fake)
	defer server.Close()
	base := []string{"harness", "create", "--name", "agent", "--engine", "pi"}
	for name, tc := range map[string]struct {
		args []string
		code string
	}{
		"missing engine version": {[]string{"--model", "mp-a:hy3", "--role-arn", "r"}, "MISSING_REQUIRED_FLAG"},
		"missing role":           {[]string{"--engine-version", "1.3.0", "--model", "mp-a:hy3"}, "MISSING_REQUIRED_FLAG"},
		"missing model":          {[]string{"--engine-version", "1.3.0", "--role-arn", "r"}, "MISSING_REQUIRED_FLAG"},
		"ambiguous default":      {[]string{"--engine-version", "1.3.0", "--role-arn", "r", "--model", "mp-a:x", "--model", "mp-a:y"}, "MISSING_REQUIRED_FLAG"},
		"bad model reference":    {[]string{"--engine-version", "1.3.0", "--role-arn", "r", "--model", "hy3"}, "INVALID_MODEL_REFERENCE"},
		"request plus shortcut":  {[]string{"--request", `{}`, "--model", "mp-a:hy3"}, "REQUEST_FLAG_CONFLICT"},
		"role conflict":          {[]string{"--engine-version", "1.3.0", "--model", "mp-a:hy3", "--role-arn", "a", "--initialize-configuration", `{"SandboxTool":{"RoleArn":"b"}}`}, "ROLE_ARN_CONFLICT"},
		"missing subnet":         {[]string{"--engine-version", "1.3.0", "--model", "mp-a:hy3", "--role-arn", "r", "--security-group-id", "sg-1"}, "MISSING_REQUIRED_FLAG"},
		"missing security group": {[]string{"--engine-version", "1.3.0", "--model", "mp-a:hy3", "--role-arn", "r", "--subnet-id", "subnet-1"}, "MISSING_REQUIRED_FLAG"},
		"subnet conflict":        {[]string{"--engine-version", "1.3.0", "--model", "mp-a:hy3", "--role-arn", "r", "--subnet-id", "subnet-1", "--security-group-id", "sg-1", "--initialize-configuration", `{"SandboxTool":{"NetworkConfiguration":{"VpcConfig":{"SubnetIds":["subnet-2"]}}}}`}, "NETWORK_CONFLICT"},
		"public network":         {[]string{"--engine-version", "1.3.0", "--model", "mp-a:hy3", "--role-arn", "r", "--subnet-id", "subnet-1", "--security-group-id", "sg-1", "--initialize-configuration", `{"SandboxTool":{"NetworkConfiguration":{"NetworkMode":"PUBLIC"}}}`}, "INVALID_NETWORK_MODE"},
	} {
		t.Run(name, func(t *testing.T) {
			result, _, err := runHarness(t, server, append(append([]string{}, base...), tc.args...)...)
			if err == nil || failureCode(result) != tc.code {
				t.Fatalf("code = %q, want %q (%+v)", failureCode(result), tc.code, result.Failure)
			}
		})
	}
	for _, action := range fake.actions() {
		if action == "CreateHarness" {
			t.Fatal("invalid input reached CreateHarness")
		}
	}
	// The missing version error lists the region's versions.
	result, _, _ := runHarness(t, server, "harness", "create", "--name", "a", "--engine", "pi", "--model", "mp-a:hy3", "--role-arn", "r")
	if result.Failure == nil || !strings.Contains(result.Failure.Hint, "1.3.0, 1.2.0") || result.Failure.Fix != "Add --engine-version 1.3.0" {
		t.Fatalf("version hint = %+v", result.Failure)
	}
}

func TestHarnessCreateDryRunSendsNothing(t *testing.T) {
	fake := newHarnessFake(t)
	server := httptest.NewTLSServer(fake)
	defer server.Close()
	result, _, err := runHarness(t, server, "harness", "create", "--name", "a", "--engine", "pi", "--engine-version", "1.3.0", "--model", "mp-a:hy3", "--role-arn", "r", "--subnet-id", "subnet-1", "--security-group-id", "sg-1", "--dry-run")
	if err != nil || result.Data["DryRun"] != true || result.Data["Request"] == nil {
		t.Fatalf("dry run: %v %+v", err, result)
	}
	if calls := fake.actions(); len(calls) != 0 {
		t.Fatalf("dry run sent %v", calls)
	}
}

func TestHarnessPublishSemantics(t *testing.T) {
	t.Run("already published is a no-op", func(t *testing.T) {
		fake := newHarnessFake(t)
		fake.seedHarness("PUBLISHED")
		server := httptest.NewTLSServer(fake)
		defer server.Close()
		result, _, err := runHarness(t, server, "harness", "revision", "publish", "hns-test", "1", "--wait")
		if err != nil || result.Data["AlreadyPublished"] != true {
			t.Fatalf("publish: %v %+v", err, result)
		}
		if fake.last("PublishHarnessRevision") != nil {
			t.Fatal("already published revision was sent to PublishHarnessRevision")
		}
	})
	t.Run("rollback supersedes the live revision", func(t *testing.T) {
		fake := newHarnessFake(t)
		fake.seedHarness("SUPERSEDED", "PUBLISHED")
		server := httptest.NewTLSServer(fake)
		defer server.Close()
		result, _, err := runHarness(t, server, "harness", "revision", "publish", "hns-test", "1", "--wait")
		revision, _ := result.Data["HarnessRevision"].(map[string]any)
		if err != nil || revision["Status"] != "PUBLISHED" || fake.revisions[2]["Status"] != "SUPERSEDED" {
			t.Fatalf("rollback: %v %+v", err, result)
		}
	})
	t.Run("publish failure exits non-zero with retry fix", func(t *testing.T) {
		fake := newHarnessFake(t)
		fake.seedHarness("PUBLISHED", "UNPUBLISHED")
		fake.failPublish[2] = true
		server := httptest.NewTLSServer(fake)
		defer server.Close()
		result, _, err := runHarness(t, server, "harness", "revision", "publish", "hns-test", "2", "--wait")
		if err == nil || failureCode(result) != "WAIT_FAILED" || result.Failure.Details["StatusReason"] != "registry version not readable" ||
			!strings.Contains(result.Failure.Hint, "agr harness revision publish hns-test 2 --wait") {
			t.Fatalf("failure: %v %+v", err, result.Failure)
		}
	})
	t.Run("revision create --publish keeps the number on failure", func(t *testing.T) {
		fake := newHarnessFake(t)
		fake.seedHarness("PUBLISHED")
		fake.failPublish[2] = true
		server := httptest.NewTLSServer(fake)
		defer server.Close()
		result, _, err := runHarness(t, server, "harness", "revision", "create", "hns-test", "--publish", "--wait")
		if err == nil || result.Failure == nil || result.Failure.Details["Revision"] != json.Number("2") || result.Failure.Details["Created"] != true ||
			result.Failure.Fix != "agr harness revision publish hns-test 2 --wait" {
			t.Fatalf("partial publish: %v %+v", err, result.Failure)
		}
	})
	t.Run("wait without publish is rejected", func(t *testing.T) {
		fake := newHarnessFake(t)
		fake.seedHarness("PUBLISHED")
		server := httptest.NewTLSServer(fake)
		defer server.Close()
		result, _, err := runHarness(t, server, "harness", "revision", "create", "hns-test", "--wait")
		if err == nil || failureCode(result) != "WAIT_REQUIRES_PUBLISH" || fake.last("CreateHarnessRevision") != nil {
			t.Fatalf("wait without publish: %v %+v", err, result.Failure)
		}
	})
}

func TestHarnessDelete(t *testing.T) {
	t.Run("non-interactive delete requires --yes", func(t *testing.T) {
		fake := newHarnessFake(t)
		fake.seedHarness("PUBLISHED")
		server := httptest.NewTLSServer(fake)
		defer server.Close()
		result, _, err := runHarness(t, server, "harness", "delete", "hns-test")
		if err == nil || failureCode(result) != "CONFIRMATION_REQUIRED" || len(fake.actions()) != 0 {
			t.Fatalf("delete without --yes: %v %+v calls=%v", err, result.Failure, fake.actions())
		}
	})
	t.Run("dry run lists kept resources", func(t *testing.T) {
		fake := newHarnessFake(t)
		fake.seedHarness("PUBLISHED")
		server := httptest.NewTLSServer(fake)
		defer server.Close()
		result, _, err := runHarness(t, server, "harness", "delete", "hns-test", "--dry-run")
		if err != nil || result.Data["DryRun"] != true || result.Data["KeptToolId"] != "sdt-test" || fake.last("DeleteHarness") != nil {
			t.Fatalf("dry run: %v %+v", err, result)
		}
	})
	t.Run("wait until gone", func(t *testing.T) {
		fake := newHarnessFake(t)
		fake.seedHarness("PUBLISHED")
		server := httptest.NewTLSServer(fake)
		defer server.Close()
		result, _, err := runHarness(t, server, "harness", "delete", "hns-test", "--yes", "--wait")
		if err != nil || result.Data["Status"] != "DELETED" {
			t.Fatalf("delete wait: %v %+v", err, result)
		}
	})
	t.Run("delete failure", func(t *testing.T) {
		fake := newHarnessFake(t)
		fake.seedHarness("PUBLISHED")
		fake.deleteFails = true
		server := httptest.NewTLSServer(fake)
		defer server.Close()
		result, _, err := runHarness(t, server, "harness", "delete", "hns-test", "--yes", "--wait")
		if err == nil || failureCode(result) != "WAIT_FAILED" || result.Failure.Details["StatusReason"] != "deployment delete failed" {
			t.Fatalf("delete failure: %v %+v", err, result.Failure)
		}
	})
}

func TestHarnessUpdateStatusAndDiff(t *testing.T) {
	fake := newHarnessFake(t)
	fake.seedHarness("PUBLISHED")
	server := httptest.NewTLSServer(fake)
	defer server.Close()

	if result, _, err := runHarness(t, server, "harness", "update", "hns-test"); err == nil || failureCode(result) != "NOTHING_TO_UPDATE" {
		t.Fatalf("empty update: %v %+v", err, result.Failure)
	}
	if _, _, err := runHarness(t, server, "harness", "update", "hns-test", "--skill", "", "--session-space-id", ""); err != nil {
		t.Fatal(err)
	}
	req := fake.last("ModifyHarness")
	if got, _ := json.Marshal(req); string(got) != `{"HarnessId":"hns-test","SessionConfiguration":{"SessionSpaceId":""},"Skills":[]}` {
		t.Fatalf("clear request = %s", got)
	}

	status, _, err := runHarness(t, server, "harness", "status", "hns-test")
	if err != nil || status.Data["DraftChanged"] != true || status.Data["Next"] != "agr harness revision create hns-test --publish --wait" {
		t.Fatalf("status: %v %+v", err, status.Data)
	}
	diff, _, err := runHarness(t, server, "harness", "revision", "diff", "hns-test", "live", "draft")
	if err != nil || diff.Data["Changed"] != true {
		t.Fatalf("diff: %v %+v", err, diff.Data)
	}
	changes, _ := diff.Data["Changes"].([]any)
	var paths []string
	for _, change := range changes {
		paths = append(paths, change.(map[string]any)["Path"].(string))
	}
	if strings.Join(paths, ",") != "SessionConfiguration,SystemPrompt" {
		t.Fatalf("diff paths = %v", paths)
	}
	if result, _, err := runHarness(t, server, "harness", "revision", "diff", "hns-test", "live", "bogus"); err == nil || failureCode(result) != "INVALID_REVISION_REF" {
		t.Fatalf("bad ref: %v %+v", err, result.Failure)
	}
}

func TestModelProviderShortcuts(t *testing.T) {
	fake := newHarnessFake(t)
	server := httptest.NewTLSServer(fake)
	defer server.Close()
	if _, _, err := runHarness(t, server, "model-provider", "create", "--name", "m", "--tokenhub-api-key-id", "key-1", "--model", "hy3", "--model", "vl:TEXT,IMAGE"); err != nil {
		t.Fatal(err)
	}
	want := `{"ModelProviderName":"m","Models":[{"ID":"hy3","Input":["TEXT"]},{"ID":"vl","Input":["TEXT","IMAGE"]}],"Source":{"TokenHub":{"ApiKeyId":"key-1"}}}`
	if got, _ := json.Marshal(fake.last("CreateModelProvider")); string(got) != want {
		t.Fatalf("request:\n got %s\nwant %s", got, want)
	}
	fake.mu.Lock()
	fake.inUse = true
	fake.mu.Unlock()
	result, _, err := runHarness(t, server, "mp", "delete", "mp-new", "--yes")
	if err == nil || failureCode(result) != "ResourceInUse" || !strings.Contains(result.Failure.Hint, "agr harness update") {
		t.Fatalf("in use: %v %+v", err, result.Failure)
	}
	if result, _, err := runHarness(t, server, "model-provider", "list", "--name", "a", "--name-like", "b"); err == nil || failureCode(result) != "FLAG_CONFLICT" {
		t.Fatalf("exclusive filters: %v %+v", err, result.Failure)
	}
	if result, _, err := runHarness(t, server, "model-provider", "list", "--type", "TOKENHUB", "--filters", `[{"Name":"type","Values":["TOKENHUB"]}]`); err == nil || failureCode(result) != "DUPLICATE_FILTER" {
		t.Fatalf("duplicate filter: %v %+v", err, result.Failure)
	}
	if _, _, err := runHarness(t, server, "model-provider", "list", "--type", "TOKENHUB", "--tag", "env=a", "--tag", "env=b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(fake.last("DescribeModelProviderList")["Filters"]); string(got) != `[{"Name":"type","Values":["TOKENHUB"]},{"Name":"tag:env","Values":["a","b"]}]` {
		t.Fatalf("filters = %s", got)
	}
}
