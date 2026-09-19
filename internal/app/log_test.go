package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/zorneth/osg-runtime/logging"
	"github.com/zorneth/slogx"
)

func TestOpLogger_includesOpAttr(t *testing.T) {
	var buf bytes.Buffer
	log := slogx.New(
		slogx.WithOutput(&buf),
		slogx.WithFormat(slogx.FormatJSON),
		slogx.WithLevel(slog.LevelInfo),
	)
	ctx := logging.ToContext(context.Background(), log)

	opLogger(ctx, "cli.sandbox.create", slog.String("sandbox", "demo")).Info("creating sandbox")

	out := buf.String()
	if !strings.Contains(out, `"op":"cli.sandbox.create"`) {
		t.Fatalf("missing op attr in %s", out)
	}
	if !strings.Contains(out, `"sandbox":"demo"`) {
		t.Fatalf("missing sandbox attr in %s", out)
	}
	if !strings.Contains(out, "creating sandbox") {
		t.Fatalf("missing message in %s", out)
	}
}

func TestCliOp_doesNotPanic(t *testing.T) {
	cliOp("cli.test").Info("ok")
}
