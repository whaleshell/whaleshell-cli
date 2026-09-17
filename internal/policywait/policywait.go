// Package policywait implements OpenShell-like policy set --wait.
package policywait

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Options controls settle polling after a policy file write.
type Options struct {
	// Settle is the minimum time to wait for proxy fsnotify reload (default 1s).
	Settle time.Duration
	// Timeout caps total wait (default 60s).
	Timeout time.Duration
	// Poll is the content re-check interval (default 200ms).
	Poll time.Duration
	// Now / Sleep are injectable for tests.
	Now   func() time.Time
	Sleep func(context.Context, time.Duration) error
}

func (o Options) withDefaults() Options {
	if o.Settle <= 0 {
		o.Settle = time.Second
	}
	if o.Timeout <= 0 {
		o.Timeout = 60 * time.Second
	}
	if o.Poll <= 0 {
		o.Poll = 200 * time.Millisecond
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	return o
}

// ErrTimeout is returned when FileApplied exceeds Options.Timeout.
var ErrTimeout = errors.New("policy wait: timeout")

// FileApplied waits until path content matches want and settle elapsed.
func FileApplied(ctx context.Context, path string, want []byte, opt Options) error {
	opt = opt.withDefaults()
	sumWant := sha256.Sum256(want)
	deadline := opt.Now().Add(opt.Timeout)
	settleUntil := opt.Now().Add(opt.Settle)

	for {
		raw, err := os.ReadFile(path)
		matched := err == nil && sha256.Sum256(raw) == sumWant
		settled := !opt.Now().Before(settleUntil)
		if matched && settled {
			return nil
		}
		if opt.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("%w: read %s: %v", ErrTimeout, path, err)
			}
			return fmt.Errorf("%w waiting for %s", ErrTimeout, path)
		}
		remain := deadline.Sub(opt.Now())
		sleep := opt.Poll
		if sleep > remain {
			sleep = remain
		}
		if sleep <= 0 {
			return fmt.Errorf("%w waiting for %s", ErrTimeout, path)
		}
		if err := opt.Sleep(ctx, sleep); err != nil {
			return err
		}
	}
}

// ExitCode maps wait errors to OpenShell-like codes: 124 timeout, 1 other.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return 124
	}
	if strings.Contains(err.Error(), "timeout") {
		return 124
	}
	return 1
}
