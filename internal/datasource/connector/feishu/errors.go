package feishu

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Feishu business / HTTP codes that warrant retry.
const (
	// CodeLockContention is returned when a document/wiki resource is locked.
	CodeLockContention = 131009
	// CodeRateLimitedA / CodeRateLimitedB are common Feishu frequency-limit codes.
	CodeRateLimitedA = 99991400
	CodeRateLimitedB = 99991403
	// CodePermissionDenied is the wiki-specific permission failure.
	CodePermissionDenied = 131006
)

// Known permission-denied Feishu codes (non-exhaustive but covers publish paths).
var permissionDeniedCodes = map[int]struct{}{
	131006:   {}, // wiki permission denied
	99991663: {},
	99991668: {},
	99991672: {},
}

// Known not-found Feishu codes for wiki/doc nodes.
var notFoundCodes = map[int]struct{}{
	131004: {}, // wiki node not found (common)
	1770002: {}, // docx not found variants
}

var rateLimitCodes = map[int]struct{}{
	CodeLockContention: {},
	CodeRateLimitedA:   {},
	CodeRateLimitedB:   {},
	99991401:           {},
}

// secretLikeRE matches bearer tokens and long opaque secrets in error text.
var secretLikeRE = regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9\-_.]+|(app_secret|tenant_access_token|user_access_token)(=|":\s*")[^"\s&]+`)

// APIError is a typed Feishu API failure with sanitized message text.
type APIError struct {
	HTTPStatus int
	Code       int
	Msg        string
	Path       string
}

func (e *APIError) Error() string {
	if e == nil {
		return "feishu api error"
	}
	msg := SanitizeErrorMessage(e.Msg)
	if e.Code != 0 {
		return fmt.Sprintf("feishu api error: http=%d code=%d msg=%s", e.HTTPStatus, e.Code, msg)
	}
	return fmt.Sprintf("feishu api error: http=%d msg=%s", e.HTTPStatus, msg)
}

// RetryableError marks transient failures (HTTP 429, rate limits, lock contention).
type RetryableError struct {
	*APIError
	Attempts int
}

func (e *RetryableError) Error() string {
	if e == nil || e.APIError == nil {
		return "feishu retryable error"
	}
	return fmt.Sprintf("feishu retryable error (attempts=%d): %s", e.Attempts, e.APIError.Error())
}

func (e *RetryableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.APIError
}

// PermissionDeniedError indicates the app lacks access to a wiki/doc resource.
type PermissionDeniedError struct {
	*APIError
}

func (e *PermissionDeniedError) Error() string {
	if e == nil || e.APIError == nil {
		return "feishu permission denied"
	}
	return fmt.Sprintf("feishu permission denied: %s", e.APIError.Error())
}

func (e *PermissionDeniedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.APIError
}

// NotFoundError indicates a wiki/doc node no longer exists remotely.
type NotFoundError struct {
	*APIError
}

func (e *NotFoundError) Error() string {
	if e == nil || e.APIError == nil {
		return "feishu resource not found"
	}
	return fmt.Sprintf("feishu not found: %s", e.APIError.Error())
}

func (e *NotFoundError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.APIError
}

// IsRetryable reports whether err (or any wrapped error) is transient.
func IsRetryable(err error) bool {
	var re *RetryableError
	return errors.As(err, &re)
}

// IsPermissionDenied reports whether err is a permission failure.
func IsPermissionDenied(err error) bool {
	var pe *PermissionDeniedError
	if errors.As(err, &pe) {
		return true
	}
	var ae *APIError
	if errors.As(err, &ae) && isPermissionDeniedCode(ae.Code) {
		return true
	}
	return false
}

// IsNotFound reports whether err means the remote wiki/doc node is gone.
func IsNotFound(err error) bool {
	var ne *NotFoundError
	if errors.As(err, &ne) {
		return true
	}
	var ae *APIError
	if errors.As(err, &ae) {
		if ae.HTTPStatus == http.StatusNotFound || isNotFoundCode(ae.Code) {
			return true
		}
		msg := strings.ToLower(ae.Msg)
		if strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") || strings.Contains(msg, "node_not_exist") {
			return true
		}
	}
	return false
}

func isPermissionDeniedCode(code int) bool {
	_, ok := permissionDeniedCodes[code]
	return ok
}

func isNotFoundCode(code int) bool {
	_, ok := notFoundCodes[code]
	return ok
}

func isRetryableCode(httpStatus, code int) bool {
	if httpStatus == http.StatusTooManyRequests {
		return true
	}
	if _, ok := rateLimitCodes[code]; ok {
		return true
	}
	return false
}

// MapAPIError converts a Feishu business code into a typed error.
// Returns nil when code == 0.
func MapAPIError(httpStatus, code int, msg, path string) error {
	if code == 0 && (httpStatus == 0 || httpStatus == http.StatusOK) {
		return nil
	}
	ae := &APIError{
		HTTPStatus: httpStatus,
		Code:       code,
		Msg:        SanitizeErrorMessage(msg),
		Path:       path,
	}
	if isRetryableCode(httpStatus, code) {
		return &RetryableError{APIError: ae}
	}
	if isPermissionDeniedCode(code) {
		return &PermissionDeniedError{APIError: ae}
	}
	if httpStatus == http.StatusNotFound || isNotFoundCode(code) {
		return &NotFoundError{APIError: ae}
	}
	msgLower := strings.ToLower(ae.Msg)
	if strings.Contains(msgLower, "not found") || strings.Contains(msgLower, "does not exist") || strings.Contains(msgLower, "node_not_exist") {
		return &NotFoundError{APIError: ae}
	}
	return ae
}

// SanitizeErrorMessage redacts secrets/tokens and truncates long payloads
// so errors are safe to log or return to callers.
func SanitizeErrorMessage(s string) string {
	if s == "" {
		return ""
	}
	out := secretLikeRE.ReplaceAllStringFunc(s, func(m string) string {
		lower := strings.ToLower(m)
		switch {
		case strings.HasPrefix(lower, "bearer "):
			return "Bearer [REDACTED]"
		case strings.Contains(lower, "app_secret"):
			return "app_secret=[REDACTED]"
		case strings.Contains(lower, "tenant_access_token"):
			return "tenant_access_token=[REDACTED]"
		case strings.Contains(lower, "user_access_token"):
			return "user_access_token=[REDACTED]"
		default:
			return "[REDACTED]"
		}
	})
	if len(out) > 256 {
		out = out[:256] + "..."
	}
	return out
}
