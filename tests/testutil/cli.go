// Package testutil contains live-test helpers for building and invoking the AGR
// CLI against isolated home directories.
package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// CLI wraps the built agr binary with test-specific environment and timeout
// settings.
type CLI struct {
	BinaryPath string
	Home       string
	Config     LiveConfig
	Timeout    time.Duration
	// ExtraEnv holds additional KEY=VALUE pairs appended to the subprocess
	// environment. Use this instead of os.Setenv to avoid race conditions
	// when tests run in parallel.
	ExtraEnv []string
}

// CommandResult captures one CLI process execution for assertions.
type CommandResult struct {
	Args     []string
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// Envelope mirrors the JSON envelope emitted by the CLI in live tests.
type Envelope struct {
	SchemaVersion string         `json:"SchemaVersion"`
	Command       string         `json:"Command"`
	Status        string         `json:"Status"`
	Data          map[string]any `json:"Data"`
	Failure       *Failure       `json:"Failure"`
	Warnings      []string       `json:"Warnings"`
	Meta          map[string]any `json:"Meta"`
}

// Failure mirrors the structured failure object inside an envelope.
type Failure struct {
	Code    string `json:"Code"`
	Kind    string `json:"Kind"`
	Message string `json:"Message"`
	Hint    string `json:"Hint"`
}

// NewCLI creates an isolated CLI runner using the suite's built binary.
func NewCLI() *CLI {
	s := State()
	home := os.TempDir()
	if temp, err := os.MkdirTemp("", "ags-cli-home-*"); err == nil {
		home = temp
	}
	return &CLI{
		BinaryPath: s.BinaryPath,
		Home:       home,
		Config:     s.Config,
		Timeout:    s.Config.CommandTimeout,
	}
}

// Cleanup removes the runner's temporary home directory.
func (c *CLI) Cleanup() {
	_ = os.RemoveAll(c.Home)
}

// InitConfig runs "agr init" for the runner's isolated home directory.
func (c *CLI) InitConfig() Envelope {
	result := c.Run(context.Background(),
		"--output", "json",
		"init",
		"--secret-id", c.Config.SecretID,
		"--secret-key", c.Config.SecretKey,
		"--overwrite",
	)
	result.ExpectExit(0)
	env := result.Envelope()
	gomega.ExpectWithOffset(1, env.Command).To(gomega.Equal("init"))
	gomega.ExpectWithOffset(1, env.Status).To(gomega.Equal("succeeded"))
	return env
}

// Run executes the agr binary with the runner's environment and captures output.
func (c *CLI) Run(ctx context.Context, args ...string) CommandResult {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, c.BinaryPath, args...)
	cmd.Env = c.env()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if runCtx.Err() == context.DeadlineExceeded {
			exitCode = -1
			err = fmt.Errorf("command timed out after %s: %w", timeout, runCtx.Err())
		}
	}

	return CommandResult{
		Args:     append([]string(nil), args...),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Err:      err,
	}
}

func (c *CLI) env() []string {
	env := make([]string, 0, len(os.Environ())+10)
	for _, item := range os.Environ() {
		key := item
		if idx := strings.IndexByte(item, '='); idx >= 0 {
			key = item[:idx]
		}
		switch {
		case key == "HOME", key == "USERPROFILE":
			continue
		case strings.HasPrefix(key, "AGR_"):
			continue
		}
		env = append(env, item)
	}
	env = append(env, "HOME="+c.Home, "USERPROFILE="+c.Home)
	env = append(env, "TENCENTCLOUD_SECRET_ID="+c.Config.SecretID, "TENCENTCLOUD_SECRET_KEY="+c.Config.SecretKey)
	env = append(env, "AGR_REGION="+c.Config.Region)
	if c.Config.Domain != "" {
		env = append(env, "AGR_DOMAIN="+c.Config.Domain)
	}
	if c.Config.CloudEndpoint != "" {
		env = append(env, "AGR_CLOUD_ENDPOINT="+c.Config.CloudEndpoint)
	}
	env = append(env, c.ExtraEnv...)
	return env
}

// ExpectExit asserts the command exit code and includes redacted diagnostics on
// failure.
func (r CommandResult) ExpectExit(code int) {
	gomega.ExpectWithOffset(1, r.ExitCode).To(gomega.Equal(code), r.Diagnostics())
}

// ExpectSuccess asserts a zero exit code.
func (r CommandResult) ExpectSuccess() {
	r.ExpectExit(0)
}

// Envelope decodes stdout as an AGR JSON envelope.
func (r CommandResult) Envelope() Envelope {
	var env Envelope
	decoder := json.NewDecoder(strings.NewReader(r.Stdout))
	decoder.UseNumber()
	gomega.ExpectWithOffset(1, decoder.Decode(&env)).To(gomega.Succeed(), r.Diagnostics())
	gomega.ExpectWithOffset(1, env.SchemaVersion).To(gomega.Equal("agr.v1"), r.Diagnostics())
	return env
}

// Diagnostics returns redacted command output useful for assertion failures.
func (r CommandResult) Diagnostics() string {
	return fmt.Sprintf("args: %s\nexit: %d\nerr: %v\nstdout:\n%s\nstderr:\n%s",
		strings.Join(redactArgs(r.Args), " "), r.ExitCode, r.Err, r.Stdout, r.Stderr)
}

func redactArgs(args []string) []string {
	redacted := append([]string(nil), args...)
	for i := range redacted {
		switch {
		case redacted[i] == "--secret-id" || redacted[i] == "--secret-key":
			if i+1 < len(redacted) {
				redacted[i+1] = "<redacted>"
			}
		case strings.HasPrefix(redacted[i], "--secret-id="):
			redacted[i] = "--secret-id=<redacted>"
		case strings.HasPrefix(redacted[i], "--secret-key="):
			redacted[i] = "--secret-key=<redacted>"
		}
	}
	return redacted
}

// StringField reads a string-like field from JSON data and fails the test when
// the field is absent.
func StringField(data map[string]any, key string) string {
	value, ok := data[key]
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "missing JSON field %s in %#v", key, data)
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

// NumberField reads a numeric field from JSON data and fails the test when the
// field is absent or non-numeric.
func NumberField(data map[string]any, key string) int {
	value, ok := data[key]
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "missing JSON field %s in %#v", key, data)
	switch typed := value.(type) {
	case json.Number:
		n, err := typed.Int64()
		gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())
		return int(n)
	case float64:
		return int(typed)
	default:
		ginkgo.Fail(fmt.Sprintf("field %s is not numeric: %#v", key, value))
		return 0
	}
}

// ItemsField reads the standard Items array from list-command JSON data.
func ItemsField(data map[string]any) []any {
	value, ok := data["Items"]
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "missing Items field in %#v", data)
	items, ok := value.([]any)
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "Items is not an array: %#v", value)
	return items
}
