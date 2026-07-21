// Package iostreams centralizes stdin/stdout/stderr handles so command code can
// be tested without touching process-global file descriptors.
package iostreams

import (
	"bytes"
	"io"
	"os"

	"golang.org/x/term"
)

// IOStreams bundles the three standard file descriptors. Commands write
// through these instead of os.Stdout/os.Stderr directly, making output
// capturable in tests.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	isStdoutTTY bool
	isStdinTTY  bool
	isStderrTTY bool
}

// System returns an IOStreams connected to the real terminal.
func System() *IOStreams {
	return &IOStreams{
		In:          os.Stdin,
		Out:         os.Stdout,
		ErrOut:      os.Stderr,
		isStdoutTTY: term.IsTerminal(int(os.Stdout.Fd())),
		isStdinTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		isStderrTTY: term.IsTerminal(int(os.Stderr.Fd())),
	}
}

// Test returns an IOStreams backed by in-memory buffers, plus the buffers
// themselves for assertion. stdout and stderr are separate buffers.
func Test() (ios *IOStreams, stdin *bytes.Buffer, stdout *bytes.Buffer, stderr *bytes.Buffer) {
	stdin = &bytes.Buffer{}
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	return &IOStreams{
		In:          stdin,
		Out:         stdout,
		ErrOut:      stderr,
		isStdoutTTY: false,
		isStdinTTY:  false,
	}, stdin, stdout, stderr
}

// IsStdoutTTY reports whether stdout was detected or forced to be a terminal.
func (s *IOStreams) IsStdoutTTY() bool { return s.isStdoutTTY }

// IsStdinTTY reports whether stdin was detected or forced to be a terminal.
func (s *IOStreams) IsStdinTTY() bool { return s.isStdinTTY }

// IsStderrTTY reports whether stderr was detected or forced to be a terminal.
func (s *IOStreams) IsStderrTTY() bool { return s.isStderrTTY }

// SetStdoutTTY overrides TTY detection (for tests).
func (s *IOStreams) SetStdoutTTY(v bool) { s.isStdoutTTY = v }

// SetStdinTTY overrides stdin TTY detection (for tests).
func (s *IOStreams) SetStdinTTY(v bool) { s.isStdinTTY = v }

// SetStderrTTY overrides stderr TTY detection (for tests).
func (s *IOStreams) SetStderrTTY(v bool) { s.isStderrTTY = v }
