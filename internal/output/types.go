package output

import "time"

// Timing holds timing information for operations
type Timing struct {
	TotalMs  int64 `json:"total_ms"`
	CreateMs int64 `json:"create_ms,omitempty"` // Sandbox creation time
	ExecMs   int64 `json:"exec_ms,omitempty"`   // Execution time
}

// NewTiming creates a Timing from a duration
func NewTiming(d time.Duration) *Timing {
	return &Timing{TotalMs: d.Milliseconds()}
}

// NewTimingWithPhases creates a Timing with create and exec phases
func NewTimingWithPhases(total, create, exec time.Duration) *Timing {
	return &Timing{
		TotalMs:  total.Milliseconds(),
		CreateMs: create.Milliseconds(),
		ExecMs:   exec.Milliseconds(),
	}
}

// Pagination holds pagination information
type Pagination struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
	Total  int `json:"total"`
}

// ExecError represents an execution error
type ExecError struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Traceback string `json:"traceback,omitempty"`
}

// ExecResult represents a single code execution result
type ExecResult struct {
	Stdout     []string         `json:"stdout"`
	Stderr     []string         `json:"stderr"`
	Results    []map[string]any `json:"results,omitempty"`
	Error      *ExecError       `json:"error,omitempty"`
	InstanceID string           `json:"instance_id,omitempty"`
	Timing     *Timing          `json:"timing,omitempty"`
}

// TaskResult represents a single task in multi-task execution
type TaskResult struct {
	ID        int              `json:"id"`
	Source    string           `json:"source"`
	Instance  int              `json:"instance,omitempty"`
	TotalInst int              `json:"-"` // For text display only
	Stdout    []string         `json:"stdout"`
	Stderr    []string         `json:"stderr"`
	Results   []map[string]any `json:"results,omitempty"`
	Error     *ExecError       `json:"error,omitempty"`
	ErrorMsg  string           `json:"error_msg,omitempty"`
	Timing    *Timing          `json:"timing,omitempty"`
	Success   bool             `json:"success"`
}

// TaskSummary represents summary for multi-task execution
type TaskSummary struct {
	Total   int     `json:"total"`
	Success int     `json:"success"`
	Failed  int     `json:"failed"`
	Timing  *Timing `json:"timing,omitempty"`
}

// MultiTaskResult represents multiple task execution results
type MultiTaskResult struct {
	Tasks   []TaskResult `json:"tasks"`
	Summary TaskSummary  `json:"summary"`
}

// CommandResult represents shell command execution result
type CommandResult struct {
	Stdout   string  `json:"stdout"`
	Stderr   string  `json:"stderr,omitempty"`
	ExitCode int     `json:"exit_code"`
	Error    string  `json:"error,omitempty"`
	Timing   *Timing `json:"timing,omitempty"`
}

// ListResult represents a list with pagination
type ListResult struct {
	Items      []map[string]string `json:"items"`
	Pagination *Pagination         `json:"pagination,omitempty"`
	Timing     *Timing             `json:"timing,omitempty"`
}

// FileContent represents file content for cat command
type FileContent struct {
	Path    string  `json:"path"`
	Content string  `json:"content"`
	Size    int64   `json:"size"`
	Timing  *Timing `json:"timing,omitempty"`
}

// FileOperation represents a file operation result
type FileOperation struct {
	Operation string  `json:"operation"` // upload, download, remove, mkdir
	Path      string  `json:"path"`
	LocalPath string  `json:"local_path,omitempty"`
	Size      int64   `json:"size,omitempty"`
	Timing    *Timing `json:"timing,omitempty"`
}

// OperationResult represents a general operation result
type OperationResult struct {
	Status  string         `json:"status"` // success, error
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
	Timing  *Timing        `json:"timing,omitempty"`
}

// KeyValue represents an ordered key-value pair
type KeyValue struct {
	Key   string
	Value string
}
