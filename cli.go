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
	"github.com/mashiike/slogutils"
)

// CLI is the command-line interface for gdnotify.
//
// Use the Run method to execute the CLI:
//
//	var cli gdnotify.CLI
//	ctx := context.Background()
//	exitCode := cli.Run(ctx)
//
// Available commands:
//   - serve: Start the webhook server (default)
//   - list: List registered notification channels
//   - sync: Force synchronization of all channels
//   - cleanup: Remove all notification channels
//   - validate: Validate configuration files
type CLI struct {
	LogLevel     string             `help:"log level" default:"info" env:"GDNOTIFY_LOG_LEVEL"`
	LogFormat    string             `help:"log format" default:"text" enum:"text,json" env:"GDNOTIFY_LOG_FORMAT"`
	LogColor     bool               `help:"enable color output" default:"true" env:"GDNOTIFY_LOG_COLOR" negatable:""`
	Version      kong.VersionFlag   `help:"show version"`
	Storage      StorageOption      `embed:"" prefix:"storage-"`
	Notification NotificationOption `embed:"" prefix:"notification-"`
	S3CopyConfig string             `name:"s3-copy-config" help:"path to S3 copy configuration file" env:"GDNOTIFY_S3_COPY_CONFIG"`
	AppOption    `embed:""`

	List     ListOption     `cmd:"" help:"list notification channels"`
	Serve    ServeOption    `cmd:"" help:"serve webhook server" default:"true"`
	Cleanup  CleanupOption  `cmd:"" help:"remove all notification channels"`
	Sync     SyncOption     `cmd:"" help:"force sync notification channels; re-register expired notification channels,register new unregistered channels and get all new notification"`
	Validate ValidateOption `cmd:"" help:"validate configuration files"`
}

// ListOption contains options for the list command.
type ListOption struct {
	Output io.Writer `kong:"-"`
}

// ServeOption contains options for the serve command.
type ServeOption struct {
	Port int `help:"webhook httpd port" default:"25254" env:"GDNOTIFY_PORT"`
}

// SyncOption contains options for the sync command.
type SyncOption struct {
}

// CleanupOption contains options for the cleanup command.
type CleanupOption struct {
}

// ValidateOption contains options for the validate command.
type ValidateOption struct {
	S3CopyConfig string `arg:"" name:"config-file" optional:"" help:"path to S3 copy configuration file (overrides --s3-copy-config)"`
}

// Run parses command-line arguments and executes the appropriate command.
// Returns 0 on success, 1 on error.
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
	if err := c.run(ctx, k); err != nil {
		slog.Error("runtime error", "details", err)
		return 1
	}
	return 0
}

func (c *CLI) run(ctx context.Context, k *kong.Context) error {
	var err error
	cmd := k.Command()
	if cmd == "version" {
		fmt.Printf("gdnotify version %s\n", Version)
		return nil
	}
	// validate command doesn't need App initialization
	if cmd == "validate" || cmd == "validate <config-file>" {
		return c.runValidate(ctx)
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
			slog.WarnContext(ctx, "gcreds cleanup error", "details", err)
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

func (c *CLI) runValidate(ctx context.Context) error {
	configPath := c.Validate.S3CopyConfig
	if configPath == "" {
		configPath = c.S3CopyConfig
	}
	if configPath == "" {
		return fmt.Errorf("no configuration file specified; use --s3-copy-config or provide a path as argument")
	}

	env, err := NewCELEnv()
	if err != nil {
		return fmt.Errorf("create CEL environment: %w", err)
	}

	slog.InfoContext(ctx, "validating S3 copy configuration", "path", configPath)
	cfg, err := LoadS3CopyConfig(configPath, env)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	slog.InfoContext(ctx, "configuration is valid",
		"rules", len(cfg.Rules),
		"default_bucket", cfg.BucketName.Raw(),
		"default_object_key_is_expr", cfg.ObjectKey.IsExpr(),
	)

	for i, rule := range cfg.Rules {
		slog.InfoContext(ctx, "rule validated",
			"index", i,
			"when", rule.When.Raw(),
			"skip", rule.Skip,
			"export", rule.Export,
		)
	}

	fmt.Println("✓ Configuration is valid")
	return nil
}

func (c *CLI) newApp(ctx context.Context) (*App, error) {
	storage, err := NewStorage(ctx, c.Storage)
	if err != nil {
		return nil, fmt.Errorf("create Storage: %w", err)
	}
	notification, err := NewNotification(ctx, c.Notification)
	if err != nil {
		return nil, fmt.Errorf("create Notification: %w", err)
	}
	app, err := New(c.AppOption, storage, notification, gcreds4aws.WithCredentials(ctx))
	if err != nil {
		return nil, err
	}
	if c.S3CopyConfig != "" {
		if err := c.setupS3Copier(ctx, app); err != nil {
			return nil, fmt.Errorf("setup S3 copier: %w", err)
		}
	}
	return app, nil
}

func (c *CLI) setupS3Copier(ctx context.Context, app *App) error {
	env, err := NewCELEnv()
	if err != nil {
		return fmt.Errorf("create CEL environment: %w", err)
	}
	cfg, err := LoadS3CopyConfig(c.S3CopyConfig, env)
	if err != nil {
		return fmt.Errorf("load S3 copy config: %w", err)
	}
	awsCfg, err := loadAWSConfig()
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	copier := NewS3Copier(cfg, env, app.driveSvc, awsCfg)
	app.SetS3Copier(copier)
	slog.InfoContext(ctx, "S3 copy enabled", "config", c.S3CopyConfig, "rules", len(cfg.Rules))
	return nil
}

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
			slog.LevelWarn:  slogutils.Color(color.FgYellow),
			slog.LevelError: slogutils.Color(color.FgRed, color.Bold),
		}
	}
	var addSource bool
	if level == slog.LevelDebug {
		addSource = true
	}
	middleware := slogutils.NewMiddleware(
		f,
		slogutils.MiddlewareOptions{
			Writer:        os.Stderr,
			ModifierFuncs: modifierFuncs,
			HandlerOptions: &slog.HandlerOptions{
				Level:     level,
				AddSource: addSource,
			},
			RecordTransformerFuncs: []slogutils.RecordTransformerFunc{
				slogutils.ConvertLegacyLevel(
					map[string]slog.Level{
						"debug": slog.LevelDebug,
						"info":  slog.LevelInfo,
						"warn":  slog.LevelWarn,
						"error": slog.LevelError,
					},
					true,
				),
			},
		},
	)
	logger := slog.New(middleware)
	return logger
}
