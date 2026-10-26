// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"slices"
	"testing"
)

type enum interface {
	~string
	Valid() bool
}

// checkEnum asserts that every wanted value is valid and in all (in order),
// and that the usual bad inputs are rejected.
func checkEnum[T enum](t *testing.T, want, all []T) {
	t.Helper()
	if !slices.Equal(want, all) {
		t.Fatalf("All* = %v, want %v", all, want)
	}
	for _, v := range want {
		if !v.Valid() {
			t.Errorf("%q should be valid", v)
		}
		if upper := T(upperASCII(string(v))); upper != v && upper.Valid() {
			t.Errorf("%q (wrong case) should be invalid", upper)
		}
	}
	for _, bad := range []string{"", "bogus", " "} {
		if T(bad).Valid() {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func upperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func TestEnums(t *testing.T) {
	t.Run("Effort", func(t *testing.T) {
		checkEnum(t, []Effort{"low", "medium", "high", "xhigh", "max"}, AllEfforts())
	})
	t.Run("StatusState", func(t *testing.T) {
		checkEnum(t, []StatusState{"ready", "not_installed", "outdated", "needs_login", "degraded", "error"}, AllStatusStates())
	})
	t.Run("ActionKind", func(t *testing.T) {
		checkEnum(t, []ActionKind{"terminal", "url", "done"}, AllActionKinds())
	})
	t.Run("UsageSource", func(t *testing.T) {
		checkEnum(t, []UsageSource{"exact", "estimated", "unknown"}, AllUsageSources())
	})
	t.Run("UsageStatus", func(t *testing.T) {
		checkEnum(t, []UsageStatus{"ok", "warning", "exhausted"}, AllUsageStatuses())
	})
	t.Run("Billing", func(t *testing.T) {
		checkEnum(t, []Billing{"subscription", "api", "mixed", "unknown"}, AllBillings())
	})
	t.Run("PermissionLevel", func(t *testing.T) {
		checkEnum(t, []PermissionLevel{"allow", "ask", "deny"}, AllPermissionLevels())
	})
	t.Run("PermissionCategory", func(t *testing.T) {
		checkEnum(t, []PermissionCategory{"command", "edit", "read", "network", "mcp", "other"}, AllPermissionCategories())
	})
	t.Run("SkillInvocation", func(t *testing.T) {
		checkEnum(t, []SkillInvocation{"auto", "manual"}, AllSkillInvocations())
	})
	t.Run("SandboxMode", func(t *testing.T) {
		checkEnum(t, []SandboxMode{"os", "container", "none"}, AllSandboxModes())
	})
	t.Run("DiscoverKind", func(t *testing.T) {
		checkEnum(t, []DiscoverKind{"mcp", "skills"}, AllDiscoverKinds())
	})
	t.Run("DiscoverScope", func(t *testing.T) {
		checkEnum(t, []DiscoverScope{"user", "project"}, AllDiscoverScopes())
	})
	t.Run("McpTransport", func(t *testing.T) {
		checkEnum(t, []McpTransport{"stdio", "http", "sse"}, AllMcpTransports())
	})
	t.Run("RoleKind", func(t *testing.T) {
		checkEnum(t, []RoleKind{"intake", "plan", "code", "check", "accept", "knowledge", "read"}, AllRoleKinds())
	})
	t.Run("SlotState", func(t *testing.T) {
		checkEnum(t, []SlotState{"on", "off", "auto", "optional"}, AllSlotStates())
	})
}

func TestAllReturnsFreshSlice(t *testing.T) {
	a := AllEfforts()
	a[0] = "mutated"
	if AllEfforts()[0] != EffortLow {
		t.Fatal("AllEfforts must return a fresh slice")
	}
}

func TestFeatureMaxParallelSessions(t *testing.T) {
	if got := FeatureMaxParallelSessions(3); got != "maxParallelSessions:3" {
		t.Fatalf("got %q", got)
	}
	if n, ok := ParseMaxParallelSessions([]string{"status", "maxParallelSessions:4"}); !ok || n != 4 {
		t.Errorf("got %d %v", n, ok)
	}
	for _, bad := range [][]string{
		{"maxParallelSessions:0"}, {"maxParallelSessions:x"}, {"maxParallelSessions:-2"}, {"status"}, nil,
	} {
		if n, ok := ParseMaxParallelSessions(bad); ok || n != 0 {
			t.Errorf("%v: got %d %v", bad, n, ok)
		}
	}
}
