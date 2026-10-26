// SPDX-License-Identifier: Apache-2.0

package protocol

// Built-in role ids (PROTOCOL §7.1).
const (
	RoleLead      = "lead"
	RoleArchitect = "architect"
	RoleDeveloper = "developer"
	RoleReviewer  = "reviewer"
	RoleSecurity  = "security"
	RoleTester    = "tester"
	RoleKeeper    = "keeper"
)

// Slots of the fixed order (PROTOCOL §14.6).
const (
	SlotLeadIntake = "lead_intake"
	SlotArchitect  = "architect"
	SlotDeveloper  = "developer"
	SlotReviewer   = "reviewer"
	SlotSecurity   = "security"
	SlotTester     = "tester"
	SlotLeadAccept = "lead_accept"
)

// Built-in task type ids (PROTOCOL §7.1).
const (
	TypeSpec     = "spec"
	TypeFeature  = "feature"
	TypeBug      = "bug"
	TypeEdit     = "edit"
	TypeReview   = "review"
	TypeAnalysis = "analysis"
)

// Order returns the slots of the fixed order, first to last. Each call returns
// a fresh slice.
func Order() []string {
	return []string{SlotLeadIntake, SlotArchitect, SlotDeveloper, SlotReviewer, SlotSecurity, SlotTester, SlotLeadAccept}
}

// IsOrderSlot reports whether s is one of the seven slots of Order.
func IsOrderSlot(s string) bool {
	for _, o := range Order() {
		if o == s {
			return true
		}
	}
	return false
}

// PluginSlot returns the slot (and stored role) id of a role added by a plugin:
// "<pluginID>:<roleID>" (PROTOCOL §14.3, §16.2).
func PluginSlot(pluginID, roleID string) string { return pluginID + ":" + roleID }
