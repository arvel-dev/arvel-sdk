// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"log/slog"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// Provider is what a vendor plugin implements. Every method that returns a
// *protocol.RPCError (built with protocol.NewError) reaches the host with that
// code; any other error becomes the Arvel error "provider_error" with its text
// redacted.
type Provider interface {
	// Initialize is called once. It must return in under 5 s and must not use
	// the network; slow checks belong in Status.
	Initialize(ctx context.Context, in protocol.InitMeta) (Features, error)
	// Status answers _arvel/status.
	Status(ctx context.Context) (protocol.Status, error)
	// Models answers _arvel/models; refresh asks for a fresh list.
	Models(ctx context.Context, refresh bool) (protocol.ModelsResult, error)
	// Usage answers _arvel/usage.
	Usage(ctx context.Context) (protocol.Usage, error)
	// Action runs one of the actions announced in the status.
	Action(ctx context.Context, id string) (protocol.ActionResult, error)
	// NewSession creates a session for session/new.
	NewSession(ctx context.Context, p SessionParams) (Session, error)
	// LoadSession resumes a session for session/load and after an idle close.
	// It is only called when Features.LoadSession is true.
	LoadSession(ctx context.Context, p LoadParams) (Session, error)
	// Shutdown is called once at the end, after every session was closed.
	Shutdown(ctx context.Context) error
}

// Session is one ACP session.
type Session interface {
	// ID is the ACP session id; it must not change.
	ID() string
	// Prompt runs one turn. It streams through out and returns when the turn
	// ends. It must honour ctx, which ends at shutdown or when the connection
	// ends; a session/cancel does not cancel ctx but calls Cancel.
	Prompt(ctx context.Context, blocks []acp.ContentBlock, out Output) (TurnResult, error)
	// SetOption changes a config option. The change applies from the next turn
	// and must not interrupt a running turn (PROTOCOL §7.5).
	SetOption(ctx context.Context, id, value string) ([]acp.SessionConfigOption, error)
	// Cancel asks the running turn to stop gracefully. It must not block and
	// must be idempotent. The provider owns killing a stuck vendor process.
	Cancel()
	// Close releases the session. It is called once, with a deadline.
	Close(ctx context.Context) error
}

// Output is what a turn writes to.
type Output interface {
	// Update sends a session/update notification. meta carries the Arvel
	// per-update metadata (subagent, interactive, state) and may be nil.
	Update(u acp.SessionUpdate, meta *protocol.UpdateMeta) error
	// RequestPermission asks the host and blocks until it answers, the session
	// is cancelled (then the decision has Cancelled set) or ctx ends. There is
	// no timeout (PROTOCOL §7.3).
	RequestPermission(ctx context.Context, req PermissionRequest) (PermissionDecision, error)
}

// Host is the provider's handle for messages it starts itself.
type Host interface {
	// StatusChanged sends _arvel/status_changed; identical consecutive
	// statuses are dropped.
	StatusChanged(protocol.Status)
	// UsageUpdated sends _arvel/usage_updated, at most once per 5 s; the last
	// value wins.
	UsageUpdated(protocol.Usage)
	// Logger returns the plugin logger (JSON on stderr, secrets redacted).
	Logger() *slog.Logger
}

// HostAware is implemented by a Provider that wants its Host. SetHost is called
// before Initialize.
type HostAware interface{ SetHost(Host) }

// Discoverer is implemented by a Provider that supports _arvel/discover. The
// framework then announces the "discover" feature.
type Discoverer interface {
	Discover(ctx context.Context, p protocol.DiscoverParams) (protocol.DiscoverResult, error)
}

// ConfigOptioner is implemented by a Session that reports ACP config options
// (categories model and thought_level) in the session/new response.
type ConfigOptioner interface {
	ConfigOptions() []acp.SessionConfigOption
}

// Features is what Initialize reports about the plugin. The status, models,
// usage and actions features are always announced.
type Features struct {
	// LoadSession announces session/load and the "resume" feature.
	LoadSession bool
	// Prompt content types the plugin accepts besides text and resource links.
	PromptImage, PromptAudio, PromptEmbeddedContext bool
	// MCP transports the plugin can hand to the vendor CLI.
	MCPHTTP, MCPSSE bool
	// Subagents announces the "subagents" feature.
	Subagents bool
	// Skills, when set, announces the "skills" feature and where the vendor CLI
	// looks for project skills.
	Skills *protocol.SkillsInfo
	// InstructionFiles are files the vendor CLI reads from the working directory.
	InstructionFiles []string
	// MaxParallelSessions announces a soft limit; 0 announces none.
	MaxParallelSessions int
}

// SessionParams are the params of session/new.
type SessionParams struct {
	CWD        string
	MCPServers []acp.McpServer
	// Meta is the decoded _meta.arvel; the zero value when absent.
	Meta protocol.SessionMeta
}

// LoadParams are the params of session/load, or the stored params of an
// idle-closed session that is loaded again.
type LoadParams struct {
	SessionID string
	SessionParams
}

// TurnResult is the outcome of a turn.
type TurnResult struct {
	// StopReason must be one of the ACP stop reasons.
	StopReason acp.StopReason
	// Usage and Stop are sent in _meta.arvel of the session/prompt result.
	Usage *protocol.TurnUsage
	Stop  *protocol.StopDetail
}

// PermissionRequest is a request for the host's decision.
type PermissionRequest struct {
	ToolCall acp.ToolCallUpdate
	Options  []acp.PermissionOption
	Meta     protocol.PermissionMeta
}

// PermissionDecision is the host's answer.
type PermissionDecision struct {
	// OptionID is the chosen option; empty when Cancelled.
	OptionID string
	// Cancelled is set when the session was cancelled, the host cancelled the
	// request, or the answer was unusable. Treat it as a rejection.
	Cancelled bool
}
