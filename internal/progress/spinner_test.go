package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestNopSpinnerDoesNothing(t *testing.T) {
	sp := Nop()
	// Should not panic.
	sp.Start("hello")
	sp.Stop("✓", "done")
	sp.Cleanup()
}

func TestNopSpinnerWithNilWriter(t *testing.T) {
	sp := New(nil)
	// Should not panic.
	sp.Start("hello")
	sp.Stop("✓", "done")
	sp.Cleanup()
}

func TestSpinnerWritesFramesToWriter(t *testing.T) {
	var buf bytes.Buffer
	sp := New(&buf)
	sp.Start("Loading...")
	// Wait for at least 2 frames to render (80ms per frame; 300ms is ~3-4 frames).
	time.Sleep(300 * time.Millisecond)
	sp.Stop("✓", "Done")

	output := buf.String()
	// Should contain at least one spinner frame.
	hasFrame := false
	for _, f := range frames {
		if strings.Contains(output, f) {
			hasFrame = true
			break
		}
	}
	if !hasFrame {
		t.Fatalf("expected spinner frame in output, got: %q", output)
	}
	// Should contain the final status line.
	if !strings.Contains(output, "✓ Done") {
		t.Fatalf("expected final status '✓ Done' in output, got: %q", output)
	}
}

func TestSpinnerStopWithError(t *testing.T) {
	var buf bytes.Buffer
	sp := New(&buf)
	sp.Start("Creating...")
	time.Sleep(100 * time.Millisecond)
	sp.Stop("✗", "Failed")

	output := buf.String()
	if !strings.Contains(output, "✗ Failed") {
		t.Fatalf("expected '✗ Failed' in output, got: %q", output)
	}
}

func TestSpinnerCleanupDoesNotPrintStatus(t *testing.T) {
	var buf bytes.Buffer
	sp := New(&buf)
	sp.Start("Working...")
	time.Sleep(100 * time.Millisecond)
	sp.Cleanup()

	output := buf.String()
	// Cleanup should NOT print a status line (no ✓ or ✗).
	if strings.Contains(output, "✓") || strings.Contains(output, "✗") {
		t.Fatalf("cleanup should not print status, got: %q", output)
	}
}

func TestSpinnerMultiStep(t *testing.T) {
	var buf bytes.Buffer
	sp := New(&buf)

	// Step 1
	sp.Start("Step 1...")
	time.Sleep(100 * time.Millisecond)
	sp.Stop("✓", "Step 1 complete")

	// Step 2
	sp.Start("Step 2...")
	time.Sleep(100 * time.Millisecond)
	sp.Stop("✓", "Step 2 complete")

	output := buf.String()
	if !strings.Contains(output, "✓ Step 1 complete") {
		t.Fatalf("missing step 1 in output: %q", output)
	}
	if !strings.Contains(output, "✓ Step 2 complete") {
		t.Fatalf("missing step 2 in output: %q", output)
	}
}

func TestSpinnerDoubleStopDoesNotPanic(t *testing.T) {
	var buf bytes.Buffer
	sp := New(&buf)
	sp.Start("test")
	time.Sleep(100 * time.Millisecond)
	sp.Stop("✓", "first")
	// Second stop should not panic.
	sp.Stop("✓", "second")
}

func TestSpinnerStartOverwritesPreviousStep(t *testing.T) {
	var buf bytes.Buffer
	sp := New(&buf)
	sp.Start("First step")
	time.Sleep(100 * time.Millisecond)
	// Starting a new step without stopping should cleanly transition.
	sp.Start("Second step")
	time.Sleep(100 * time.Millisecond)
	sp.Stop("✓", "Done")

	output := buf.String()
	if !strings.Contains(output, "✓ Done") {
		t.Fatalf("expected final status in output: %q", output)
	}
}

func TestSpinnerMessage(t *testing.T) {
	var buf bytes.Buffer
	sp := New(&buf)
	sp.Start("Creating instance...")
	time.Sleep(150 * time.Millisecond)
	sp.Stop("✓", "Instance created")

	output := buf.String()
	if !strings.Contains(output, "Creating instance...") {
		t.Fatalf("expected message in spinner output: %q", output)
	}
}

func TestSpinnerStopWithoutStartIsNoop(t *testing.T) {
	var buf bytes.Buffer
	sp := New(&buf)
	// Stop without Start should not print anything or panic.
	sp.Stop("✓", "should not appear")

	output := buf.String()
	if output != "" {
		t.Fatalf("expected no output when Stop called without Start, got: %q", output)
	}
}

func TestSpinnerCleanupWithoutStartIsNoop(t *testing.T) {
	var buf bytes.Buffer
	sp := New(&buf)
	// Cleanup without Start should not panic.
	sp.Cleanup()

	output := buf.String()
	if output != "" {
		t.Fatalf("expected no output, got: %q", output)
	}
}
