package queryengine

import (
	"fmt"
	"strconv"
	"strings"
)

type ErrorCategory string

const (
	ErrorCategoryRateLimit      ErrorCategory = "rate_limit"
	ErrorCategoryOverloaded     ErrorCategory = "overloaded"
	ErrorCategoryTimeout        ErrorCategory = "timeout"
	ErrorCategoryNetwork        ErrorCategory = "network"
	ErrorCategoryInvalidRequest ErrorCategory = "invalid_request"
	ErrorCategoryAuth           ErrorCategory = "auth"
	ErrorCategoryContextLength  ErrorCategory = "context_length"
	ErrorCategoryUnknown        ErrorCategory = "unknown"
)

type QueryEngineError struct {
	Message      string
	Category     ErrorCategory
	Retryable    bool
	RetryAfterMs int
}

func (e *QueryEngineError) Error() string {
	return e.Message
}

func NewQueryEngineError(message string, category ErrorCategory, retryable bool, retryafterms int) *QueryEngineError {
	return &QueryEngineError{
		Message:      message,
		Category:     category,
		Retryable:    retryable,
		RetryAfterMs: retryafterms,
	}
}

func ClassifyError(err error) *QueryEngineError {
	// If already a QueryEngineError, return it directly
	if qe, ok := err.(*QueryEngineError); ok {
		return qe
	}

	message := err.Error()
	status := extractStatusCode(err)

	// Rate limit (429)
	if status == 429 {
		retryAfter := extractRetryAfter(err, 5) * 1000
		return NewQueryEngineError("Rate limited", ErrorCategoryRateLimit, true, retryAfter)
	}

	// Overloaded (529 or 503)
	if status == 529 || status == 503 {
		return NewQueryEngineError("Overloaded", ErrorCategoryOverloaded, true, 10000)
	}

	// Invalid request (400)
	if status == 400 {
		if strings.Contains(message, "context") || strings.Contains(message, "token") {
			return NewQueryEngineError(message, ErrorCategoryContextLength, false, 0)
		}
		return NewQueryEngineError(message, ErrorCategoryInvalidRequest, false, 0)
	}

	// Auth failed (401 or 403)
	if status == 401 || status == 403 {
		return NewQueryEngineError("Auth failed", ErrorCategoryAuth, false, 0)
	}

	// Timeout
	if strings.Contains(message, "timeout") || strings.Contains(message, "ETIMEDOUT") {
		return NewQueryEngineError("Timeout", ErrorCategoryTimeout, true, 3000)
	}

	// Network error
	if strings.Contains(message, "ECONNREFUSED") || strings.Contains(message, "fetch failed") {
		return NewQueryEngineError("Network error", ErrorCategoryNetwork, true, 2000)
	}

	// Unknown error
	return NewQueryEngineError(message, ErrorCategoryUnknown, false, 0)
}

// extractStatusCode extracts HTTP status code from error
func extractStatusCode(err error) int {
	// Try to extract from common error types
	// This is a simplified implementation; real HTTP errors may have different structures
	if httpErr, ok := err.(interface{ StatusCode() int }); ok {
		return httpErr.StatusCode()
	}
	if httpErr, ok := err.(interface{ Status() int }); ok {
		return httpErr.Status()
	}
	return 0
}

// extractRetryAfter extracts retry-after header value from error
func extractRetryAfter(err error, defaultSeconds int) int {
	// Try to extract Retry-After header
	if httpErr, ok := err.(interface{ Header(key string) string }); ok {
		retryAfterStr := httpErr.Header("Retry-After")
		if retryAfterStr != "" {
			seconds, err := strconv.Atoi(retryAfterStr)
			if err == nil {
				return seconds
			}
		}
	}
	return defaultSeconds
}

// IsRetryable checks if an error is retryable
func IsRetryable(err error) bool {
	qe := ClassifyError(err)
	return qe.Retryable
}

// GetRetryAfterMs returns the retry delay in milliseconds
func GetRetryAfterMs(err error) int {
	qe := ClassifyError(err)
	return qe.RetryAfterMs
}

// GetCategory returns the error category
func GetCategory(err error) ErrorCategory {
	qe := ClassifyError(err)
	return qe.Category
}

// String returns the string representation of error category
func (c ErrorCategory) String() string {
	return string(c)
}

// FormatQueryEngineError formats a QueryEngineError with all details
func FormatQueryEngineError(err *QueryEngineError) string {
	return fmt.Sprintf("QueryEngineError: %s (category=%s, retryable=%v, retryAfterMs=%d)",
		err.Message, err.Category, err.Retryable, err.RetryAfterMs)
}
