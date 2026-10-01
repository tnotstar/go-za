// Command za automates the management of private multi-repository
// AI/development meta-workspaces.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/tnotstar/go-za/internal/cli"
	"github.com/tnotstar/go-za/internal/initcmd"
	"github.com/tnotstar/go-za/internal/tool"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		// After the first interrupt starts cancellation and cleanup, restore
		// default handling so a second interrupt terminates immediately.
		<-ctx.Done()
		stop()
	}()
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, initcmd.Env{
		Runner: tool.Exec{},
		Getwd:  os.Getwd,
	})
	stop()
	os.Exit(code)
}
