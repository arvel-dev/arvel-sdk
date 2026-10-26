// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"errors"
	"fmt"
)

// SessionMeta is "_meta.arvel" of session/new and session/load (PROTOCOL §7.1).
type SessionMeta struct {
	Role         *RoleRef     `json:"role,omitempty"`
	Task         *TaskMeta    `json:"task,omitempty"`
	Model        string       `json:"model,omitempty"`
	Effort       Effort       `json:"effort,omitempty"`
	Instructions string       `json:"instructions,omitempty"`
	Language     string       `json:"language,omitempty"`
	Permissions  *Permissions `json:"permissions,omitempty"`
	Subagents    *Subagents   `json:"subagents,omitempty"`
	Budget       *Budget      `json:"budget,omitempty"`
	Skills       []SkillRef   `json:"skills,omitempty"`
	ResumeFrom   *string      `json:"resumeFrom,omitempty"`
}

// RoleRef names the role a session works for.
type RoleRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// TaskMeta is "_meta.arvel.task" of session/new (PROTOCOL §7.1). It is
// informative: plugins must not change behaviour by role or task type, except
// that ReadOnly forbids every file change.
type TaskMeta struct {
	Number   int    `json:"number"`
	Code     string `json:"code,omitempty"`
	Type     string `json:"type"`
	ReadOnly bool   `json:"readOnly"`
}

// Permissions is the role's baseline policy.
type Permissions struct {
	Read     PermissionLevel `json:"read,omitempty"`
	Edit     PermissionLevel `json:"edit,omitempty"`
	Commands *CommandPolicy  `json:"commands,omitempty"`
	Network  PermissionLevel `json:"network,omitempty"`
	MCP      *MCPPolicy      `json:"mcp,omitempty"`
}

// CommandPolicy is the policy for shell commands.
type CommandPolicy struct {
	Default PermissionLevel `json:"default"`
	Allow   []string        `json:"allow,omitempty"`
	Deny    []string        `json:"deny,omitempty"`
}

// MCPPolicy is the policy for MCP tools.
type MCPPolicy struct {
	Default PermissionLevel            `json:"default"`
	Tools   map[string]PermissionLevel `json:"tools,omitempty"`
}

// Subagents configures subagent use.
type Subagents struct {
	Enabled     bool   `json:"enabled"`
	Model       string `json:"model,omitempty"`
	Effort      Effort `json:"effort,omitempty"`
	MaxParallel int    `json:"maxParallel,omitempty"`
}

// Budget limits a session; zero means not set.
type Budget struct {
	MaxTurns int     `json:"maxTurns,omitempty"`
	MaxUSD   float64 `json:"maxUsd,omitempty"`
}

// SkillRef is an Agent Skills folder prepared by the host.
type SkillRef struct {
	Name       string          `json:"name"`
	Path       string          `json:"path"`
	Invocation SkillInvocation `json:"invocation"`
}

// PermissionMeta is "_meta.arvel" of session/request_permission (PROTOCOL §7.3).
type PermissionMeta struct {
	Category PermissionCategory `json:"category"`
	Command  string             `json:"command,omitempty"`
	Paths    []string           `json:"paths,omitempty"`
	Hosts    []string           `json:"hosts,omitempty"`
	Reason   string             `json:"reason,omitempty"`
}

// SubagentMeta marks an update produced inside a subagent (PROTOCOL §7.2).
type SubagentMeta struct {
	ID               string `json:"id"`
	ParentToolCallID string `json:"parentToolCallId"`
	Label            string `json:"label,omitempty"`
}

// UpdateMeta is "_meta.arvel" of session/update (PROTOCOL §7.2).
type UpdateMeta struct {
	Subagent *SubagentMeta `json:"subagent,omitempty"`
}

// TurnMeta is "_meta.arvel" of the session/prompt result.
type TurnMeta struct {
	Usage *TurnUsage  `json:"usage,omitempty"`
	Stop  *StopDetail `json:"stop,omitempty"`
}

func checkLevel(errs *[]error, path string, l PermissionLevel) {
	if l != "" && !l.Valid() {
		*errs = append(*errs, fmt.Errorf("protocol: %s %q is not valid", path, l))
	}
}

// Validate checks enums, skills and budget values.
func (m SessionMeta) Validate() error {
	var errs []error
	if m.Effort != "" && !m.Effort.Valid() {
		errs = append(errs, fmt.Errorf("protocol: effort %q is not valid", m.Effort))
	}
	if p := m.Permissions; p != nil {
		checkLevel(&errs, "permissions.read", p.Read)
		checkLevel(&errs, "permissions.edit", p.Edit)
		checkLevel(&errs, "permissions.network", p.Network)
		if p.Commands != nil {
			checkLevel(&errs, "permissions.commands.default", p.Commands.Default)
		}
		if p.MCP != nil {
			checkLevel(&errs, "permissions.mcp.default", p.MCP.Default)
			for tool, l := range p.MCP.Tools {
				checkLevel(&errs, "permissions.mcp.tools["+tool+"]", l)
			}
		}
	}
	if s := m.Subagents; s != nil {
		if s.Effort != "" && !s.Effort.Valid() {
			errs = append(errs, fmt.Errorf("protocol: subagents.effort %q is not valid", s.Effort))
		}
		if s.MaxParallel < 0 {
			errs = append(errs, errors.New("protocol: subagents.maxParallel is negative"))
		}
	}
	if b := m.Budget; b != nil && (b.MaxTurns < 0 || b.MaxUSD < 0) {
		errs = append(errs, errors.New("protocol: budget values must not be negative"))
	}
	if t := m.Task; t != nil {
		if t.Number < 0 {
			errs = append(errs, errors.New("protocol: task.number is negative"))
		}
		if t.Type == "" {
			errs = append(errs, errors.New("protocol: task.type is empty"))
		}
	}
	for i, s := range m.Skills {
		if s.Name == "" || s.Path == "" {
			errs = append(errs, fmt.Errorf("protocol: skills[%d]: name and path are required", i))
		}
		if !s.Invocation.Valid() {
			errs = append(errs, fmt.Errorf("protocol: skills[%d].invocation %q is not valid", i, s.Invocation))
		}
	}
	return errors.Join(errs...)
}

// Validate checks the category.
func (p PermissionMeta) Validate() error {
	if !p.Category.Valid() {
		return fmt.Errorf("protocol: permission category %q is not valid", p.Category)
	}
	return nil
}

// Normalize applies what a plugin must do whatever the host sent: a read-only
// task never gets edit access (PROTOCOL §7.1). The receiver is not modified.
func (m SessionMeta) Normalize() SessionMeta {
	if m.Task == nil || !m.Task.ReadOnly {
		return m
	}
	var p Permissions
	if m.Permissions != nil {
		p = *m.Permissions
	}
	p.Edit = PermissionLevelDeny
	m.Permissions = &p
	return m
}
