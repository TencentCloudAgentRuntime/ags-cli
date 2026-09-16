// Package output defines AGR's machine-readable envelope, exit-code taxonomy,
// and canonical data shapes shared by text and JSON renderers.
package output

// InstanceData is the canonical JSON shape for a sandbox instance.
type InstanceData struct {
	InstanceId     string  `json:"InstanceId"`
	ToolId         string  `json:"ToolId"`
	ToolName       string  `json:"ToolName"`
	Status         string  `json:"Status"`
	CreateTime     string  `json:"CreateTime"`
	UpdateTime     string  `json:"UpdateTime,omitempty"`
	TimeoutSeconds *uint64 `json:"TimeoutSeconds,omitempty"`
	ExpiresAt      string  `json:"ExpiresAt,omitempty"`
	StopReason     string  `json:"StopReason,omitempty"`
	Endpoints      any     `json:"Endpoints,omitempty"`
	MountOptions   any     `json:"MountOptions,omitempty"`
}

// InstanceListData is the canonical JSON shape for instance list responses.
type InstanceListData struct {
	Items      []InstanceData  `json:"Items"`
	Pagination *PaginationData `json:"Pagination"`
}

// PaginationData describes pagination metadata shared by list commands.
type PaginationData struct {
	Offset     int     `json:"Offset"`
	Limit      int     `json:"Limit"`
	Total      int     `json:"Total"`
	NextCursor *string `json:"NextCursor"`
}

// CodeRunData is the canonical JSON shape for code execution results.
type CodeRunData struct {
	Stdout           string `json:"Stdout"`
	Stderr           string `json:"Stderr"`
	Results          any    `json:"Results"`
	Error            any    `json:"Error"`
	ExecutionCount   int    `json:"ExecutionCount"`
	ExecutionContext any    `json:"ExecutionContext,omitempty"`
}

// ExecData is the canonical JSON shape for shell execution results.
type ExecData struct {
	Stdout           string `json:"Stdout"`
	Stderr           string `json:"Stderr"`
	ExitCode         int    `json:"ExitCode"`
	Error            any    `json:"Error"`
	ExecutionContext any    `json:"ExecutionContext,omitempty"`
}

// FileOpData is the canonical JSON shape for file operation results.
type FileOpData struct {
	Operation string `json:"Operation"`
	Path      string `json:"Path"`
	LocalPath string `json:"LocalPath,omitempty"`
	Size      int64  `json:"Size,omitempty"`
}

// BrowserData is the canonical JSON shape for browser session URLs.
type BrowserData struct {
	InstanceId string `json:"InstanceId"`
	VncUrl     string `json:"VncUrl"`
	CdpUrl     string `json:"CdpUrl"`
}

// DeleteData is the canonical JSON shape for delete summaries.
type DeleteData struct {
	Deleted   int      `json:"Deleted"`
	Failed    int      `json:"Failed"`
	FailedIds []string `json:"FailedIds,omitempty"`
}

// VersionData is the canonical JSON shape for version output.
type VersionData struct {
	Channel   string `json:"Channel"`
	Version   string `json:"Version"`
	Commit    string `json:"Commit"`
	BuildTime string `json:"BuildTime"`
}
