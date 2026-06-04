package testutil

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

var state *SuiteState

// SuiteState holds shared live-test resources such as the built binary, SDK
// client, and resources created for cleanup.
type SuiteState struct {
	BinaryPath string
	BuildDir   string
	Config     LiveConfig
	sdk        *ags.Client
	toolID     string
	createdIDs []string
}

// RunSpecs wires Gomega into Ginkgo and runs the named suite.
func RunSpecs(t *testing.T, description string) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, description)
}

// SetupSuite loads live-test configuration, builds the CLI binary, and creates
// the shared SDK client.
func SetupSuite() {
	cfg := LoadConfig()
	if missing := cfg.Missing(); len(missing) > 0 {
		ginkgo.Skip(fmt.Sprintf("live AGR tests require Tencent Cloud credentials from environment or ~/.agr/config.toml; missing %v", missing))
	}

	repoRoot, err := FindRepoRoot()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	buildDir, err := os.MkdirTemp("", "ags-cli-live-tests-*")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	binaryName := "agr"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(buildDir, binaryName)

	cmd := buildCLIBinaryCommand(binaryPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	gomega.Expect(cmd.Run()).To(gomega.Succeed(), "go build failed\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())

	sdk, err := newSDKClient(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	state = &SuiteState{BinaryPath: binaryPath, BuildDir: buildDir, Config: cfg, sdk: sdk}
	ginkgo.GinkgoWriter.Printf("AGR live tests initialized: binary=%s region=%s\n", binaryPath, cfg.Region)
}

func buildCLIBinaryCommand(binaryPath string) *exec.Cmd {
	return exec.Command("go", "build", "-buildvcs=false", "-o", binaryPath, "./cmd/agr")
}

// CleanupSuite removes test-created tools and the temporary binary directory
// unless the live-test config asks to keep them.
func CleanupSuite() {
	if state == nil {
		return
	}
	if !state.Config.KeepResources {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for i := len(state.createdIDs) - 1; i >= 0; i-- {
			id := state.createdIDs[i]
			req := ags.NewDeleteSandboxToolRequest()
			req.ToolId = &id
			if _, err := state.sdk.DeleteSandboxToolWithContext(ctx, req); err != nil {
				ginkgo.GinkgoWriter.Printf("Warning: failed to delete test tool %s: %v\n", id, err)
			}
		}
	}
	if state.BuildDir != "" && !state.Config.KeepBinary {
		if err := os.RemoveAll(state.BuildDir); err != nil {
			ginkgo.GinkgoWriter.Printf("Warning: failed to remove test build dir %s: %v\n", state.BuildDir, err)
		}
	}
}

// State returns the initialized suite state and fails the current spec when the
// suite was not set up.
func State() *SuiteState {
	gomega.ExpectWithOffset(1, state).NotTo(gomega.BeNil(), "testutil.SetupSuite must run before tests")
	return state
}

// FindRepoRoot walks upward from the current working directory until it finds
// the repository go.mod.
func FindRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", wd)
		}
	}
}

func newSDKClient(cfg LiveConfig) (*ags.Client, error) {
	credential := common.NewCredential(cfg.SecretID, cfg.SecretKey)
	cpf := profile.NewClientProfile()
	if cfg.CloudEndpoint != "" {
		cpf.HttpProfile.Endpoint = cfg.CloudEndpoint
	} else {
		cpf.HttpProfile.Endpoint = "ags.tencentcloudapi.com"
	}
	return ags.NewClient(credential, cfg.Region, cpf)
}

// EnsureToolID returns a configured test tool or creates one for the suite.
func (s *SuiteState) EnsureToolID(ctx context.Context) string {
	if s.Config.ToolID != "" {
		return s.Config.ToolID
	}
	if s.toolID != "" {
		return s.toolID
	}

	name := fmt.Sprintf("agr-live-test-%d", time.Now().UnixNano())
	toolType := "code-interpreter"
	networkMode := "PUBLIC"
	timeout := "5m"
	req := ags.NewCreateSandboxToolRequest()
	req.ToolName = &name
	req.ToolType = &toolType
	req.DefaultTimeout = &timeout
	req.NetworkConfiguration = &ags.NetworkConfiguration{NetworkMode: &networkMode}
	resp, err := s.sdk.CreateSandboxToolWithContext(ctx, req)
	gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())
	gomega.ExpectWithOffset(1, resp.Response).NotTo(gomega.BeNil())
	gomega.ExpectWithOffset(1, resp.Response.ToolId).NotTo(gomega.BeNil())
	s.toolID = *resp.Response.ToolId
	s.createdIDs = append(s.createdIDs, s.toolID)
	s.WaitForToolStatus(ctx, s.toolID, "ACTIVE")
	return s.toolID
}

// WaitForToolStatus polls the Cloud API until the tool reaches one of the
// expected statuses.
func (s *SuiteState) WaitForToolStatus(ctx context.Context, toolID string, statuses ...string) {
	allowed := map[string]bool{}
	for _, status := range statuses {
		allowed[status] = true
	}
	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) string {
		req := ags.NewDescribeSandboxToolListRequest()
		req.ToolIds = []*string{&toolID}
		resp, err := s.sdk.DescribeSandboxToolListWithContext(ctx, req)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(resp.Response).NotTo(gomega.BeNil())
		g.Expect(resp.Response.SandboxToolSet).NotTo(gomega.BeEmpty())
		status := ""
		if resp.Response.SandboxToolSet[0].Status != nil {
			status = *resp.Response.SandboxToolSet[0].Status
		}
		return status
	}, s.Config.EventuallyTimeout, 10*time.Second).Should(gomega.Satisfy(func(status string) bool {
		return allowed[status]
	}))
}

// ContainsAny reports whether value contains any of the provided substrings.
func ContainsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
