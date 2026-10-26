// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/redact"
)

const testSecret = "sk-ant-0123456789abcdef"

func TestToRPCError(t *testing.T) {
	rd := redact.New()
	rd.Add(testSecret)
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))

	t.Run("nil", func(t *testing.T) {
		if toRPCError(rd, log, nil) != nil {
			t.Fatal("want nil")
		}
	})
	t.Run("plain_error_is_provider_error_and_redacted", func(t *testing.T) {
		re := toRPCError(rd, log, errors.New("vendor said "+testSecret))
		d, ok := re.Arvel()
		if !ok || d.Code != protocol.ErrProviderError || d.VendorCode != "unclassified" {
			t.Fatalf("got %+v %v", d, ok)
		}
		if strings.Contains(re.Message, testSecret) || !strings.Contains(re.Message, redact.Replacement) {
			t.Fatalf("message not redacted: %q", re.Message)
		}
	})
	t.Run("typed_error_passes_through", func(t *testing.T) {
		in := protocol.NewError(protocol.ErrRateLimited, "window exhausted",
			map[string]any{"windowId": "five_hour", "resetsAt": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		re := toRPCError(rd, log, fmt.Errorf("wrapped: %w", in))
		d, ok := re.Arvel()
		if !ok || d.Code != protocol.ErrRateLimited || d.WindowID != "five_hour" || d.ResetsAt.IsZero() {
			t.Fatalf("got %+v %v", d, ok)
		}
	})
	t.Run("typed_error_is_redacted", func(t *testing.T) {
		in := protocol.NewError(protocol.ErrOverloaded, "busy "+testSecret, nil)
		re := toRPCError(rd, log, in)
		if strings.Contains(re.Message, testSecret) || strings.Contains(string(re.Data), testSecret) {
			t.Fatalf("not redacted: %q %s", re.Message, re.Data)
		}
	})
	t.Run("invalid_arvel_data_becomes_internal", func(t *testing.T) {
		in := protocol.NewError(protocol.ErrProviderError, "no vendor code", nil)
		re := toRPCError(rd, log, in)
		d, _ := re.Arvel()
		if d.Code != protocol.ErrInternal {
			t.Fatalf("got %+v", d)
		}
		if !strings.Contains(logs.String(), "invalid error") {
			t.Fatal("violation not logged")
		}
	})
	t.Run("context_error", func(t *testing.T) {
		re := toRPCError(rd, log, context.Canceled)
		d, _ := re.Arvel()
		if d.Code != protocol.ErrProviderError || d.VendorCode != "unclassified" {
			t.Fatalf("got %+v", d)
		}
	})
	t.Run("data_invalid_after_redaction_is_dropped", func(t *testing.T) {
		in := &protocol.RPCError{Code: -32000, Message: "x", Data: json.RawMessage(`{"a":"` + testSecret + `"}`)}
		rd2 := redact.New()
		rd2.Add(`{"a":"` + testSecret[:5]) // swallows structural characters
		re := toRPCError(rd2, log, in)
		if len(re.Data) != 0 && !json.Valid(re.Data) {
			t.Fatalf("invalid JSON data kept: %s", re.Data)
		}
	})
}

func TestOptions_Defaults(t *testing.T) {
	o := defaultOptions()
	if o.idle != DefaultIdleTimeout || o.routeTimeout != DefaultRouteTimeout ||
		o.initTimeout != DefaultInitTimeout || o.closeTimeout != DefaultCloseTimeout {
		t.Fatalf("defaults wrong: %+v", o)
	}
	if o.name != "arvel-plugin" || o.version != "0.0.0" || o.clock == nil {
		t.Fatalf("defaults wrong: %+v", o)
	}
}

func TestOptions_IgnoreNonPositive(t *testing.T) {
	o := defaultOptions()
	for _, opt := range []Option{WithIdleTimeout(0), WithRouteTimeout(-1), WithInitTimeout(0), WithCloseTimeout(-5), WithClock(nil), WithVersion("", "")} {
		opt(&o)
	}
	if o != func() options { d := defaultOptions(); d.clock = o.clock; return d }() {
		t.Fatalf("non-positive values must keep defaults: %+v", o)
	}
}

func TestOptions_Set(t *testing.T) {
	o := defaultOptions()
	WithIdleTimeout(time.Minute)(&o)
	WithVersion("x", "1.2.3")(&o)
	if o.idle != time.Minute || o.name != "x" || o.version != "1.2.3" {
		t.Fatalf("got %+v", o)
	}
}
