// Package progress provides a lightweight terminal spinner for long-running
// operations. The spinner writes to stderr and is automatically disabled when
// stderr is not a TTY, --non-interactive is set, or -o json is active.
//
// Design inspired by AgentCore CLI's deploy/progress.ts:
//   - onProgress(step, status) callback pattern with start/success/error states
//   - 80ms Braille dot animation on \r overwrite
//   - --json mode: Nop() spinner (no output, stdout stays pure)
//   - cleanup() for safe teardown on interrupt/error
//   - Multi-step support via repeated Start()/Stop() calls
package progress

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// frames is the Braille dots spinner animation (10 frames, matching AgentCore).
var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner displays a terminal spinner with a message on stderr.
// It is safe for concurrent use. Use New() for interactive mode or
// Nop() for JSON/non-interactive mode.
type Spinner struct {
	w       io.Writer
	mu      sync.Mutex
	msg     string
	stop    chan struct{}
	stopped chan struct{}
	active  bool
	started bool // tracks whether Start was ever called
	noColor bool // when true, skip ANSI escape sequences
}

// New creates a new Spinner that writes to the given writer (typically stderr).
// If w is nil, the spinner is a no-op.
func New(w io.Writer) *Spinner {
	return &Spinner{w: w}
}

// Nop returns a no-op Spinner that does nothing.
// Use this when output is JSON or non-interactive (mirrors AgentCore's
// "onProgress = undefined" pattern).
func Nop() *Spinner {
	return &Spinner{w: nil}
}

// NewForCLI creates a Spinner appropriate for a CLI command context.
// It returns a real spinner only when all conditions are met:
//   - jsonOutput is false (text mode)
//   - nonInteractive is false
//   - w is not nil (stderr writer)
//   - isTTY is true (stderr is a terminal)
//
// When noColor is true, the spinner omits ANSI escape sequences (uses simple
// overwrite with spaces instead of \033[K). Otherwise returns Nop().
// This eliminates duplicate guard logic across commands.
func NewForCLI(w io.Writer, jsonOutput, nonInteractive, isTTY, noColor bool) *Spinner {
	if !jsonOutput && !nonInteractive && w != nil && isTTY {
		sp := New(w)
		sp.noColor = noColor
		return sp
	}
	return Nop()
}

// Start begins the spinner animation with the given message.
// Can be called multiple times for multi-step flows (each call resets the
// spinner to the new step, matching AgentCore's startStep() pattern).
func (s *Spinner) Start(msg string) {
	if s.w == nil {
		return
	}
	s.mu.Lock()
	if s.active {
		// Already running — stop the current animation before starting new one.
		s.mu.Unlock()
		s.stopInternal()
		s.mu.Lock()
	}
	s.msg = msg
	s.active = true
	s.started = true
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})
	s.mu.Unlock()

	go s.run()
}

// Stop stops the spinner and prints a final status line.
// status is typically "✓" (success) or "✗" (error).
// If Start was never called, Stop is a no-op (prevents orphan status lines).
// This matches AgentCore's endStep(status) pattern.
func (s *Spinner) Stop(status string, msg string) {
	if s.w == nil {
		return
	}
	s.mu.Lock()
	wasStarted := s.started
	s.mu.Unlock()
	if !wasStarted {
		return
	}
	s.stopInternal()
	// Print final status line.
	fmt.Fprintf(s.w, "%s %s\n", status, msg)
}

// Cleanup stops the spinner without printing a final status.
// Use in defer or signal handlers to prevent orphan spinner output.
// Matches AgentCore's cleanup() pattern.
func (s *Spinner) Cleanup() {
	if s.w == nil {
		return
	}
	s.stopInternal()
}

func (s *Spinner) stopInternal() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	s.mu.Unlock()

	close(s.stop)
	<-s.stopped
}

func (s *Spinner) run() {
	defer close(s.stopped)
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-s.stop:
			// Clear the spinner line before returning.
			s.clearLine()
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			s.clearLine()
			fmt.Fprintf(s.w, "\r%s %s", frames[i%len(frames)], msg)
			i++
		}
	}
}

// clearLine moves cursor to start and clears the line. When noColor is set,
// uses spaces padding instead of ANSI escape \033[K.
func (s *Spinner) clearLine() {
	if s.noColor {
		// Overwrite with spaces (80 chars should cover most lines).
		fmt.Fprintf(s.w, "\r%-80s\r", "")
	} else {
		fmt.Fprintf(s.w, "\r\033[K")
	}
}
