package fetcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// instantCfg returns a RetryConfig with zero delays so tests run fast.
func instantCfg(maxAttempts int) config.RetryConfig {
	return config.RetryConfig{
		MaxAttempts:  maxAttempts,
		InitialDelay: config.Duration{},
		Multiplier:   1.0,
		MaxDelay:     config.Duration{},
	}
}

func TestWithRetry_ImmediateSuccess(t *testing.T) {
	var invocations int
	if err := WithRetry(context.Background(), instantCfg(3), func() error {
		invocations++
		return nil
	}); err != nil {
		t.Fatalf("unexpected error on immediate success: %v", err)
	}
	if invocations != 1 {
		t.Errorf("want 1 invocation, got %d", invocations)
	}
}

func TestWithRetry_EventualSuccess(t *testing.T) {
	var invocations int
	if err := WithRetry(context.Background(), instantCfg(5), func() error {
		invocations++
		if invocations < 4 {
			return errors.New("not yet")
		}
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if invocations != 4 {
		t.Errorf("want 4 invocations, got %d", invocations)
	}
}

func TestWithRetry_ExhaustedReturnsLastError(t *testing.T) {
	sentinel := errors.New("permanent failure")
	err := WithRetry(context.Background(), instantCfg(3), func() error {
		return sentinel
	})
	if err == nil {
		t.Fatal("expected an error after all attempts are exhausted")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel error, got: %v", err)
	}
}

func TestWithRetry_ContextCancelledBeforeBackoff(t *testing.T) {
	cfg := config.RetryConfig{
		MaxAttempts:  10,
		InitialDelay: config.Duration{Duration: 500 * time.Millisecond},
		Multiplier:   1.0,
		MaxDelay:     config.Duration{Duration: time.Second},
	}

	ctx, cancel := context.WithCancel(context.Background())

	var invocations int
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := WithRetry(ctx, cfg, func() error {
		invocations++
		return errors.New("transient")
	})

	if err == nil {
		t.Fatal("expected an error when context is cancelled")
	}
	if invocations == 0 {
		t.Error("expected at least one invocation before cancellation")
	}
	if invocations >= 10 {
		t.Errorf("context should have cancelled before all 10 attempts; got %d", invocations)
	}
}
