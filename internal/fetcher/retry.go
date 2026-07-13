package fetcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// WithRetry executes fn up to cfg.MaxAttempts times.
// On each failure it waits for an exponentially increasing delay before retrying.
// The delay starts at cfg.InitialDelay, is multiplied by cfg.Multiplier after
// each attempt, and is capped at cfg.MaxDelay.
//
// Context cancellation is respected between retry attempts: if ctx is cancelled
// while waiting, WithRetry returns immediately with a wrapped context error.
func WithRetry(ctx context.Context, cfg config.RetryConfig, fn func() error) error {
	delay := cfg.InitialDelay.Duration
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			if attempt == cfg.MaxAttempts {
				break
			}
			slog.WarnContext(ctx, "request failed, will retry",
				"attempt", attempt,
				"max_attempts", cfg.MaxAttempts,
				"next_delay", delay,
				"error", err,
			)
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while waiting for retry: %w", ctx.Err())
			case <-time.After(delay):
			}
			// Compute next delay with cap.
			next := time.Duration(float64(delay) * cfg.Multiplier)
			if cfg.MaxDelay.Duration > 0 && next > cfg.MaxDelay.Duration {
				next = cfg.MaxDelay.Duration
			}
			delay = next
			continue
		}
		return nil
	}

	return fmt.Errorf("all %d attempt(s) failed; last error: %w", cfg.MaxAttempts, lastErr)
}
