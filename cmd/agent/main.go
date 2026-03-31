// Package vli implements the root level command for the disruptor agent CLI
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/danhngo-lx/xk6-disruptor/cmd/agent/commands"
	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
)

func main() {
	env := runtime.DefaultEnvironment()

	rootCmd := commands.NewRootCommand(env)

	if err := rootCmd.Execute(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
