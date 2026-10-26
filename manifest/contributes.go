// SPDX-License-Identifier: Apache-2.0

package manifest

import "github.com/arvel-dev/arvel-sdk/protocol"

// MaxInstructionsSize is the largest role instruction file (PROTOCOL §16.2).
const MaxInstructionsSize = 16 * 1024

// Localized maps a language tag to a text. "en" is required and "ru" optional
// (PROTOCOL §16.5).
type Localized map[string]string

// Contributes is the "contributes" block of a team or integration plugin
// (PROTOCOL §16.1).
type Contributes struct {
	Roles      []RoleSpec      `json:"roles,omitempty"`
	TaskTypes  []TaskTypeSpec  `json:"taskTypes,omitempty"`
	MCPServers []MCPServerSpec `json:"mcpServers,omitempty"`
	Skills     []SkillSpec     `json:"skills,omitempty"`
}

// RoleSpec is a role added by a plugin (PROTOCOL §16.2). The host stores its id
// as "<pluginId>:<id>".
type RoleSpec struct {
	ID           string                `json:"id"`
	Name         Localized             `json:"name"`
	Description  Localized             `json:"description"`
	Kind         protocol.RoleKind     `json:"kind"`
	Placement    Placement             `json:"placement"`
	Instructions Localized             `json:"instructions"` // language -> Markdown file in the package
	Permissions  *protocol.Permissions `json:"permissions,omitempty"`
	Icon         string                `json:"icon,omitempty"`
}

// Placement says where a plugin role runs: right after a slot, or only on call.
// Exactly one of the two is set.
type Placement struct {
	After  string `json:"after,omitempty"`
	OnCall bool   `json:"onCall,omitempty"`
}

// TaskTypeSpec is a task type added by a plugin (PROTOCOL §16.3).
type TaskTypeSpec struct {
	ID          string                        `json:"id"`
	Name        Localized                     `json:"name"`
	Description Localized                     `json:"description"`
	Roles       map[string]protocol.SlotState `json:"roles"`
	Branch      BranchMode                    `json:"branch"`
	Result      ResultMode                    `json:"result"`
}

// MCPServerSpec is an MCP server of an integration plugin (PROTOCOL §16.4):
// either a stdio server started from the plugin's own binary ("{bin}" first in
// Command) or an HTTP URL.
type MCPServerSpec struct {
	Name    string   `json:"name"`
	Command []string `json:"command,omitempty"`
	URL     string   `json:"url,omitempty"`
	Tools   []string `json:"tools,omitempty"`
}

// SkillSpec is an Agent Skills folder inside the release archive.
type SkillSpec struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// BranchMode is the default branch mode of a task type (PROTOCOL §16.3).
type BranchMode string

const (
	BranchNew      BranchMode = "new"
	BranchContinue BranchMode = "continue"
	BranchView     BranchMode = "view"
	BranchAsk      BranchMode = "ask"
)

// Valid reports whether v is a defined BranchMode value.
func (v BranchMode) Valid() bool {
	switch v {
	case BranchNew, BranchContinue, BranchView, BranchAsk:
		return true
	}
	return false
}

// AllBranchModes returns every defined BranchMode value in protocol order.
func AllBranchModes() []BranchMode {
	return []BranchMode{BranchNew, BranchContinue, BranchView, BranchAsk}
}

// ResultMode is what a task type produces (PROTOCOL §16.3).
type ResultMode string

const (
	ResultCode     ResultMode = "code"
	ResultReport   ResultMode = "report"
	ResultSubtasks ResultMode = "subtasks"
)

// Valid reports whether v is a defined ResultMode value.
func (v ResultMode) Valid() bool {
	switch v {
	case ResultCode, ResultReport, ResultSubtasks:
		return true
	}
	return false
}

// AllResultModes returns every defined ResultMode value in protocol order.
func AllResultModes() []ResultMode {
	return []ResultMode{ResultCode, ResultReport, ResultSubtasks}
}
