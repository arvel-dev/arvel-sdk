// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// ErrorCode is the machine-readable Arvel error code (PROTOCOL §9).
type ErrorCode string

const (
	ErrRateLimited      ErrorCode = "rate_limited"
	ErrAuthRequired     ErrorCode = "auth_required"
	ErrBinaryMissing    ErrorCode = "binary_missing"
	ErrBinaryOutdated   ErrorCode = "binary_outdated"
	ErrModelUnavailable ErrorCode = "model_unavailable"
	ErrBudgetExceeded   ErrorCode = "budget_exceeded"
	ErrContextOverflow  ErrorCode = "context_overflow"
	ErrOverloaded       ErrorCode = "overloaded"
	ErrBusy             ErrorCode = "busy"
	ErrProviderError    ErrorCode = "provider_error"
	ErrInternal         ErrorCode = "internal"
)

// Valid reports whether c is a defined ErrorCode.
func (c ErrorCode) Valid() bool {
	switch c {
	case ErrRateLimited, ErrAuthRequired, ErrBinaryMissing, ErrBinaryOutdated, ErrModelUnavailable,
		ErrBudgetExceeded, ErrContextOverflow, ErrOverloaded, ErrBusy, ErrProviderError, ErrInternal:
		return true
	}
	return false
}

// AllErrorCodes returns every defined ErrorCode in protocol order.
func AllErrorCodes() []ErrorCode {
	return []ErrorCode{ErrRateLimited, ErrAuthRequired, ErrBinaryMissing, ErrBinaryOutdated, ErrModelUnavailable,
		ErrBudgetExceeded, ErrContextOverflow, ErrOverloaded, ErrBusy, ErrProviderError, ErrInternal}
}

// JSON-RPC and ACP numeric error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	// CodeServerError is the ACP code carried by every Arvel error.
	CodeServerError = -32000
	// CodeBusy is used for the "busy" error (PROTOCOL §8).
	CodeBusy = -32002
)

// RPCCode returns the JSON-RPC numeric code used for c.
func (c ErrorCode) RPCCode() int {
	if c == ErrBusy {
		return CodeBusy
	}
	return CodeServerError
}

// ErrorData is the body of "error.data.arvel" and of "_meta.arvel.stop".
type ErrorData struct {
	Code              ErrorCode `json:"code"`
	Message           string    `json:"message,omitempty"`
	Retryable         bool      `json:"retryable,omitempty"`
	ResetsAt          time.Time `json:"resetsAt,omitzero"`
	WindowID          string    `json:"windowId,omitempty"`
	MinVersion        string    `json:"minVersion,omitempty"`
	FoundVersion      string    `json:"foundVersion,omitempty"`
	Model             string    `json:"model,omitempty"`
	Limit             string    `json:"limit,omitempty"` // "turns" or "usd"
	RetryAfterSeconds int       `json:"retryAfterSeconds,omitempty"`
	VendorCode        string    `json:"vendorCode,omitempty"`
}

// StopDetail is the turn-level outcome in "_meta.arvel.stop"; it has the same
// shape as ErrorData (PROTOCOL §9).
type StopDetail = ErrorData

func missing(code ErrorCode, field string) error {
	return fmt.Errorf("protocol: error %q: missing field %q", code, field)
}

// Validate checks that the code is defined and that the fields required for
// it by PROTOCOL §9 are present.
func (d ErrorData) Validate() error {
	if !d.Code.Valid() {
		return fmt.Errorf("protocol: unknown error code %q", d.Code)
	}
	switch d.Code {
	case ErrBinaryOutdated:
		if d.MinVersion == "" {
			return missing(d.Code, "minVersion")
		}
		if d.FoundVersion == "" {
			return missing(d.Code, "foundVersion")
		}
	case ErrModelUnavailable:
		if d.Model == "" {
			return missing(d.Code, "model")
		}
	case ErrBudgetExceeded:
		if d.Limit != "turns" && d.Limit != "usd" {
			return missing(d.Code, "limit")
		}
	case ErrProviderError:
		if d.VendorCode == "" {
			return missing(d.Code, "vendorCode")
		}
		if d.Message == "" {
			return missing(d.Code, "message")
		}
	case ErrInternal:
		if d.Message == "" {
			return missing(d.Code, "message")
		}
	}
	return nil
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Arvel decodes the "arvel" member of the error data. It reports false when
// the data is absent or malformed.
func (e *RPCError) Arvel() (ErrorData, bool) {
	if len(e.Data) == 0 {
		return ErrorData{}, false
	}
	var wrap struct {
		Arvel *ErrorData `json:"arvel"`
	}
	if err := json.Unmarshal(e.Data, &wrap); err != nil || wrap.Arvel == nil {
		return ErrorData{}, false
	}
	return *wrap.Arvel, true
}

// NewError builds a JSON-RPC error with the ACP code (-32000, or -32002 for
// "busy") and "data.arvel" holding the code, the message and fields. The
// code and message arguments win over same-named keys in fields.
//
// NewError does not redact msg: callers pass text that is already free of
// secrets (the plugin framework redacts vendor text before it gets here).
func NewError(code ErrorCode, msg string, fields map[string]any) *RPCError {
	arvel := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		if k == "code" || k == "message" {
			continue
		}
		arvel[k] = v
	}
	arvel["code"] = code
	arvel["message"] = msg
	data, err := json.Marshal(map[string]any{"arvel": arvel})
	if err != nil {
		fallback, _ := json.Marshal(map[string]any{"arvel": map[string]any{
			"code": ErrInternal, "message": "unmarshalable error fields",
		}})
		return &RPCError{Code: CodeServerError, Message: msg, Data: fallback}
	}
	return &RPCError{Code: code.RPCCode(), Message: msg, Data: data}
}

// NewMethodNotFound builds the -32601 error for an unknown method.
func NewMethodNotFound(method string) *RPCError {
	return &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + method}
}
