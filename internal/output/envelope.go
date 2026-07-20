package output

// Envelope is the unified JSON output for all commands using -o json.
type Envelope struct {
	SchemaVersion string      `json:"SchemaVersion"`
	Command       string      `json:"Command"`
	Status        string      `json:"Status"` // succeeded, failed, partial
	Data          interface{} `json:"Data"`
	Failure       *Failure    `json:"Failure"`
	Warnings      []string    `json:"Warnings"`
	Meta          *Meta       `json:"Meta"`
	ExitCodes     any         `json:"ExitCodes,omitempty"`
}

// Failure describes a CLI-level failure.
type Failure struct {
	Code      string         `json:"Code"`
	Kind      string         `json:"Kind"`
	Message   string         `json:"Message"`
	Hint      string         `json:"Hint,omitempty"`
	Fix       string         `json:"Fix,omitempty"`
	Retryable bool           `json:"Retryable"`
	Details   map[string]any `json:"Details,omitempty"`
}

// Meta holds envelope metadata.
type Meta struct {
	Backend    string         `json:"Backend"`
	DurationMs int64          `json:"DurationMs"`
	Effects    []Effect       `json:"Effects,omitempty"`
	Extra      map[string]any `json:"Extra,omitempty"`
}

// Effect describes a side effect of the command.
type Effect struct {
	Kind     string `json:"Kind"`
	Resource string `json:"Resource"`
	Id       string `json:"Id"`
}

// NewSuccessEnvelope creates a succeeded envelope.
func NewSuccessEnvelope(command string, data interface{}, backend string, durationMs int64) *Envelope {
	return &Envelope{
		SchemaVersion: "agr.v1",
		Command:       command,
		Status:        "succeeded",
		Data:          data,
		Failure:       nil,
		Warnings:      []string{},
		Meta: &Meta{
			Backend:    backend,
			DurationMs: durationMs,
		},
	}
}

// NewFailedEnvelope creates a failed envelope.
func NewFailedEnvelope(command string, failure *Failure, backend string, durationMs int64) *Envelope {
	return &Envelope{
		SchemaVersion: "agr.v1",
		Command:       command,
		Status:        "failed",
		Data:          nil,
		Failure:       failure,
		Warnings:      []string{},
		Meta: &Meta{
			Backend:    backend,
			DurationMs: durationMs,
		},
	}
}

// NewPartialEnvelope creates a partial success envelope.
func NewPartialEnvelope(command string, data interface{}, warnings []string, backend string, durationMs int64) *Envelope {
	return &Envelope{
		SchemaVersion: "agr.v1",
		Command:       command,
		Status:        "partial",
		Data:          data,
		Failure:       nil,
		Warnings:      warnings,
		Meta: &Meta{
			Backend:    backend,
			DurationMs: durationMs,
		},
	}
}
