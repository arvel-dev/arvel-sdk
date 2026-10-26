// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionMetaValidate(t *testing.T) {
	good := SessionMeta{
		Role: &RoleRef{ID: "developer"}, Model: "m", Effort: EffortMedium,
		Permissions: &Permissions{Read: PermissionLevelAllow, Edit: PermissionLevelAllow, Network: PermissionLevelAsk,
			Commands: &CommandPolicy{Default: PermissionLevelAsk},
			MCP:      &MCPPolicy{Default: PermissionLevelAsk, Tools: map[string]PermissionLevel{"pg/query": PermissionLevelAllow}}},
		Subagents: &Subagents{Enabled: true, Effort: EffortLow, MaxParallel: 2},
		Budget:    &Budget{MaxTurns: 200, MaxUSD: 5},
		Skills:    []SkillRef{{Name: "a", Path: "/p", Invocation: SkillInvocationAuto}},
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]SessionMeta{
		"effort":   {Effort: "ultra"},
		"commands": {Permissions: &Permissions{Commands: &CommandPolicy{Default: "maybe"}}},
		"mcp tool": {Permissions: &Permissions{MCP: &MCPPolicy{Default: PermissionLevelAsk, Tools: map[string]PermissionLevel{"x": "no"}}}},
		"mcp def":  {Permissions: &Permissions{MCP: &MCPPolicy{Default: "no"}}},
		"read":     {Permissions: &Permissions{Read: "x"}},
		"skill":    {Skills: []SkillRef{{Name: "a", Invocation: SkillInvocationAuto}}},
		"inv":      {Skills: []SkillRef{{Name: "a", Path: "/p", Invocation: "x"}}},
		"budget":   {Budget: &Budget{MaxTurns: -1}},
		"sub eff":  {Subagents: &Subagents{Effort: "x"}},
		"sub par":  {Subagents: &Subagents{MaxParallel: -1}},
	}
	for name, m := range bad {
		if m.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestSessionMetaResumeFrom(t *testing.T) {
	var m SessionMeta
	if err := json.Unmarshal([]byte(`{"resumeFrom":null}`), &m); err != nil || m.ResumeFrom != nil {
		t.Fatalf("null: %v %v", m.ResumeFrom, err)
	}
	if err := json.Unmarshal([]byte(`{"resumeFrom":"abc"}`), &m); err != nil || m.ResumeFrom == nil || *m.ResumeFrom != "abc" {
		t.Fatalf("string: %v %v", m.ResumeFrom, err)
	}
}

func TestPermissionMetaValidate(t *testing.T) {
	if err := (PermissionMeta{Category: PermissionCategoryCommand}).Validate(); err != nil {
		t.Fatal(err)
	}
	if (PermissionMeta{Category: "x"}).Validate() == nil {
		t.Fatal("want error")
	}
}

func TestUpdateMetaAndTurnMeta(t *testing.T) {
	u := UpdateMeta{Subagent: &SubagentMeta{ID: "s", ParentToolCallID: "t", Label: "Explore"}}
	b, _ := json.Marshal(u)
	var u2 UpdateMeta
	if err := json.Unmarshal(b, &u2); err != nil || *u2.Subagent != *u.Subagent {
		t.Fatalf("update round trip: %s %v", b, err)
	}
	tm := TurnMeta{
		Usage: &TurnUsage{InputTokens: 1, OutputTokens: 2, CostUSD: 0.42, CostSource: UsageSourceEstimated},
		Stop:  &StopDetail{Code: ErrRateLimited, ResetsAt: time.Date(2026, 10, 25, 18, 40, 0, 0, time.UTC)},
	}
	b, _ = json.Marshal(tm)
	var tm2 TurnMeta
	if err := json.Unmarshal(b, &tm2); err != nil || tm2.Stop.Code != ErrRateLimited || tm2.Usage.CostUSD != 0.42 {
		t.Fatalf("turn round trip: %s %v", b, err)
	}
}

func TestSecretFieldRejectsLiteral(t *testing.T) {
	var f SecretField
	if json.Unmarshal([]byte(`"hunter2"`), &f) == nil {
		t.Fatal("literal string must not decode")
	}
	var m DiscoveredMcp
	if json.Unmarshal([]byte(`{"env":{"DATABASE_URL":"postgres://u:p@h/db"}}`), &m) == nil {
		t.Fatal("literal env value must not decode")
	}
}

func TestSecretFieldValidate(t *testing.T) {
	for f, ok := range map[SecretField]bool{
		{}: false, {Secret: true, EnvRef: "X"}: false, {Secret: true}: true, {EnvRef: "X"}: true,
	} {
		if (f.Validate() == nil) != ok {
			t.Errorf("%+v: ok=%v", f, ok)
		}
	}
}

func TestDiscoverResultValidate(t *testing.T) {
	good := DiscoverResult{
		McpServers: []DiscoveredMcp{
			{Name: "pg", Scope: DiscoverScopeProject, Project: "/p", Transport: McpTransportStdio, Command: []string{"npx"},
				Env: map[string]SecretField{"DATABASE_URL": {Secret: true}}},
			{Name: "gh", Scope: DiscoverScopeUser, Transport: McpTransportHTTP, URL: "https://x",
				Headers: map[string]SecretField{"Authorization": {EnvRef: "GH"}}},
		},
		Skills: []DiscoveredSkill{{Name: "s", Scope: DiscoverScopeUser, Path: "/s"}},
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]DiscoverResult{
		"stdio no cmd":  {McpServers: []DiscoveredMcp{{Name: "a", Scope: DiscoverScopeUser, Transport: McpTransportStdio}}},
		"http no url":   {McpServers: []DiscoveredMcp{{Name: "a", Scope: DiscoverScopeUser, Transport: McpTransportHTTP}}},
		"project":       {McpServers: []DiscoveredMcp{{Name: "a", Scope: DiscoverScopeProject, Transport: McpTransportSSE, URL: "u"}}},
		"transport":     {McpServers: []DiscoveredMcp{{Name: "a", Scope: DiscoverScopeUser, Transport: "x"}}},
		"scope":         {McpServers: []DiscoveredMcp{{Name: "a", Scope: "x", Transport: McpTransportSSE, URL: "u"}}},
		"name":          {McpServers: []DiscoveredMcp{{Scope: DiscoverScopeUser, Transport: McpTransportSSE, URL: "u"}}},
		"env":           {McpServers: []DiscoveredMcp{{Name: "a", Scope: DiscoverScopeUser, Transport: McpTransportSSE, URL: "u", Env: map[string]SecretField{"A": {}}}}},
		"header":        {McpServers: []DiscoveredMcp{{Name: "a", Scope: DiscoverScopeUser, Transport: McpTransportSSE, URL: "u", Headers: map[string]SecretField{"A": {}}}}},
		"skill no path": {Skills: []DiscoveredSkill{{Name: "s", Scope: DiscoverScopeUser}}},
		"skill scope":   {Skills: []DiscoveredSkill{{Name: "s", Scope: "x", Path: "/p"}}},
	}
	for name, r := range bad {
		if r.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestSessionMetaTaskValidate(t *testing.T) {
	ok := SessionMeta{Task: &TaskMeta{Number: 42, Type: "feature"}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, task := range map[string]*TaskMeta{
		"negative number": {Number: -1, Type: "feature"},
		"empty type":      {Number: 1},
	} {
		if (SessionMeta{Task: task}).Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestNormalize_ReadOnlyForcesDeny(t *testing.T) {
	cmds := &CommandPolicy{Default: PermissionLevelAsk}
	for _, tc := range []struct {
		name string
		perm *Permissions
	}{
		{"no permissions", nil},
		{"allow", &Permissions{Edit: PermissionLevelAllow, Commands: cmds}},
		{"ask", &Permissions{Edit: PermissionLevelAsk}},
		{"deny", &Permissions{Edit: PermissionLevelDeny}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before PermissionLevel
			if tc.perm != nil {
				before = tc.perm.Edit
			}
			m := SessionMeta{Task: &TaskMeta{Type: "review", ReadOnly: true}, Permissions: tc.perm}
			got := m.Normalize()
			if got.Permissions == nil || got.Permissions.Edit != PermissionLevelDeny {
				t.Fatalf("edit not denied: %+v", got.Permissions)
			}
			if tc.perm != nil && tc.perm.Edit != before {
				t.Fatal("receiver was modified")
			}
			if tc.perm != nil && tc.perm.Commands != got.Permissions.Commands {
				t.Fatal("commands policy not preserved")
			}
		})
	}
	writable := SessionMeta{Task: &TaskMeta{Type: "feature"}, Permissions: &Permissions{Edit: PermissionLevelAllow}}
	if writable.Normalize().Permissions.Edit != PermissionLevelAllow {
		t.Fatal("writable task must keep edit")
	}
	if (SessionMeta{}).Normalize().Permissions != nil {
		t.Fatal("no task: unchanged")
	}
}
