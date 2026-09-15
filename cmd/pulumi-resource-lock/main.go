package main

import (
	"context"
	"fmt"
	"os"

	"github.com/use-lock/pulumi/provider"
)

var version = "0.1.0"

func main() {
	p, err := provider.New()
	if err == nil {
		err = p.Run(context.Background(), "lock", version)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
