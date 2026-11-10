package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// maxRetryWait is the longest Retry-After a create waits out; a longer one fails at once.
const maxRetryWait = 10 * time.Second

const defaultRetryWait = time.Second

type createOptions struct {
	withoutKey bool
}

type CreateOption func(*createOptions)

// WithoutIdempotencyKey sends the create without an Idempotency-Key and without a retry, as dry runs need.
func WithoutIdempotencyKey() CreateOption {
	return func(options *createOptions) { options.withoutKey = true }
}

func newIdempotencyKey() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate idempotency key: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

// doCreate retries a keyed create once on 429 only: the API stores no 5xx answer, so a repeat could create twice.
func (c Client) doCreate(ctx context.Context, request call) (rawResponse, error) {
	response, err := c.do(ctx, request)
	var httpErr *HTTPError
	if request.idempotencyKey == "" || !errors.As(err, &httpErr) || httpErr.Status != http.StatusTooManyRequests || httpErr.RetryAfter > maxRetryWait {
		return response, err
	}
	wait := httpErr.RetryAfter
	if wait <= 0 {
		wait = defaultRetryWait
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
