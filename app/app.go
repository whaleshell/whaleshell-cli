// Package app is the composition root for the whaleshell CLI binary.
package app

import (
	"context"

	"github.com/whaleshell/whaleshell-cli/app/cli"
	"github.com/whaleshell/whaleshell-cli/config"
	"github.com/whaleshell/whaleshell-cli/internal/logger"
)

// Run wires config + logger and executes the CLI.
func Run(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_ = config.Load()
	log := logger.Setup(ctx, logger.Options{Service: "whaleshell"})
	ctx = logger.ToContext(ctx, log)
	return cli.Execute(ctx, args)
}
