package service

import (
	"context"
	"time"
)

// SetCommandContext installs the per-invocation context (signal-aware from main).
// Nil resets to context.Background for tests.
func (a *App) SetCommandContext(ctx context.Context) {
	if a == nil {
		return
	}
	if ctx == nil {
		a.cmdCtx = context.Background()
		return
	}
	a.cmdCtx = ctx
}

// CommandContext returns the active command context (never nil).
func (a *App) CommandContext() context.Context {
	if a == nil || a.cmdCtx == nil {
		return context.Background()
	}
	return a.cmdCtx
}

// withTimeout derives a deadline from the command context.
func (a *App) withTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.CommandContext(), d)
}

// withCancel derives a cancelable child (interactive / follow streams).
func (a *App) withCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(a.CommandContext())
}

// apiCtx returns a TimeoutAPI child of the command context.
// The cancel is registered via AfterFunc so callers can pass it inline
// without a defer (timer is released when the ctx is done).
func (a *App) apiCtx() context.Context {
	ctx, cancel := a.withTimeout(TimeoutAPI)
	context.AfterFunc(ctx, cancel)
	return ctx
}
