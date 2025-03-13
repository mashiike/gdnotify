package gdnotify

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/alecthomas/kong"
	"github.com/fatih/color"
	"github.com/mashiike/gcreds4aws"
	"github.com/mashiike/sloglevel"
	"github.com/mashiike/slogutils"
)

type CLI struct {
	LogLevel      string             `help:"log level" default:"info" env:"GDNOTIFY_LOG_LEVEL"`
	LogFormat     string             `help:"log format" default:"text" enum:"text,json" env:"GDNOTIFY_LOG_FORMAT"`
	LogColor      bool               `help:"enable color output" default:"true" env:"GDNOTIFY_LOG_COLOR" negatable:""`
	Version       kong.VersionFlag   `help:"show version"`
	Storage       StorageOption      `embed:"" prefix:"storage-"`
	Nootification NotificationOption `embed:"" prefix:"notification-"`
	AppOption     `embed:""`

	List    ListOption    `cmd:"" help:"list notification channels"`
	Serve   ServeOption   `cmd:"" help:"serve webhook server" default:"true"`
	Cleanup CleanupOption `cmd:"" help:"remove all notification channels"`
	Sync    SyncOption    `cmd:"" help:"force sync notification channels; re-register expired notification channels,register new unregistered channels and get all new notification"`
}

type ListOption struct {
	Output io.Writer `kong:"-"`
}

type ServeOption struct {
	Port int `help:"webhook httpd port" default:"25254" env:"GDNOTIFY_PORT"`
}

type SyncOption struct {
}

type CleanupOption struct {
}

func (c *CLI) Run(ctx context.Context) int {
	k := kong.Parse(c,
		kong.Name("gdnotify"),
		kong.Description("gdnotify is a tool for managing notification channels for Google Drive."),
		kong.UsageOnError(),
		kong.Vars{"version": Version},
	)
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(c.LogLevel)); err != nil {
		k.Fatalf("invalid log level: %s", c.LogLevel)
	}
	logger := newLogger(logLevel, c.LogFormat, c.LogColor)
	slog.SetDefault(logger)
	if err := c.run(ctx, k, logger); err != nil {
		slog.Error("runtime error", "details", err)
		return 1
	}
	return 0
}

func (c *CLI) run(ctx context.Context, k *kong.Context, logger *slog.Logger) error {
	var err error
	cmd := k.Command()
	if cmd == "version" {
		fmt.Printf("estellm version %s\n", Version)
		return nil
	}
	app, err := c.newApp(ctx)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	defer func() {
		if err := app.Close(); err != nil {
			slog.WarnContext(ctx, "app cleanup error", "details", err)
		}
		if err := gcreds4aws.Close(); err != nil {
			slog.WarnContext(ctx, "gqreds cleanup error", "details", err)
		}
	}()
	switch cmd {
	case "list":
		return app.List(ctx, c.List)
	case "serve", "":
		return app.Serve(ctx, c.Serve)
	case "cleanup":
		return app.Cleanup(ctx, c.Cleanup)
	case "sync":
		return app.Sync(ctx, c.Sync)
	default:
		return fmt.Errorf("unknown command: %s", k.Command())
	}
}

func (c *CLI) newApp(ctx context.Context) (*App, error) {
	storage, err := NewStorage(ctx, c.Storage)
	if err != nil {
		return nil, fmt.Errorf("create Storage: %w", err)
	}
	notification, err := NewNotification(ctx, c.Nootification)
	if err != nil {
		return nil, fmt.Errorf("create Notification: %w", err)
	}
	return New(c.AppOption, storage, notification, gcreds4aws.WithCredentials(ctx))
}

var LevelNotice slog.Level = slog.LevelInfo + 2

func newLogger(level slog.Level, format string, c bool) *slog.Logger {
	var f func(io.Writer, *slog.HandlerOptions) slog.Handler
	switch format {
	case "json":
		f = func(w io.Writer, ho *slog.HandlerOptions) slog.Handler {
			return slog.NewJSONHandler(w, ho)
		}
	default:
		f = func(w io.Writer, ho *slog.HandlerOptions) slog.Handler {
			return slog.NewTextHandler(w, ho)
		}
	}
	var modifierFuncs map[slog.Level]slogutils.ModifierFunc
	if c {
		modifierFuncs = map[slog.Level]slogutils.ModifierFunc{
			slog.LevelDebug: slogutils.Color(color.FgBlack),
			slog.LevelInfo:  nil,
			LevelNotice:     slogutils.Color(color.FgHiBlue),
			slog.LevelWarn:  slogutils.Color(color.FgYellow),
			slog.LevelError: slogutils.Color(color.FgRed, color.Bold),
		}
	}
	middleware := slogutils.NewMiddleware(
		f,
		slogutils.MiddlewareOptions{
			Writer:        os.Stderr,
			ModifierFuncs: modifierFuncs,
			HandlerOptions: &slog.HandlerOptions{
				Level: level,
				ReplaceAttr: sloglevel.NewAttrReplacer(
					sloglevel.AddLevel(LevelNotice, "NOTICE"),
				),
				AddSource: true,
			},
			RecordTransformerFuncs: []slogutils.RecordTransformerFunc{
				slogutils.ConvertLegacyLevel(
					map[string]slog.Level{
						"debug":  slog.LevelDebug,
						"info":   slog.LevelInfo,
						"notice": LevelNotice,
						"warn":   slog.LevelWarn,
						"error":  slog.LevelError,
					},
					true,
				),
			},
		},
	)
	logger := slog.New(middleware)
	return logger
}
