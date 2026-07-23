package queryengine

import (
	"math"
	"math/rand/v2"
	"time"
)

type RetryOptions struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

var (
	defaultMaxRetries = 3
	defaultBaseDelay  = time.Second
	defaultMaxDelay   = 30 * time.Second
)

func NewRetryOptions(maxRetries int, baseDelay, maxDelay time.Duration) *RetryOptions {
	return &RetryOptions{
		MaxRetries: maxRetries,
		BaseDelay:  baseDelay,
		MaxDelay:   maxDelay,
	}
}

func WithRetry(fn func() error, opts *RetryOptions) error {
	maxRetries := defaultMaxRetries
	baseDelay := defaultBaseDelay
	maxDelay := defaultMaxDelay

	if opts != nil {
		if opts.MaxRetries > 0 {
			maxRetries = opts.MaxRetries
		}
		if opts.BaseDelay > 0 {
			baseDelay = opts.BaseDelay
		}
		if opts.MaxDelay > 0 {
			maxDelay = opts.MaxDelay
		}
	}

	var lastError *QueryEngineError

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastError = ClassifyError(err)

		if !lastError.Retryable || attempt == maxRetries {
			return lastError
		}

		var delay time.Duration
		if lastError.RetryAfterMs > 0 {
			delay = time.Duration(lastError.RetryAfterMs) * time.Millisecond
		} else {
			jitter := rand.Float64()*0.3 + 0.85
			delay = time.Duration(float64(baseDelay) * jitter * math.Pow(2, float64(attempt)))

		}

		if delay > maxDelay {
			delay = maxDelay
		}

		time.Sleep(delay)

	}

	return lastError
}

func WithRetryResult[T any](fn func() (T, error), opts *RetryOptions) (T, error) {
	maxRetries := defaultMaxRetries
	baseDelay := defaultBaseDelay
	maxDelay := defaultMaxDelay

	if opts != nil {
		if opts.MaxRetries > 0 {
			maxRetries = opts.MaxRetries
		}
		if opts.BaseDelay > 0 {
			baseDelay = opts.BaseDelay
		}
		if opts.MaxDelay > 0 {
			maxDelay = opts.MaxDelay
		}
	}

	var lastError *QueryEngineError

	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastError = ClassifyError(err)

		if !lastError.Retryable || attempt == maxRetries {
			var zero T
			return zero, lastError
		}

		var delay time.Duration
		if lastError.RetryAfterMs > 0 {
			delay = time.Duration(lastError.RetryAfterMs) * time.Millisecond
		} else {
			jitter := rand.Float64()*0.3 + 0.85
			delay = time.Duration(float64(baseDelay) * jitter * math.Pow(2, float64(attempt)))
		}

		if delay > maxDelay {
			delay = maxDelay
		}

		time.Sleep(delay)
	}

	var zero T
	return zero, lastError
}
