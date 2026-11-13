//go:build preview

// Package prompt runs one ACP prompt turn against a harness from the terminal.
package prompt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/cli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/command"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/harness/get"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/acpclient"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/commands/internal/harnesscli"
	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

const defaultCwd = "/workspace"

// Module returns the prompt workflow.
func Module() command.Module {
	spec := command.Spec{
		ID:    "harness.prompt",
		Path:  []string{"harness", "prompt"},
		Use:   "prompt <harness-id> [text]",
		Short: "Send one prompt to a harness and stream the reply",
		Long: "Connect to the harness over ACP, send one prompt turn, and stream the agent's reply. Use it to smoke-test a published harness without another client.\n\n" +
			"Pass the prompt as an argument or '-' to read stdin. Text output streams the reply to stdout and tool progress to stderr; -o json returns one envelope with the reply, StopReason, SessionId and AffinityId; -o ndjson streams ACP session updates.\n\n" +
			"Each run is a new ACP connection. To continue a conversation, pass the returned --session-id and --affinity-id; without the affinity value the request may reach another instance, and without a session space an earlier session may not exist there. Tool permission requests are answered 'cancelled'. Ctrl+C sends session/cancel; an unfinished turn is never resent. Available in preview builds.",
		Examples: []string{
			"Example - Ask one question:\n  agr harness prompt hns-a1b2c3d4 'List the files in /workspace'",
			"Example - Continue the same conversation:\n  agr harness prompt hns-a1b2c3d4 'Remember cedar' -o json --jq '.Data | {SessionId, AffinityId}'\n  agr harness prompt hns-a1b2c3d4 'What did I ask you to remember?' --session-id <session-id> --affinity-id <affinity-id>",
			"Example - Prompt from a file and stream events:\n  agr harness prompt hns-a1b2c3d4 - -o ndjson < task.md",
		},
		SupportsJSON:   true,
		SupportsNDJSON: true,
		Args: []command.ArgSpec{
			{Name: "harness-id", Required: true, Description: "Harness ID, such as hns-a1b2c3d4."},
			{Name: "text", Description: "Prompt text, or - to read stdin."},
		},
		Flags: []command.FlagSpec{
			harnesscli.StringFlag("cwd", "Working directory inside the instance (default /workspace); must exist there"),
			harnesscli.StringFlag("session-id", "Continue an existing ACP session instead of creating one"),
			harnesscli.StringFlag("affinity-id", "Affinity value from an earlier run, to reach the same instance"),
			{Name: "timeout", Usage: "Maximum time for the turn, such as 5m (default 10m)", Type: command.FlagString, Workflow: true},
		},
		Output: command.OutputSpec{DataType: "HarnessPromptData", Description: "Reply text, stop reason, session and affinity identifiers.", Effects: []string{"read:harness", "read:deployment-token", "read:acp-session"}},
	}
	return command.Module{
		Descriptor: command.Descriptor{Spec: spec, Groups: harnesscli.GroupsOf(get.APIDescriptor(), 1), Source: command.SourceWorkflow},
		Build: func(deps command.Deps) (command.Runtime, error) {
			deps = deps.WithDefaults()
			client, err := harnesscli.NewClient(deps, spec.ID)
			if err != nil {
				return command.Runtime{}, err
			}
			return command.Runtime{Handler: command.HandlerFunc(func(ctx context.Context, req command.Request) (*command.Result, error) {
				return run(ctx, req, deps, client)
			})}, nil
		},
	}
}

func promptText(req command.Request) (string, error) {
	text, ok := req.ArgValues["text"]
	if !ok || strings.TrimSpace(text) == "" {
		return "", output.NewUsageError("MISSING_REQUIRED_ARG", "missing prompt text",
			"Pass the prompt as an argument, or '-' to read it from stdin: agr harness prompt <harness-id> 'Say hello'")
	}
	if text != "-" {
		return text, nil
	}
	if req.Stdin == nil {
		return "", output.NewUsageError("INVALID_PROMPT_INPUT", "stdin is not available", "Pass the prompt as an argument instead.")
	}
	data, err := io.ReadAll(io.LimitReader(req.Stdin, 4<<20))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", output.NewUsageError("INVALID_PROMPT_INPUT", "stdin was empty", "Pipe the prompt text into the command.")
	}
	return string(data), nil
}

func run(ctx context.Context, req command.Request, deps command.Deps, client harnesscli.Client) (*command.Result, error) {
	text, err := promptText(req)
	if err != nil {
		return nil, err
	}
	timeout := 10 * time.Minute
	if raw := harnesscli.String(req, "timeout"); raw != "" {
		if timeout, err = time.ParseDuration(raw); err != nil || timeout <= 0 {
			return nil, output.NewUsageError("INVALID_TIMEOUT", fmt.Sprintf("invalid --timeout %q", raw), "Use a positive Go duration such as 30s or 5m.")
		}
	}
	cwd := harnesscli.String(req, "cwd")
	if cwd == "" {
		cwd = defaultCwd
	}
	harnessID := req.ArgValues["harness-id"]
	endpoint, _, err := harnesscli.ResolveEndpoint(ctx, deps, client, harnessID, harnesscli.String(req, "affinity-id"))
	if err != nil {
		return nil, err
	}
	mode := renderText
	switch {
	case cli.IsNDJSON():
		mode = renderNDJSON
	case cli.IsJSONOutput():
		mode = renderNone
	}
	turnCtx, cancelTurn := context.WithTimeout(ctx, timeout)
	defer cancelTurn()
	signalCtx, stop := signal.NotifyContext(turnCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sink := &updates{mode: mode, out: deps.IO.Out, errOut: deps.IO.ErrOut, events: output.NewNDJSONWriter(deps.IO.Out, "harness.prompt")}
	session, err := acpclient.Connect(signalCtx, endpoint, sink)
	if err != nil {
		return nil, connectFailure(err, harnessID)
	}
	defer session.Close()

	sessionID := harnesscli.String(req, "session-id")
	if sessionID == "" {
		created, err := session.Conn.NewSession(signalCtx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
		if err != nil {
			return nil, rpcFailure("SESSION_CREATE_FAILED", session.Err(err), "Check that --cwd exists inside the instance; the default image provides /workspace.")
		}
		sessionID = string(created.SessionId)
	} else if err := restore(signalCtx, session, sessionID, cwd); err != nil {
		return nil, err
	}
	if mode == renderNDJSON {
		_ = sink.events.WriteStarted(map[string]any{"HarnessId": harnessID, "SessionId": sessionID})
	}
	sink.reset()
	started := time.Now()
	response, promptErr := session.Conn.Prompt(signalCtx, acp.PromptRequest{SessionId: acp.SessionId(sessionID), Prompt: []acp.ContentBlock{acp.TextBlock(text)}})
	if promptErr != nil && signalCtx.Err() != nil {
		// Interrupted or timed out: ask the agent to stop, but report the
		// outcome as unknown because the turn may still finish remotely.
		cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_ = session.Conn.Cancel(cancelCtx, acp.CancelNotification{SessionId: acp.SessionId(sessionID)})
		cancel()
	}
	data := map[string]any{
		"HarnessId":  harnessID,
		"SessionId":  sessionID,
		"AffinityId": session.Bridge.Affinity(),
		"Reply":      sink.reply(),
		"ToolCalls":  sink.toolCalls(),
		"DurationMs": time.Since(started).Milliseconds(),
	}
	if promptErr != nil {
		failure := rpcFailure("PROMPT_FAILED", session.Err(promptErr), "The turn outcome is unknown and was not resent. Inspect the session or send the next prompt explicitly.")
		if errors.Is(signalCtx.Err(), context.DeadlineExceeded) {
			failure = output.NewCLIError(&output.Failure{Code: "PROMPT_TIMEOUT", Kind: output.KindTimeout, Message: fmt.Sprintf("no reply within %s; session/cancel was sent", timeout), Hint: "Raise --timeout, or continue with --session-id and --affinity-id."})
		}
		failure.Failure.Details = data
		if mode == renderNDJSON {
			return sink.finishNDJSON(data, failure), nil
		}
		return nil, failure
	}
	data["StopReason"] = string(response.StopReason)
	return finish(mode, sink, data), nil
}

func restore(ctx context.Context, session *acpclient.Session, sessionID, cwd string) error {
	id := acp.SessionId(sessionID)
	var err error
	if session.Info.LoadSession {
		_, err = session.Conn.LoadSession(ctx, acp.LoadSessionRequest{SessionId: id, Cwd: cwd, McpServers: []acp.McpServer{}})
	} else {
		_, err = session.Conn.ResumeSession(ctx, acp.ResumeSessionRequest{SessionId: id, Cwd: cwd, McpServers: []acp.McpServer{}})
	}
	if err != nil {
		return rpcFailure("SESSION_RESTORE_FAILED", session.Err(err),
			"Pass the --affinity-id from the run that created the session, and the same --cwd. Without a session space, history exists only on the original instance.")
	}
	return nil
}

func finish(mode renderMode, sink *updates, data map[string]any) *command.Result {
	switch mode {
	case renderNDJSON:
		return sink.finishNDJSON(data, nil)
	case renderText:
		sink.endText()
		fmt.Fprintf(sink.errOut, "\n[%s] session %s", data["StopReason"], data["SessionId"])
		if affinity, _ := data["AffinityId"].(string); affinity != "" {
			fmt.Fprintf(sink.errOut, " affinity %s", affinity)
		}
		fmt.Fprintln(sink.errOut)
		return &command.Result{Data: data, Text: func(io.Writer) {}}
	}
	return &command.Result{Data: data}
}

func connectFailure(err error, harnessID string) error {
	return acpclient.Classify(err, fmt.Sprintf("The Deployment may still be starting an instance. Check 'agr harness endpoint %s --check'.", harnessID))
}

func rpcFailure(code string, err error, hint string) *output.CLIError {
	return output.NewCLIError(&output.Failure{Code: code, Kind: output.KindGenericError, Message: err.Error(), Hint: hint})
}

type renderMode int

const (
	renderText renderMode = iota
	renderNDJSON
	renderNone
)

// updates collects streamed reply text and tool activity from session/update.
type updates struct {
	mode   renderMode
	out    io.Writer
	errOut io.Writer
	events *output.NDJSONWriter

	mu      sync.Mutex
	text    strings.Builder
	tools   map[string]map[string]any
	order   []string
	pending bool
}

func (u *updates) reset() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.text.Reset()
	u.tools = map[string]map[string]any{}
	u.order = nil
}

func (u *updates) finishNDJSON(data map[string]any, failure *output.CLIError) *command.Result {
	u.mu.Lock()
	defer u.mu.Unlock()
	// Stop accepting updates before writing the terminal event. The SDK may
	// still drain queued notifications while the connection is closing.
	u.tools = nil
	if failure != nil {
		_ = u.events.WriteFailed(data, failure.Failure)
		return &command.Result{StreamDone: true, ExitCode: failure.ExitCode, Cause: failure}
	}
	_ = u.events.WriteCompleted(data)
	return &command.Result{StreamDone: true}
}

func (u *updates) SessionUpdate(_ context.Context, notification acp.SessionNotification) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.tools == nil {
		// Ignore replayed history before reset and late updates after finishing.
		return nil
	}
	if u.mode == renderNDJSON {
		_ = u.events.WriteEvent("update", acpclient.Raw(notification.Update))
	}
	update := notification.Update
	switch {
	case update.AgentMessageChunk != nil:
		chunk := acpclient.TextOf(update.AgentMessageChunk.Content)
		u.text.WriteString(chunk)
		if u.mode == renderText {
			fmt.Fprint(u.out, chunk)
			u.pending = !strings.HasSuffix(chunk, "\n")
		}
	case update.ToolCall != nil:
		call := update.ToolCall
		u.track(string(call.ToolCallId), map[string]any{"Title": call.Title, "Kind": string(call.Kind), "Status": string(call.Status)})
		u.progress(acpclient.ToolTitle(call.Title, call.Kind, &call.Status))
	case update.ToolCallUpdate != nil:
		call := update.ToolCallUpdate
		fields := map[string]any{}
		if call.Status != nil {
			fields["Status"] = string(*call.Status)
		}
		if call.Title != nil {
			fields["Title"] = *call.Title
		}
		u.track(string(call.ToolCallId), fields)
		if call.Status != nil && (*call.Status == acp.ToolCallStatusCompleted || *call.Status == acp.ToolCallStatusFailed) {
			title := ""
			if tool := u.tools[string(call.ToolCallId)]; tool != nil {
				title, _ = tool["Title"].(string)
			}
			u.progress(acpclient.ToolTitle(title, "", call.Status))
		}
	}
	return nil
}

func (u *updates) RequestPermission(_ context.Context, request acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if u.mode != renderNDJSON {
		fmt.Fprintf(u.errOut, "agr: declined a tool permission request (%s); prompt cannot approve tools\n", request.ToolCall.ToolCallId)
	}
	return acpclient.Cancelled(), nil
}

func (u *updates) track(id string, fields map[string]any) {
	tool, ok := u.tools[id]
	if !ok {
		tool = map[string]any{"ToolCallId": id}
		u.tools[id] = tool
		u.order = append(u.order, id)
	}
	for key, value := range fields {
		if value != "" {
			tool[key] = value
		}
	}
}

func (u *updates) progress(line string) {
	if u.mode != renderText {
		return
	}
	if u.pending {
		fmt.Fprintln(u.out)
		u.pending = false
	}
	fmt.Fprintf(u.errOut, "  ⋯ %s\n", line)
}

func (u *updates) endText() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.pending {
		fmt.Fprintln(u.out)
	}
}

func (u *updates) reply() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.text.String()
}

func (u *updates) toolCalls() []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]map[string]any, 0, len(u.order))
	for _, id := range u.order {
		out = append(out, u.tools[id])
	}
	return out
}
