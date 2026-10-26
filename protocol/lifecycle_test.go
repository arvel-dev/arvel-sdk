// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestConnectionInfoDoesNotLeak(t *testing.T) {
	c := ConnectionInfo{ID: "c1", Kind: "api_key", Secrets: map[string]string{"apiKey": "sk-ant-SECRET123"}}
	out := fmt.Sprintf("%v %+v %#v %s", c, c, c, c)
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "conn", c)
	out += buf.String()
	if strings.Contains(out, "SECRET123") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, "apiKey") {
		t.Fatalf("secret name missing: %s", out)
	}
}

func TestInitResultHas(t *testing.T) {
	r := InitResultMeta{Features: []string{FeatureStatus, FeatureModels}}
	if !r.Has(FeatureStatus) || r.Has(FeatureUsage) {
		t.Fatal("Has misbehaves")
	}
}

func TestSandboxInfoValidate(t *testing.T) {
	ok := []SandboxInfo{
		{Mode: SandboxModeOS, Proxy: "http://127.0.0.1:41234", Workspace: "w1"},
		{Mode: SandboxModeNone},
		{Mode: SandboxModeContainer, Proxy: "http://proxy:3128"},
	}
	for _, s := range ok {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := []SandboxInfo{
		{Mode: "vm"}, {}, {Mode: SandboxModeOS, Proxy: "socks5://127.0.0.1:1"},
		{Mode: SandboxModeOS, Proxy: "http://"}, {Mode: SandboxModeOS, Proxy: "::bad"},
	}
	for _, s := range bad {
		if s.Validate() == nil {
			t.Errorf("%+v: want error", s)
		}
	}
}

func TestStatusValidate(t *testing.T) {
	good := Status{State: StateReady, Binary: &BinaryInfo{Name: "claude"},
		Actions: []Action{{ID: "login", Label: "Sign in", Kind: ActionKindTerminal}}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := []Status{
		{State: "bogus"},
		{State: StateReady, Binary: &BinaryInfo{}},
		{State: StateReady, Actions: []Action{{ID: "", Label: "x", Kind: ActionKindDone}}},
		{State: StateReady, Actions: []Action{{ID: "a", Label: "x", Kind: "nope"}}},
	}
	for _, s := range bad {
		if s.Validate() == nil {
			t.Errorf("%+v: want error", s)
		}
	}
}

func TestActionResultValidate(t *testing.T) {
	ok := []ActionResult{
		{Kind: ActionKindTerminal, Command: []string{"claude", "auth", "login"}},
		{Kind: ActionKindURL, URL: "https://auth.openai.com/x"},
		{Kind: ActionKindDone, Message: "Signed out."},
	}
	for _, a := range ok {
		if err := a.Validate(); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	bad := []ActionResult{
		{Kind: ActionKindTerminal}, {Kind: ActionKindTerminal, Command: []string{""}},
		{Kind: ActionKindURL, URL: "javascript:alert(1)"}, {Kind: ActionKindURL},
		{Kind: "nope"},
	}
	for _, a := range bad {
		if a.Validate() == nil {
			t.Errorf("%+v: want error", a)
		}
	}
}

func TestModelMarshalEfforts(t *testing.T) {
	b, err := json.Marshal(Model{ID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"efforts":[]`) {
		t.Fatalf("got %s", b)
	}
}

func TestModelsResultValidate(t *testing.T) {
	good := ModelsResult{Default: "a", Models: []Model{
		{ID: "a", Label: "A", Efforts: []Effort{EffortLow, EffortHigh}, DefaultEffort: EffortLow, Billing: BillingSubscription},
		{ID: "b", Label: "B"},
	}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]ModelsResult{
		"empty":        {Default: "a"},
		"duplicate":    {Default: "a", Models: []Model{{ID: "a"}, {ID: "a"}}},
		"bad effort":   {Default: "a", Models: []Model{{ID: "a", Efforts: []Effort{"ultra"}}}},
		"no default":   {Default: "z", Models: []Model{{ID: "a"}}},
		"default eff":  {Default: "a", Models: []Model{{ID: "a", Efforts: []Effort{EffortLow}, DefaultEffort: EffortMax}}},
		"eff w/o list": {Default: "a", Models: []Model{{ID: "a", DefaultEffort: EffortLow}}},
		"empty id":     {Default: "", Models: []Model{{ID: ""}}},
		"bad billing":  {Default: "a", Models: []Model{{ID: "a", Billing: "free"}}},
	}
	for name, r := range bad {
		if r.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestUsageValidate(t *testing.T) {
	good := Usage{Source: UsageSourceExact, Status: UsageStatusOK,
		Windows: []UsageWindow{{ID: "w", UsedPercent: 38}}, Spend: &Spend{Currency: "USD", SinceStart: 1.84}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := []Usage{
		{Source: "x", Status: UsageStatusOK},
		{Source: UsageSourceExact, Status: "x"},
		{Source: UsageSourceExact, Status: UsageStatusOK, Windows: []UsageWindow{{ID: "w", UsedPercent: 101}}},
		{Source: UsageSourceExact, Status: UsageStatusOK, Windows: []UsageWindow{{ID: "", UsedPercent: 1}}},
		{Source: UsageSourceExact, Status: UsageStatusOK, Spend: &Spend{Currency: "USD", SinceStart: -1}},
		{Source: UsageSourceExact, Status: UsageStatusOK, Spend: &Spend{SinceStart: 1}},
	}
	for _, u := range bad {
		if u.Validate() == nil {
			t.Errorf("%+v: want error", u)
		}
	}
}
