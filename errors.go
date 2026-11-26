package bachs

import (
	"encoding/json"
	"fmt"
)

// ConfigError means the SDK was misused. Nothing was sent to Bachs.
type ConfigError struct {
	Msg string
	Err error
}

func (e *ConfigError) Error() string { return "bachs: " + e.Msg }
func (e *ConfigError) Unwrap() error { return e.Err }

// FieldError is one entry in a validation error's "errors" array.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// APIError means Bachs answered with a non-2xx status.
//
// Use errors.As to inspect it:
//
//	var apiErr *bachs.APIError
//	if errors.As(err, &apiErr) && apiErr.Code == "INSUFFICIENT_BALANCE" { ... }
type APIError struct {
	StatusCode  int
	Code        string // Bachs error_code, e.g. VALIDATION_ERROR
	Detail      string
	DocURL      string
	FieldErrors []FieldError
	RequestID   string
	RetryAfter  string
	// OutcomeUnknown is true when a write got a 5xx: Bachs may or may not have
	// applied it. Look the resource up before repeating the write, and repeat it
	// only with the same idempotency key.
	OutcomeUnknown bool
	Body           []byte
}

func (e *APIError) Error() string {
	msg := e.Detail
	if msg == "" {
		msg = "request failed"
	}
	s := fmt.Sprintf("bachs: %d", e.StatusCode)
	if e.Code != "" {
		s += " " + e.Code
	}
	s += ": " + msg
	if e.RequestID != "" {
		s += " (request " + e.RequestID + ")"
	}
	return s
}

func newAPIError(status int, body []byte, requestID, retryAfter string, write bool) *APIError {
	e := &APIError{StatusCode: status, Body: body, RequestID: requestID, RetryAfter: retryAfter, OutcomeUnknown: write && status >= 500}
	var parsed struct {
		Detail    string       `json:"detail"`
		ErrorCode string       `json:"error_code"`
		DocURL    string       `json:"doc_url"`
		Errors    []FieldError `json:"errors"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		e.Detail, e.Code, e.DocURL, e.FieldErrors = parsed.Detail, parsed.ErrorCode, parsed.DocURL, parsed.Errors
	}
	return e
}

// NetworkError means no usable response came back: a connection failure, a
// timeout, or cancellation. errors.Is works with context.DeadlineExceeded and
// context.Canceled.
type NetworkError struct {
	Err error
	// OutcomeUnknown is true for a write that may have reached Bachs.
	OutcomeUnknown bool
}

func (e *NetworkError) Error() string { return "bachs: no usable response: " + e.Err.Error() }
func (e *NetworkError) Unwrap() error { return e.Err }

// ResponseError means Bachs returned 2xx but the body could not be decoded.
// For a write, the operation most likely happened.
type ResponseError struct {
	StatusCode int
	RequestID  string
	Body       []byte
	Err        error
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("bachs: unusable response body (HTTP %d): %v", e.StatusCode, e.Err)
}
func (e *ResponseError) Unwrap() error { return e.Err }
