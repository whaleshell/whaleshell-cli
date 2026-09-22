package service

import (
	"context"
	"testing"
	"time"
)

func TestCommandContext_defaultsAndSet(t *testing.T) {
	a := New()
	if a.CommandContext() == nil {
		t.Fatal("CommandContext nil")
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.SetCommandContext(parent)
	if a.CommandContext() != parent {
		t.Fatal("SetCommandContext not applied")
	}

	ctx, stop := a.withTimeout(20 * time.Millisecond)
	defer stop()
	select {
	case <-ctx.Done():
		// ok
	case <-time.After(time.Second):
		t.Fatal("withTimeout did not fire")
	}
}

func TestAPICtx_inheritsCancel(t *testing.T) {
	a := New()
	parent, cancel := context.WithCancel(context.Background())
	a.SetCommandContext(parent)
	ctx := a.apiCtx()
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("apiCtx did not observe parent cancel")
	}
}
