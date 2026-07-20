// Package apierrors defines HTTP-status-aware error values for cases Gofr's
// built-in error types do not cover (401/403/422). Gofr uses StatusCode() to
// pick the response status (ARCHITECTURE.md §8).
package apierrors

import "net/http"

// Error is an API error carrying its HTTP status code and, optionally, a
// machine-readable code surfaced to clients inside the error envelope.
type Error struct {
	code    int
	message string
	reason  string // machine-readable code, e.g. CodeLimitReached; "" for none
}

// Error implements the error interface.
func (e Error) Error() string { return e.message }

// StatusCode makes Gofr respond with the intended HTTP status.
func (e Error) StatusCode() int { return e.code }

// Response implements Gofr's ResponseMarshaller: when the error carries a
// machine-readable code, it is merged into the error envelope as
// {"error": {"code": ..., "message": ...}}.
func (e Error) Response() map[string]any {
	if e.reason == "" {
		return nil
	}

	return map[string]any{"code": e.reason}
}

// Unauthorized returns a 401 error (missing/invalid credentials).
func Unauthorized(msg string) Error { return Error{code: http.StatusUnauthorized, message: msg} }

// Forbidden returns a 403 error (authenticated but not allowed).
func Forbidden(msg string) Error { return Error{code: http.StatusForbidden, message: msg} }

// BadRequest returns a 400 error (malformed request values, e.g. a bad
// analytics date range for from > to).
func BadRequest(msg string) Error { return Error{code: http.StatusBadRequest, message: msg} }

// NotFound returns a 404 error.
func NotFound(msg string) Error { return Error{code: http.StatusNotFound, message: msg} }

// Conflict returns a 409 error.
func Conflict(msg string) Error { return Error{code: http.StatusConflict, message: msg} }

// Unprocessable returns a 422 error (semantically invalid input).
func Unprocessable(msg string) Error { return Error{code: http.StatusUnprocessableEntity, message: msg} }

// Gone returns a 410 error (a resource that existed but was withdrawn, e.g.
// a link disabled for abuse).
func Gone(msg string) Error { return Error{code: http.StatusGone, message: msg} }

// TooManyRequests returns a 429 error (rate limit exceeded).
func TooManyRequests(msg string) Error { return Error{code: http.StatusTooManyRequests, message: msg} }

// NotImplemented returns a 501 error (optional feature not configured, e.g.
// city autocomplete without GEOIP_CITIES_CSV).
func NotImplemented(msg string) Error { return Error{code: http.StatusNotImplemented, message: msg} }

// CodeLimitReached is the machine-readable code for a limits.Policy denial.
const CodeLimitReached = "LIMIT_REACHED"

// LimitReached returns a 403 whose body carries the machine-readable code
// LIMIT_REACHED plus the denying policy's message, passed through verbatim
// for the client to display.
func LimitReached(msg string) Error {
	return Error{code: http.StatusForbidden, message: msg, reason: CodeLimitReached}
}
