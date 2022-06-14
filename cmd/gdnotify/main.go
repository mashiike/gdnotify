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
		fmt.Fprintln(flag.CommandLine.Output(), "gdnotify -config <config file> [options] [command]")
		fmt.Fprintln(flag.CommandLine.Output(), "version:", Version)
		fmt.Fprintln(flag.CommandLine.Output(), "")
		fmt.Fprintln(flag.CommandLine.Output(), "commands:")
		for _, cmd := range gdnotify.CLICommandValues() {
			fmt.Fprintln(flag.CommandLine.Output(), "  ", cmd.String(), "\t", cmd.Description())
		}
		fmt.Fprintln(flag.CommandLine.Output(), "")
		fmt.Fprintln(flag.CommandLine.Output(), "options:")
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
	flag.StringVar(&mode, "run-mode", gdnotify.DefaultRunMode().String(), fmt.Sprintf(
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
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer cancel()
	cfg := gdnotify.DefaultConfig()
	if err := cfg.Load(ctx, configs...); err != nil {
		return err
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
	if command := flag.Arg(0); command != "" {
		optFns = append(optFns, gdnotify.WithCLICommand(command))
	}

	if err := app.RunWithContext(ctx, optFns...); err != nil {
		return err
	}
	return nil
}
