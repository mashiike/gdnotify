package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/fatih/color"
	"github.com/fujiwara/logutils"
	flagx "github.com/ken39arg/go-flagx"
	"github.com/mashiike/didumean"
	"github.com/mashiike/gdnotify"
)

var (
	Version = "current"
)

func main() {
	if err := _main(); err != nil {
		log.Fatalln("[error]", err)
	}
}

func _main() error {
	flag.CommandLine.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "gdnotify [options]")
		fmt.Fprintln(flag.CommandLine.Output(), "version:", Version)
		flag.CommandLine.PrintDefaults()
	}
	var (
		configs  = flagx.StringSlice([]string{})
		port     int
		mode     string
		minLevel string
	)

	flag.Var(&configs, "config", "config list")
	flag.IntVar(&port, "port", 0, "webhook httpd port")
	flag.StringVar(&mode, "mode", gdnotify.RunModeValues()[0].String(), fmt.Sprintf(
		"run mode (%s)",
		strings.Join(gdnotify.RunModeStrings(), "|"),
	))
	flag.StringVar(&minLevel, "log-level", "info", "run mode")
	flag.VisitAll(flagx.EnvToFlagWithPrefix("GDNOTIFY_"))
	didumean.Parse()

	filter := &logutils.LevelFilter{
		Levels: []logutils.LogLevel{"debug", "info", "notice", "warn", "error"},
		ModifierFuncs: []logutils.ModifierFunc{
			logutils.Color(color.FgHiBlack),
			nil,
			logutils.Color(color.FgHiBlue),
			logutils.Color(color.FgYellow),
			logutils.Color(color.FgRed, color.BgBlack),
		},
		MinLevel: logutils.LogLevel(strings.ToLower(minLevel)),
		Writer:   os.Stdout,
	}
	log.SetOutput(filter)
	if minLevel == "debug" {
		log.SetFlags(log.Lshortfile)
	}

	cfg := gdnotify.DefaultConfig()
	if len(configs) > 0 {
		if err := cfg.Load(configs...); err != nil {
			return err
		}
	}
	if err := cfg.ValidateVersion(Version); err != nil {
		return err
	}
	app, err := gdnotify.New(cfg)
	if err != nil {
		return err
	}
	defer app.Close()
	optFns := make([]func(*gdnotify.RunOptions) error, 0)
	if port > 0 {
		optFns = append(optFns, gdnotify.WithLocalAddress(fmt.Sprintf(":%d", port)))
	}
	if mode != "" {
		optFns = append(optFns, gdnotify.WithRunMode(mode))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer cancel()
	if err := app.RunWithContext(ctx, optFns...); err != nil {
		return err
	}
	return nil
}
