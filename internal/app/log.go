package app

import (
	"context"
	"log/slog"

	"github.com/zorneth/osg-runtime/logging"
	"github.com/zorneth/slogx"
)

// opLogger returns a context logger tagged with a stable operation name
// (same convention as gateway/agent and team-tasks: package.resource.action).
func opLogger(ctx context.Context, op string, attrs ...any) *slogx.Logger {
	if ctx == nil {
		ctx = context.TODO()
	}
	args := make([]any, 0, 1+len(attrs))
	args = append(args, slog.String("op", op))
	args = append(args, attrs...)
	return logging.FromContext(ctx).With(args...)
}

// cliOp is a convenience for CLI methods that do not carry a request context.
func cliOp(op string, attrs ...any) *slogx.Logger {
	return opLogger(context.TODO(), op, attrs...)
}
