// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRandomPlan_CoversDeclaredRequestShapes(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	other := rand.New(rand.NewSource(1))
	types, roles := map[int]bool{}, map[int]bool{}
	for i := 0; i < 200; i++ {
		p := randomPlan(rng)
		if !reflect.DeepEqual(p, randomPlan(other)) {
			t.Fatal("generation is not reproducible")
		}
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
		types[len(p.Types)], roles[len(p.Roles)] = true, true
		pluginSlot := false
		for _, r := range p.Roles {
			pluginSlot = pluginSlot || strings.Contains(r.Slot, ":")
		}
		if !pluginSlot {
			t.Fatal("request has no plugin slot")
		}
	}
	for n := 1; n <= 8; n++ {
		if !types[n] {
			t.Fatalf("no request with %d types", n)
		}
	}
	for n := 3; n <= 12; n++ {
		if !roles[n] {
			t.Fatalf("no request with %d roles", n)
		}
	}
}

func TestCheckRouteValidity_PlanOnlyRouterPasses(t *testing.T) {
	t.Setenv("STUB_MODE", "plan-only")
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubrouter", "stubrouter")
	if c := checkRouteValidity(context.Background(), &flags{binary: bin}); c.Status != StatusPass {
		t.Fatalf("plan-only router: %+v", c)
	}
}

func TestCheckRouteValidity_EchoRouterPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo-router", "echo-router")
	if c := checkRouteValidity(context.Background(), &flags{binary: bin}); c.Status != StatusPass {
		t.Fatalf("route validity: status=%s detail=%s", c.Status, c.Detail)
	}
}

// The raw stubrouter returns a confidence outside [0, 1], which the checker's
// own validator must reject by name.
func TestCheckRouteValidity_ConfidenceFails(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	t.Setenv("STUB_MODE", "confidence")
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubrouter", "stubrouter")
	old := randomizedRequests
	randomizedRequests = 1
	t.Cleanup(func() { randomizedRequests = old })

	c := checkRouteValidity(context.Background(), &flags{binary: bin})
	if c.Status != StatusFail || !strings.Contains(c.Detail, "outside [0, 1]") {
		t.Fatalf("route validity: status=%s detail=%s", c.Status, c.Detail)
	}
}

// The raw stubrouter never answers route/plan in "stall" mode, so the checker's
// host-side deadline must fail the check.
func TestCheckRouteValidity_StallFails(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	t.Setenv("STUB_MODE", "stall")
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubrouter", "stubrouter")
	oldReq, oldDeadline := randomizedRequests, routeDeadline
	randomizedRequests = 1
	routeDeadline = 200 * time.Millisecond
	t.Cleanup(func() { randomizedRequests, routeDeadline = oldReq, oldDeadline })

	c := checkRouteValidity(context.Background(), &flags{binary: bin})
	if c.Status != StatusFail || !strings.Contains(c.Detail, "exceeded") {
		t.Fatalf("route validity: status=%s detail=%s", c.Status, c.Detail)
	}
}
