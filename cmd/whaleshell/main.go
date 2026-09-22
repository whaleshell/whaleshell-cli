package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/whaleshell/whaleshell-cli/internal/app"
	"github.com/whaleshell/whaleshell-cli/internal/logger"
	"github.com/whaleshell/whaleshell-cli/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args[1:]); err != nil {
		log := logger.FromContext(ctx)
		var ee *service.ExitError
		if errors.As(err, &ee) {
			log.Error("command failed", "exit_code", ee.Code, "error", err)
			os.Exit(ee.Code)
		}
		if errors.Is(err, context.Canceled) {
			log.Info("command canceled")
			os.Exit(130)
		}
		log.Error("command failed", "error", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
