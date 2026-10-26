// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"slices"
	"testing"
)

func TestOrder_IsFixedAndFresh(t *testing.T) {
	want := []string{"lead_intake", "architect", "developer", "reviewer", "security", "tester", "lead_accept"}
	got := Order()
	if !slices.Equal(got, want) {
		t.Fatalf("Order() = %v", got)
	}
	got[0] = "mutated"
	if Order()[0] != SlotLeadIntake {
		t.Fatal("Order must return a fresh slice")
	}
}

func TestIsOrderSlot(t *testing.T) {
	for s, want := range map[string]bool{
		"lead_intake": true, "architect": true, "developer": true, "reviewer": true, "security": true,
		"tester": true, "lead_accept": true, "keeper": false, "": false, "godot:x": false,
	} {
		if got := IsOrderSlot(s); got != want {
			t.Errorf("IsOrderSlot(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestPluginSlot(t *testing.T) {
	if got := PluginSlot("godot", "scene-designer"); got != "godot:scene-designer" {
		t.Fatalf("got %q", got)
	}
}
