// SPDX-License-Identifier: Apache-2.0

package protocol

// Every enum is a string type with a Valid method and an AllX function that
// returns a fresh slice. Decoding is permissive; validation is explicit.

// Effort is a reasoning effort level (PROTOCOL §6.2).
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// Valid reports whether v is a defined Effort value.
func (v Effort) Valid() bool {
	switch v {
	case EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	}
	return false
}

// AllEfforts returns every defined Effort value in protocol order.
func AllEfforts() []Effort {
	return []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
}

// StatusState is the state reported by _arvel/status (PROTOCOL §6.1).
type StatusState string

const (
	StateReady        StatusState = "ready"
	StateNotInstalled StatusState = "not_installed"
	StateOutdated     StatusState = "outdated"
	StateNeedsLogin   StatusState = "needs_login"
	StateDegraded     StatusState = "degraded"
	StateError        StatusState = "error"
)

// Valid reports whether v is a defined StatusState value.
func (v StatusState) Valid() bool {
	switch v {
	case StateReady, StateNotInstalled, StateOutdated, StateNeedsLogin, StateDegraded, StateError:
		return true
	}
	return false
}

// AllStatusStates returns every defined StatusState value in protocol order.
func AllStatusStates() []StatusState {
	return []StatusState{StateReady, StateNotInstalled, StateOutdated, StateNeedsLogin, StateDegraded, StateError}
}

// ActionKind is the kind of an action or its result (PROTOCOL §6.4).
type ActionKind string

const (
	ActionKindTerminal ActionKind = "terminal"
	ActionKindURL      ActionKind = "url"
	ActionKindDone     ActionKind = "done"
)

// Valid reports whether v is a defined ActionKind value.
func (v ActionKind) Valid() bool {
	switch v {
	case ActionKindTerminal, ActionKindURL, ActionKindDone:
		return true
	}
	return false
}

// AllActionKinds returns every defined ActionKind value in protocol order.
func AllActionKinds() []ActionKind {
	return []ActionKind{ActionKindTerminal, ActionKindURL, ActionKindDone}
}

// UsageSource tells how exact usage numbers are (PROTOCOL §6.3).
type UsageSource string

const (
	UsageSourceExact     UsageSource = "exact"
	UsageSourceEstimated UsageSource = "estimated"
	UsageSourceUnknown   UsageSource = "unknown"
)

// Valid reports whether v is a defined UsageSource value.
func (v UsageSource) Valid() bool {
	switch v {
	case UsageSourceExact, UsageSourceEstimated, UsageSourceUnknown:
		return true
	}
	return false
}

// AllUsageSources returns every defined UsageSource value in protocol order.
func AllUsageSources() []UsageSource {
	return []UsageSource{UsageSourceExact, UsageSourceEstimated, UsageSourceUnknown}
}

// UsageStatus is the overall usage status (PROTOCOL §6.3).
type UsageStatus string

const (
	UsageStatusOK        UsageStatus = "ok"
	UsageStatusWarning   UsageStatus = "warning"
	UsageStatusExhausted UsageStatus = "exhausted"
)

// Valid reports whether v is a defined UsageStatus value.
func (v UsageStatus) Valid() bool {
	switch v {
	case UsageStatusOK, UsageStatusWarning, UsageStatusExhausted:
		return true
	}
	return false
}

// AllUsageStatuses returns every defined UsageStatus value in protocol order.
func AllUsageStatuses() []UsageStatus {
	return []UsageStatus{UsageStatusOK, UsageStatusWarning, UsageStatusExhausted}
}

// Billing is the billing mode of a connection or model (PROTOCOL §4).
type Billing string

const (
	BillingSubscription Billing = "subscription"
	BillingAPI          Billing = "api"
	BillingMixed        Billing = "mixed"
	BillingUnknown      Billing = "unknown"
)

// Valid reports whether v is a defined Billing value.
func (v Billing) Valid() bool {
	switch v {
	case BillingSubscription, BillingAPI, BillingMixed, BillingUnknown:
		return true
	}
	return false
}

// AllBillings returns every defined Billing value in protocol order.
func AllBillings() []Billing {
	return []Billing{BillingSubscription, BillingAPI, BillingMixed, BillingUnknown}
}

// PermissionLevel is a role permission level (PROTOCOL §7.1).
type PermissionLevel string

const (
	PermissionLevelAllow PermissionLevel = "allow"
	PermissionLevelAsk   PermissionLevel = "ask"
	PermissionLevelDeny  PermissionLevel = "deny"
)

// Valid reports whether v is a defined PermissionLevel value.
func (v PermissionLevel) Valid() bool {
	switch v {
	case PermissionLevelAllow, PermissionLevelAsk, PermissionLevelDeny:
		return true
	}
	return false
}

// AllPermissionLevels returns every defined PermissionLevel value in protocol order.
func AllPermissionLevels() []PermissionLevel {
	return []PermissionLevel{PermissionLevelAllow, PermissionLevelAsk, PermissionLevelDeny}
}

// PermissionCategory is the category of a permission request (PROTOCOL §7.3).
type PermissionCategory string

const (
	PermissionCategoryCommand PermissionCategory = "command"
	PermissionCategoryEdit    PermissionCategory = "edit"
	PermissionCategoryRead    PermissionCategory = "read"
	PermissionCategoryNetwork PermissionCategory = "network"
	PermissionCategoryMCP     PermissionCategory = "mcp"
	PermissionCategoryOther   PermissionCategory = "other"
)

// Valid reports whether v is a defined PermissionCategory value.
func (v PermissionCategory) Valid() bool {
	switch v {
	case PermissionCategoryCommand, PermissionCategoryEdit, PermissionCategoryRead, PermissionCategoryNetwork, PermissionCategoryMCP, PermissionCategoryOther:
		return true
	}
	return false
}

// AllPermissionCategories returns every defined PermissionCategory value in protocol order.
func AllPermissionCategories() []PermissionCategory {
	return []PermissionCategory{PermissionCategoryCommand, PermissionCategoryEdit, PermissionCategoryRead, PermissionCategoryNetwork, PermissionCategoryMCP, PermissionCategoryOther}
}

// SkillInvocation tells whether a skill is invoked automatically (PROTOCOL §7.1).
type SkillInvocation string

const (
	SkillInvocationAuto   SkillInvocation = "auto"
	SkillInvocationManual SkillInvocation = "manual"
)

// Valid reports whether v is a defined SkillInvocation value.
func (v SkillInvocation) Valid() bool {
	switch v {
	case SkillInvocationAuto, SkillInvocationManual:
		return true
	}
	return false
}

// AllSkillInvocations returns every defined SkillInvocation value in protocol order.
func AllSkillInvocations() []SkillInvocation {
	return []SkillInvocation{SkillInvocationAuto, SkillInvocationManual}
}

// SandboxMode is the sandbox mode of a workspace (PROTOCOL §15.1).
type SandboxMode string

const (
	SandboxModeOS        SandboxMode = "os"
	SandboxModeContainer SandboxMode = "container"
	SandboxModeNone      SandboxMode = "none"
)

// Valid reports whether v is a defined SandboxMode value.
func (v SandboxMode) Valid() bool {
	switch v {
	case SandboxModeOS, SandboxModeContainer, SandboxModeNone:
		return true
	}
	return false
}

// AllSandboxModes returns every defined SandboxMode value in protocol order.
func AllSandboxModes() []SandboxMode {
	return []SandboxMode{SandboxModeOS, SandboxModeContainer, SandboxModeNone}
}

// DiscoverKind is a kind of discoverable item (PROTOCOL §7.9).
type DiscoverKind string

const (
	DiscoverKindMCP    DiscoverKind = "mcp"
	DiscoverKindSkills DiscoverKind = "skills"
)

// Valid reports whether v is a defined DiscoverKind value.
func (v DiscoverKind) Valid() bool {
	switch v {
	case DiscoverKindMCP, DiscoverKindSkills:
		return true
	}
	return false
}

// AllDiscoverKinds returns every defined DiscoverKind value in protocol order.
func AllDiscoverKinds() []DiscoverKind {
	return []DiscoverKind{DiscoverKindMCP, DiscoverKindSkills}
}

// DiscoverScope is the scope of a discovered item (PROTOCOL §7.9).
type DiscoverScope string

const (
	DiscoverScopeUser    DiscoverScope = "user"
	DiscoverScopeProject DiscoverScope = "project"
)

// Valid reports whether v is a defined DiscoverScope value.
func (v DiscoverScope) Valid() bool {
	switch v {
	case DiscoverScopeUser, DiscoverScopeProject:
		return true
	}
	return false
}

// AllDiscoverScopes returns every defined DiscoverScope value in protocol order.
func AllDiscoverScopes() []DiscoverScope {
	return []DiscoverScope{DiscoverScopeUser, DiscoverScopeProject}
}

// McpTransport is the transport of a discovered MCP server (PROTOCOL §7.9).
type McpTransport string

const (
	McpTransportStdio McpTransport = "stdio"
	McpTransportHTTP  McpTransport = "http"
	McpTransportSSE   McpTransport = "sse"
)

// Valid reports whether v is a defined McpTransport value.
func (v McpTransport) Valid() bool {
	switch v {
	case McpTransportStdio, McpTransportHTTP, McpTransportSSE:
		return true
	}
	return false
}

// AllMcpTransports returns every defined McpTransport value in protocol order.
func AllMcpTransports() []McpTransport {
	return []McpTransport{McpTransportStdio, McpTransportHTTP, McpTransportSSE}
}

// RoleKind is the kind of a role (PROTOCOL §14.2; "read" is the kind of a
// read-only plugin role, §16.2).
type RoleKind string

const (
	RoleKindIntake    RoleKind = "intake"
	RoleKindPlan      RoleKind = "plan"
	RoleKindCode      RoleKind = "code"
	RoleKindCheck     RoleKind = "check"
	RoleKindAccept    RoleKind = "accept"
	RoleKindKnowledge RoleKind = "knowledge"
	RoleKindRead      RoleKind = "read"
)

// Valid reports whether v is a defined RoleKind value.
func (v RoleKind) Valid() bool {
	switch v {
	case RoleKindIntake, RoleKindPlan, RoleKindCode, RoleKindCheck, RoleKindAccept, RoleKindKnowledge, RoleKindRead:
		return true
	}
	return false
}

// AllRoleKinds returns every defined RoleKind value in protocol order.
func AllRoleKinds() []RoleKind {
	return []RoleKind{RoleKindIntake, RoleKindPlan, RoleKindCode, RoleKindCheck, RoleKindAccept, RoleKindKnowledge, RoleKindRead}
}

// SlotState is a role's default in a task type (PROTOCOL §14.3, §16.3).
type SlotState string

const (
	SlotStateOn       SlotState = "on"
	SlotStateOff      SlotState = "off"
	SlotStateAuto     SlotState = "auto"
	SlotStateOptional SlotState = "optional"
)

// Valid reports whether v is a defined SlotState value.
func (v SlotState) Valid() bool {
	switch v {
	case SlotStateOn, SlotStateOff, SlotStateAuto, SlotStateOptional:
		return true
	}
	return false
}

// AllSlotStates returns every defined SlotState value in protocol order.
func AllSlotStates() []SlotState {
	return []SlotState{SlotStateOn, SlotStateOff, SlotStateAuto, SlotStateOptional}
}
