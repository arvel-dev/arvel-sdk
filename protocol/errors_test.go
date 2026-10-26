// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestErrorCodeEnum(t *testing.T) {
	checkEnum(t, []ErrorCode{"rate_limited", "auth_required", "binary_missing", "binary_outdated",
		"model_unavailable", "budget_exceeded", "context_overflow", "overloaded", "busy",
		"provider_error", "internal"}, AllErrorCodes())
}

func TestRPCCode(t *testing.T) {
	for _, c := range AllErrorCodes() {
		want := CodeServerError
		if c == ErrBusy {
			want = CodeBusy
		}
		if got := NewError(c, "x", nil).Code; got != want {
			t.Errorf("%s: code %d, want %d", c, got, want)
		}
	}
}

func TestNewErrorShape(t *testing.T) {
	e := NewError(ErrRateLimited, "5-hour window exhausted", map[string]any{
		"retryable": true, "resetsAt": "2026-10-25T18:40:00Z", "windowId": "five_hour",
	})
	var got map[string]any
	if err := json.Unmarshal(e.Data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"arvel": map[string]any{
		"code": "rate_limited", "retryable": true, "resetsAt": "2026-10-25T18:40:00Z",
		"windowId": "five_hour", "message": "5-hour window exhausted",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("data = %v, want %v", got, want)
	}
	d, ok := e.Arvel()
	if !ok || d.Code != ErrRateLimited || !d.Retryable || d.WindowID != "five_hour" {
		t.Fatalf("Arvel() = %+v, %v", d, ok)
	}
	if !d.ResetsAt.Equal(time.Date(2026, 10, 25, 18, 40, 0, 0, time.UTC)) {
		t.Errorf("ResetsAt = %v", d.ResetsAt)
	}
	if e.Error() == "" {
		t.Error("empty Error()")
	}
}

func TestNewErrorArgumentsWin(t *testing.T) {
	e := NewError(ErrAuthRequired, "real", map[string]any{"code": "internal", "message": "fake"})
	d, ok := e.Arvel()
	if !ok || d.Code != ErrAuthRequired || d.Message != "real" {
		t.Fatalf("got %+v", d)
	}
}

func TestNewErrorNilFieldsAndBadValue(t *testing.T) {
	if _, ok := NewError(ErrBusy, "busy", nil).Arvel(); !ok {
		t.Error("nil fields should work")
	}
	e := NewError(ErrOverloaded, "x", map[string]any{"bad": make(chan int)})
	d, ok := e.Arvel()
	if !ok || d.Code != ErrInternal || e.Code != CodeServerError {
		t.Fatalf("fallback = %+v %v code=%d", d, ok, e.Code)
	}
}

func TestArvelMalformed(t *testing.T) {
	for _, data := range []string{"", "{}", `{"arvel":5}`, "not json", `{"arvel":null}`} {
		e := &RPCError{Data: json.RawMessage(data)}
		if _, ok := e.Arvel(); ok {
			t.Errorf("%q: want false", data)
		}
	}
}

func TestNewMethodNotFound(t *testing.T) {
	e := NewMethodNotFound("_arvel/nope")
	if e.Code != CodeMethodNotFound || e.Data != nil {
		t.Fatalf("got %+v", e)
	}
}

func TestErrorDataValidate(t *testing.T) {
	ok := []ErrorData{
		{Code: ErrRateLimited}, {Code: ErrAuthRequired}, {Code: ErrBinaryMissing}, {Code: ErrContextOverflow},
		{Code: ErrOverloaded}, {Code: ErrBusy},
		{Code: ErrBinaryOutdated, MinVersion: "2.0.0", FoundVersion: "1.0.0"},
		{Code: ErrModelUnavailable, Model: "m"},
		{Code: ErrBudgetExceeded, Limit: "turns"}, {Code: ErrBudgetExceeded, Limit: "usd"},
		{Code: ErrProviderError, VendorCode: "v", Message: "m"},
		{Code: ErrInternal, Message: "bug"},
	}
	for _, d := range ok {
		if err := d.Validate(); err != nil {
			t.Errorf("%+v: %v", d, err)
		}
	}
	bad := []ErrorData{
		{}, {Code: "nope"},
		{Code: ErrBinaryOutdated, MinVersion: "2.0.0"}, {Code: ErrBinaryOutdated, FoundVersion: "1"},
		{Code: ErrModelUnavailable}, {Code: ErrBudgetExceeded}, {Code: ErrBudgetExceeded, Limit: "money"},
		{Code: ErrProviderError, Message: "m"}, {Code: ErrProviderError, VendorCode: "v"},
		{Code: ErrInternal},
	}
	for _, d := range bad {
		if d.Validate() == nil {
			t.Errorf("%+v: want error", d)
		}
	}
}

type metaProbe struct {
	Name string `json:"name"`
}

func TestMetaFrom(t *testing.T) {
	if v, ok, err := MetaFrom[metaProbe](nil); ok || err != nil || v != (metaProbe{}) {
		t.Errorf("nil map: %v %v %v", v, ok, err)
	}
	if _, ok, err := MetaFrom[metaProbe](map[string]any{"other": 1}); ok || err != nil {
		t.Errorf("absent: %v %v", ok, err)
	}
	v, ok, err := MetaFrom[metaProbe](map[string]any{"arvel": map[string]any{"name": "x"}})
	if err != nil || !ok || v.Name != "x" {
		t.Errorf("valid: %v %v %v", v, ok, err)
	}
	_, ok, err = MetaFrom[metaProbe](map[string]any{"arvel": 5})
	var ute *json.UnmarshalTypeError
	if ok || !errors.As(err, &ute) {
		t.Errorf("wrong type: ok=%v err=%v", ok, err)
	}
	if _, _, err := MetaFrom[metaProbe](map[string]any{"arvel": make(chan int)}); err == nil {
		t.Error("unmarshalable value should fail")
	}
}

func TestWithMeta(t *testing.T) {
	in := map[string]any{"other": 1}
	out := WithMeta(in, metaProbe{Name: "x"})
	if _, ok := in["arvel"]; ok {
		t.Fatal("input mutated")
	}
	if out["other"] != 1 {
		t.Error("other namespace lost")
	}
	if got := WithMeta(nil, metaProbe{}); len(got) != 1 {
		t.Errorf("nil input: %v", got)
	}
	v, ok, err := MetaFrom[metaProbe](out)
	if err != nil || !ok || v.Name != "x" {
		t.Errorf("round trip: %v %v %v", v, ok, err)
	}
}
