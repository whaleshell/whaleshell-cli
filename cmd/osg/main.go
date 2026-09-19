package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/zorneth/osg-cli/internal/app"
	"github.com/zorneth/osg-cli/internal/cli"
	"github.com/zorneth/osg-runtime/logging"
)

func main() {
	ctx := context.Background()
	_ = logging.Setup(ctx, logging.Options{Service: "osg"})

	if err := cli.Execute(os.Args[1:]); err != nil {
		var ee *app.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
