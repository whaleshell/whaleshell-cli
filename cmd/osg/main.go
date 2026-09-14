package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/zorneth/osg-cli/internal/app"
	"github.com/zorneth/osg-cli/internal/cli"
)

func main() {
	if err := cli.Execute(os.Args[1:]); err != nil {
		var ee *app.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
