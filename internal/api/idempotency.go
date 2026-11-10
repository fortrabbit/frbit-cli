package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// maxRetryWait is the longest Retry-After the client waits out; a longer one fails at once.
const maxRetryWait = time.Minute

const defaultRetryWait = time.Second

func newIdempotencyKey() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate idempotency key: %w", err)
	}
	return fmt.Sprintf("%x", value), nil
}

// doRetry waits out one 429 on a GET or a keyed create; the API stores no 5xx answer, so a 5xx is never repeated.
func (c Client) doRetry(ctx context.Context, request call) (rawResponse, error) {
	response, err := c.do(ctx, request)
	var httpErr *HTTPError
	safe := request.method == http.MethodGet || request.idempotencyKey != ""
	if !safe || !errors.As(err, &httpErr) || httpErr.Status != http.StatusTooManyRequests || httpErr.RetryAfter > maxRetryWait {
		return response, err
	}
	wait := httpErr.RetryAfter
	if wait <= 0 {
		wait = defaultRetryWait
	}
	if c.Warnings != nil {
		_, _ = fmt.Fprintf(c.Warnings, "rate limited by the API; retrying in %s\n", wait)
	}
	sleep := c.sleep
	if sleep == nil {
		sleep = sleepContext
	}
	if sleepErr := sleep(ctx, wait); sleepErr != nil {
		return rawResponse{}, sleepErr
	}
	return c.do(ctx, request)
}

func sleepContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
