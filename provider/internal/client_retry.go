package internal

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

const (
	maxAttempts       = 4
	backoffBase       = 250 * time.Millisecond
	backoffMultiplier = 2
	backoffPerWaitCap = 8 * time.Second
	serverWaitCap     = 30 * time.Second
	retryAfterHeader  = "Retry-After"
)

// shouldRetry reads the envelope signal first: a false retryable is terminal at any status.
// The published ceiling of 150 requests per second needs no rate limiter, only this reaction.
func shouldRetry(apiErr *APIError) bool {
	if apiErr.Retryable != nil && !*apiErr.Retryable {
		return false
	}
	return apiErr.StatusCode == http.StatusTooManyRequests ||
		apiErr.StatusCode >= http.StatusInternalServerError
}

// retryWait honours a wait the server named and falls back to jittered exponential backoff.
func retryWait(attempt int, resp *http.Response, apiErr *APIError) time.Duration {
	if wait, found := serverWait(resp.Header.Get(retryAfterHeader), apiErr.RetryAfterSeconds); found {
		return wait
	}
	return fullJitter(backoffWait(attempt))
}

// backoffWait doubles from 250 milliseconds and stops at the per-wait cap.
func backoffWait(attempt int) time.Duration {
	wait := backoffBase
	for range attempt - 1 {
		wait *= backoffMultiplier
		if wait >= backoffPerWaitCap {
			return backoffPerWaitCap
		}
	}
	return wait
}

// fullJitter spreads the retry across the whole computed window.
func fullJitter(computed time.Duration) time.Duration {
	if computed <= 0 {
		return 0
	}
	// A retry wait is not a secret, so the fast generator is the right one.
	return time.Duration(rand.Int64N(int64(computed) + 1)) //nolint:gosec
}

// serverWait reads the wait the server asked for, from the header first and the envelope second.
// The header carries either a second count or an HTTP date.
func serverWait(header string, envelopeSeconds *int) (time.Duration, bool) {
	if header != "" {
		if seconds, err := strconv.Atoi(header); err == nil {
			return capServerWait(time.Duration(seconds) * time.Second), true
		}
		if when, err := http.ParseTime(header); err == nil {
			return capServerWait(time.Until(when)), true
		}
	}
	if envelopeSeconds != nil {
		return capServerWait(time.Duration(*envelopeSeconds) * time.Second), true
	}
	return 0, false
}

func capServerWait(wait time.Duration) time.Duration {
	if wait < 0 {
		return 0
	}
	if wait > serverWaitCap {
		return serverWaitCap
	}
	return wait
}

// waitFor waits, and stops early when the context ends.
func waitFor(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
