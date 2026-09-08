// Command patch-e2e produces local, commit-bound evidence, not a trusted CI check.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchcoverage"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchscenarios"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/patchtest"
)

const entrypointVersion = "patch-e2e/v1"

// Set only when building the worker from a committed archive. An ordinary
// go run/build produces a launcher, which rebuilds the worker before testing.
var runnerCommit string

type Report struct {
	Version     string             `json:"entrypoint_version"`
	Repository  string             `json:"repository"`
	PR          int                `json:"pr"`
	Head        string             `json:"head_sha"`
	Base        string             `json:"base_sha"`
	Source      string             `json:"source_commit"`
	Tree        string             `json:"source_tree"`
	Environment string             `json:"environment"`
	Started     time.Time          `json:"started"`
	Finished    time.Time          `json:"finished"`
	Status      string             `json:"status"`
	Reason      string             `json:"reason,omitempty"`
	Plan        patchcoverage.Plan `json:"plan"`
	Results     []patchtest.Result `json:"results"`
	Passed      int                `json:"pass"`
	Failed      int                `json:"fail"`
	Skipped     int                `json:"skip"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "patch-e2e:", err)
		os.Exit(1)
	}
}

func git(ctx context.Context, args ...string) (string, error) {
	b, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git identity check failed")
	}
	return strings.TrimSpace(string(b)), nil
}

func run(ctx context.Context, args []string, out, diagnostics io.Writer) (retErr error) {
	r := Report{Version: entrypointVersion, Started: time.Now().UTC(), Status: "fail"}
	f := flag.NewFlagSet("patch-e2e", flag.ContinueOnError)
	f.StringVar(&r.Repository, "repository", "", "public owner/repository")
	f.IntVar(&r.PR, "pr", 0, "pull request number")
	f.StringVar(&r.Head, "head", "", "exact PR head SHA; must equal checked-out HEAD")
	f.StringVar(&r.Base, "base", "", "exact PR base SHA")
	f.StringVar(&r.Environment, "environment", "", "non-sensitive test environment alias")
	if err := f.Parse(args); err != nil {
		return err
	}
	defer func() {
		r.Finished = time.Now().UTC()
		if retErr != nil {
			r.Status = "fail"
			r.Reason = retErr.Error()
		}
		// Contract values are review material, not public execution evidence.
		r.Plan.Entries = nil
		data, err := patchtest.EncodeReport(r)
		if err == nil {
			_, err = fmt.Fprintln(out, string(data))
		}
		if err != nil {
			retErr = fmt.Errorf("write evidence report failed")
		}
	}()
	sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
	if f.NArg() != 0 || !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(r.Repository) || r.PR <= 0 || !sha.MatchString(r.Head) || !sha.MatchString(r.Base) || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(r.Environment) {
		return fmt.Errorf("repository, pr, full head/base SHAs and a safe environment alias are required")
	}
	root, err := git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	cwd, err := os.Stat(".")
	if err != nil {
		return fmt.Errorf("cannot determine working directory")
	}
	repoRoot, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("cannot determine repository root")
	}
	// macOS temporary directories can have both logical and physical paths.
	// Compare directory identity so aliases work without accepting subdirectories.
	if !os.SameFile(cwd, repoRoot) {
		return fmt.Errorf("run from the repository root")
	}
	if err := clean(ctx, r.Head); err != nil {
		return err
	}
	if runnerCommit != "" && runnerCommit != r.Head {
		return fmt.Errorf("runner commit does not match requested head; rebuild the launcher")
	}
	r.Source = r.Head
	r.Tree, err = git(ctx, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return err
	}
	base, err := git(ctx, "rev-parse", "--verify", r.Base+"^{commit}")
	if err != nil || base != r.Base {
		return fmt.Errorf("base commit unavailable locally")
	}
	if runnerCommit == "" {
		tmp, err := os.MkdirTemp("", "agr-patch-runner-")
		if err != nil {
			return fmt.Errorf("create runner directory failed")
		}
		defer func() {
			if os.RemoveAll(tmp) != nil {
				retErr = fmt.Errorf("runner artifact cleanup failed")
			}
		}()
		if err := extractCommit(ctx, r.Source, tmp); err != nil {
			return err
		}
		binary := filepath.Join(tmp, "patch-e2e-worker")
		// The archive has no Git metadata; identity is bound explicitly above.
		build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-mod=readonly", "-ldflags=-X main.runnerCommit="+r.Source, "-o", binary, "./cmd/internal/patch-e2e")
		build.Dir = tmp
		build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
		build.Stderr = diagnostics
		if build.Run() != nil {
			return fmt.Errorf("committed runner build failed")
		}
		worker := exec.CommandContext(ctx, binary, args...)
		worker.Stderr = diagnostics
		// Forward cancellation so the worker can run resource cleanup before
		// a bounded forced termination, rather than killing it immediately.
		worker.Cancel = func() error { return worker.Process.Signal(os.Interrupt) }
		worker.WaitDelay = 3 * time.Minute
		data, runErr := worker.Output()
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("committed runner did not produce a valid report")
		}
		if runErr != nil {
			return fmt.Errorf("committed runner failed: %s", r.Reason)
		}
		return clean(ctx, r.Head)
	}
	r.Plan, err = patchcoverage.Load(root, patchscenarios.Registry.Coverage(), true)
	if err != nil {
		return fmt.Errorf("coverage validation failed; run apipatch coverage for details")
	}
	if len(r.Plan.Entries) == 0 {
		r.Status = "not_applicable"
		return nil
	}
	live := false
	for _, b := range r.Plan.Bindings {
		if b.Scenario != "" {
			live = true
		}
	}
	if !live {
		r.Status = "not_applicable"
		return nil
	}
	if strings.TrimSpace(os.Getenv("TENCENTCLOUD_SECRET_ID")) == "" || strings.TrimSpace(os.Getenv("TENCENTCLOUD_SECRET_KEY")) == "" || strings.TrimSpace(os.Getenv("AGR_REGION")) == "" {
		return fmt.Errorf("explicit test credentials and AGR_REGION are required")
	}
	// Build only tracked committed source in a temporary extraction; ignored
	// local Go files or concurrent edits cannot change the candidate binary.
	tmp, err := os.MkdirTemp("", "agr-patch-e2e-")
	if err != nil {
		return fmt.Errorf("create build directory failed")
	}
	defer func() {
		if os.RemoveAll(tmp) != nil {
			retErr = fmt.Errorf("local artifact cleanup failed")
		}
	}()
	if err := extractCommit(ctx, r.Source, tmp); err != nil {
		return err
	}
	binary := filepath.Join(tmp, "agr-candidate")
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-mod=readonly", "-o", binary, "./cmd/agr")
	build.Dir = tmp
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	build.Stderr = diagnostics
	if build.Run() != nil {
		return fmt.Errorf("candidate CLI build failed")
	}
	home := filepath.Join(tmp, "test-home")
	if os.Mkdir(home, 0700) != nil {
		return fmt.Errorf("create isolated CLI home failed")
	}
	env := []string{"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH")}
	for _, key := range []string{"TENCENTCLOUD_SECRET_ID", "TENCENTCLOUD_SECRET_KEY", "TENCENTCLOUD_TOKEN", "AGR_REGION", "AGR_CLOUD_ENDPOINT", "AGR_DOMAIN"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	r.Results, err = patchtest.Run(ctx, r.Plan, patchscenarios.Registry, binary, env)
	for _, result := range r.Results {
		switch result.Status {
		case "pass":
			r.Passed++
		case "skip":
			r.Skipped++
		default:
			r.Failed++
		}
	}
	if err != nil {
		return fmt.Errorf("live scenarios failed; inspect status and cleanup records")
	}
	if err := clean(ctx, r.Head); err != nil {
		return err
	}
	r.Status = "pass"
	return nil
}

func extractCommit(ctx context.Context, commit, dir string) error {
	archive := exec.CommandContext(ctx, "git", "archive", "--format=tar", commit)
	reader, err := archive.StdoutPipe()
	if err != nil {
		return fmt.Errorf("archive source failed")
	}
	if err := archive.Start(); err != nil {
		return fmt.Errorf("archive source failed")
	}
	extract := exec.CommandContext(ctx, "tar", "-xf", "-", "-C", dir)
	extract.Stdin = reader
	extractErr := extract.Run()
	_ = reader.Close()
	archiveErr := archive.Wait()
	if extractErr != nil || archiveErr != nil {
		return fmt.Errorf("extract committed source failed")
	}
	return nil
}

func clean(ctx context.Context, head string) error {
	current, err := git(ctx, "rev-parse", "HEAD")
	if err != nil || current != head {
		return fmt.Errorf("head SHA does not match checked-out source")
	}
	status, err := git(ctx, "status", "--porcelain", "--untracked-files=all")
	if err != nil || status != "" {
		return fmt.Errorf("a clean worktree including untracked files is required")
	}
	return nil
}
