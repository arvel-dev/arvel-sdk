// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arvel-dev/arvel-sdk/manifest"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

func runRouterBasic(t *testing.T, bin, manifestPath string) []CheckResult {
	t.Helper()
	m, err := manifest.LoadFile(manifestPath)
	if err != nil {
		t.Fatalf("load manifest %s: %v", manifestPath, err)
	}
	f := &flags{level: "basic", binary: bin, manifest: manifestPath}
	return routerBasic(context.Background(), f, m)
}

func TestRouterBasic_EchoRouterPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo-router", "echo-router")
	cs := runRouterBasic(t, bin, filepath.Join("..", "..", "examples", "echo-router", "arvel.json"))

	for _, name := range []string{"initialize", "status", "session/new", "shutdown", "canary leak"} {
		if c := findCheck(t, cs, name); c.Status != StatusPass {
			t.Fatalf("check %q: status=%s detail=%s", name, c.Status, c.Detail)
		}
	}
	if c := findCheck(t, cs, "stderr JSON lines"); c.Status == StatusFail {
		t.Fatalf("stderr check failed: %s", c.Detail)
	}
}

// The stubrouter speaks raw JSON-RPC, so the checker itself must reject a
// non-empty agentCapabilities; no SDK framework hides the defect.
func TestRouterBasic_AgentCapabilityFails(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	t.Setenv("STUB_MODE", "agent-capability")
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubrouter", "stubrouter")
	cs := runRouterBasic(t, bin, filepath.Join("testdata", "stubrouter", "arvel.json"))

	if c := findCheck(t, cs, "initialize"); c.Status != StatusFail || !strings.Contains(c.Detail, "agentCapabilities") {
		t.Fatalf("initialize: status=%s detail=%s", c.Status, c.Detail)
	}
}

func TestRouterBasic_SessionNewFails(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	t.Setenv("STUB_MODE", "session-new")
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubrouter", "stubrouter")
	cs := runRouterBasic(t, bin, filepath.Join("testdata", "stubrouter", "arvel.json"))

	if c := findCheck(t, cs, "session/new"); c.Status != StatusFail || !strings.Contains(c.Detail, "-32601") {
		t.Fatalf("session/new: status=%s detail=%s", c.Status, c.Detail)
	}
}

func TestFirstNonRouterFeature(t *testing.T) {
	all := []string{protocol.FeatureRoutePlan, protocol.FeatureRouteMessage, protocol.FeatureRouteEffort, protocol.FeatureRouteExplain}
	if got := firstNonRouterFeature(all); got != "" {
		t.Fatalf("firstNonRouterFeature(all route features) = %q, want empty", got)
	}
	if got := firstNonRouterFeature(append(append([]string{}, all...), protocol.FeatureStatus)); got != protocol.FeatureStatus {
		t.Fatalf("firstNonRouterFeature = %q, want %q", got, protocol.FeatureStatus)
	}
	if !isRouterFeature(protocol.FeatureRoutePlan) || isRouterFeature(protocol.FeatureModels) {
		t.Fatalf("isRouterFeature misclassifies a feature")
	}
}

func TestRouterBasic_DetectsCanaryOnStdout(t *testing.T) {
	t.Setenv("STUB_MODE", "stdout-canary")
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubrouter", "stubrouter")
	cs := runRouterBasic(t, bin, filepath.Join("testdata", "stubrouter", "arvel.json"))
	if c := findCheck(t, cs, "canary leak"); c.Status != StatusFail {
		t.Fatalf("stdout secret leak was missed: %+v", c)
	}
	for _, c := range cs {
		if strings.Contains(c.Detail, "arvel-canary-") {
			t.Fatal("report leaked canary")
		}
	}
}
