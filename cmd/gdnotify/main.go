package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/mashiike/gdnotify"
)

var (
	Version = "current"
)

func main() {
	if code := _main(); code != 0 {
		os.Exit(code)
	}
}

func _main() int {
	var cli gdnotify.CLI
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer cancel()
	return cli.Run(ctx)
}
