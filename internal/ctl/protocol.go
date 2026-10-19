// Package ctl is the control channel between a running ct and programs that
// want to act on it: the `ct ctl` command, and through it the orchestrator
// agent. The TUI serves one Unix socket per ct process; a client sends one
// JSON request per connection and reads one JSON response.
package ctl

// Op names a request. Each maps to one `ct ctl` subcommand.
type Op string

const (
	OpList      Op = "list"
	OpSummary   Op = "summary"
	OpSpawn     Op = "spawn"
	OpSend      Op = "send"
	OpInterrupt Op = "interrupt"
	OpStop      Op = "stop"
	OpOpen      Op = "open"
	OpReopen    Op = "reopen"
	OpRemove    Op = "remove"
)

// Request is one command for the running ct. Worktree is the "repo/name" key
// ct uses everywhere; Repo and Name are only for spawn, which creates the
// worktree the key would point at.
type Request struct {
	Op Op `json:"op"`

	Worktree string `json:"worktree,omitempty"`

	Repo     string `json:"repo,omitempty"`
	Name     string `json:"name,omitempty"`
	Base     string `json:"base,omitempty"`
	Provider string `json:"provider,omitempty"`

	// Text is the prompt for spawn and the message for send.
	Text string `json:"text,omitempty"`
	// Lines caps how many commits a summary lists (0 = default).
	Lines int `json:"lines,omitempty"`
	// Force lets stop and reopen close a workspace whose agent is still busy.
	Force bool `json:"force,omitempty"`
}

// Response is the reply to a Request. OK is false when Error is set.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// Message is a one-line human outcome for the mutating ops.
	Message string `json:"message,omitempty"`

	Worktrees []WorktreeInfo `json:"worktrees,omitempty"`
	Summary   *Summary       `json:"summary,omitempty"`
}

// WorktreeInfo is one worktree as the deck sees it, plus its hosted agents.
type WorktreeInfo struct {
	Key    string `json:"key"`
	Repo   string `json:"repo"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Base   string `json:"base,omitempty"`

	// Open reports that ct hosts sessions for this worktree right now.
	Open   bool        `json:"open"`
	Agents []AgentInfo `json:"agents,omitempty"`
	// Queued counts messages waiting for the active agent to go idle.
	Queued int `json:"queued,omitempty"`

	Ahead   int    `json:"ahead"`
	Behind  int    `json:"behind"`
	Dirty   bool   `json:"dirty"`
	Added   int    `json:"added,omitempty"`
	Deleted int    `json:"deleted,omitempty"`
	Subject string `json:"subject,omitempty"`
}

// AgentInfo is one hosted agent and its live status.
type AgentInfo struct {
	Provider  string `json:"provider"`
	Label     string `json:"label"`
	SessionID string `json:"session_id,omitempty"`
	Pid       int    `json:"pid,omitempty"`
	// Status is "busy", "idle", "waiting", or "" when ct has no reading yet.
	Status     string `json:"status,omitempty"`
	WaitingFor string `json:"waiting_for,omitempty"`
	Active     bool   `json:"active"`
}

// Summary is the cheap view of one worktree's work: what git knows, plus the
// agent's last words, so an orchestrator can follow progress without reading
// whole transcripts.
type Summary struct {
	WorktreeInfo
	Commits     []string   `json:"commits,omitempty"`
	Files       []FileStat `json:"files,omitempty"`
	Uncommitted []FileStat `json:"uncommitted,omitempty"`
	LastMessage string     `json:"last_message,omitempty"`
	Queue       []string   `json:"queue,omitempty"`
}

// FileStat is one file's line counts in a diff.
type FileStat struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}
