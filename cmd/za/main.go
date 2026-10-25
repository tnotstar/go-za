// Command za automates the management of private multi-repository
// AI/development meta-workspaces.
package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/tnotstar/go-za/internal/cli"
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
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, cli.Env{
		Runner: tool.Exec{},
		Getwd:  os.Getwd,
		Now:    time.Now,
	})
	stop()
	os.Exit(code)
}
