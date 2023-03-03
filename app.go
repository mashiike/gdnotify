package gdnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Songmu/flextime"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/fujiwara/ridge"
	"github.com/google/uuid"
	logx "github.com/mashiike/go-logx"
	"github.com/mattn/go-shellwords"
	"github.com/olekukonko/tablewriter"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type App struct {
	storage         Storage
	notification    Notification
	drives          map[string]*DriveConfig
	rotateRemaining time.Duration
	driveSvc        *drive.Service
	cleanupFns      []func() error
	expiration      time.Duration
	webhookAddress  string
}

type RunOptions struct {
	Mode         RunMode
	LocalAddress string
	CLICommand   CLICommand
}

func WithRunMode(mode string) func(*RunOptions) error {
	return func(opts *RunOptions) error {
		if m, err := RunModeString(mode); err != nil {
			return err
		} else {
			opts.Mode = m
		}
		return nil
	}
}

func WithLocalAddress(addr string) func(*RunOptions) error {
	return func(opts *RunOptions) error {
		opts.LocalAddress = addr
		return nil
	}
}

func WithCLICommand(cmd string) func(*RunOptions) error {
	return func(opts *RunOptions) error {
		if c, err := CLICommandString(cmd); err != nil {
			return err
		} else {
			opts.CLICommand = c
		}
		return nil
	}
}

func isLambda() bool {
	if strings.HasPrefix(os.Getenv("AWS_EXECUTION_ENV"), "AWS_Lambda") || os.Getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		return true
	}
	return false
}

func DefaultRunMode() RunMode {
	if isLambda() {
		return RunModeWebhook
	}
	return RunModeCLI
}

func newRunOptions() *RunOptions {
	return &RunOptions{
		Mode:         DefaultRunMode(),
		LocalAddress: ":8080",
	}
}

func defaultAWSConfig(ctx context.Context) (aws.Config, error) {
	awsOpts := make([]func(*config.LoadOptions) error, 0)
	if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
		awsOpts = append(awsOpts, config.WithRegion(region))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, awsOpts...)
	if err != nil {
		return *aws.NewConfig(), err
	}
	return awsCfg, nil
}

func New(cfg *Config, gcpOpts ...option.ClientOption) (*App, error) {
	drives := lo.FromEntries(lo.Map(cfg.Drives, func(cfg *DriveConfig, _ int) lo.Entry[string, *DriveConfig] {
		return lo.Entry[string, *DriveConfig]{
			Key:   cfg.DriveID,
			Value: cfg,
		}
	}))

	ctx := context.Background()

	awsCfg, err := defaultAWSConfig(ctx)
	if err != nil {
		return nil, err
	}

	cleanupFns := make([]func() error, 0)
	storage, cleanup, err := NewStorage(ctx, cfg.Storage, awsCfg)
	if err != nil {
		return nil, fmt.Errorf("create Storage: %w", err)
	}
	if cleanup != nil {
		cleanupFns = append(cleanupFns, cleanup)
	}
	notification, cleanup, err := NewNotification(ctx, cfg.Notification, awsCfg)
	if err != nil {
		return nil, fmt.Errorf("create Notification: %w", err)
	}
	if cleanup != nil {
		cleanupFns = append(cleanupFns, cleanup)
	}

	gcpOpts = append(
		gcpOpts,
		option.WithScopes(
			drive.DriveScope,
			drive.DriveFileScope,
		),
	)
	credentialsBackend, err := NewCredentialsBackend(ctx, cfg.Credentials, awsCfg)
	if err != nil {
		return nil, fmt.Errorf("create Credentials Backend: %w", err)
	}
	gcpOpts, err = credentialsBackend.WithCredentialsClientOption(ctx, gcpOpts)
	if err != nil {
		return nil, fmt.Errorf("google Application Credentials Load: %w", err)
	}
	driveSvc, err := drive.NewService(ctx, gcpOpts...)
	if err != nil {
		return nil, fmt.Errorf("create Google Drive Service: %w", err)
	}

	rotateRemaining := time.Duration(0.2 * float64(cfg.Expiration))
	log.Printf("[debug] cfg.Expiration=%s 20%% rotateRemaining=%s", cfg.Expiration, rotateRemaining)

	app := &App{
		storage:         storage,
		notification:    notification,
		drives:          drives,
		rotateRemaining: rotateRemaining,
		driveSvc:        driveSvc,
		cleanupFns:      cleanupFns,
		webhookAddress:  cfg.Webhook,
		expiration:      cfg.Expiration,
	}
	return app, nil
}

func (app *App) Close() error {
	eg, ctx := errgroup.WithContext(context.Background())
	for i, cleanup := range app.cleanupFns {
		_i, _cleanup := i, cleanup
		eg.Go(func() error {
			logx.Printf(ctx, "[debug][%d] start cleanup", _i)
			if err := _cleanup(); err != nil {
				logx.Printf(ctx, "[debug][%d] error cleanup: %s", _i, err.Error())
				return err
			}
			logx.Printf(ctx, "[debug][%d] end cleanup", _i)
			return nil
		})
	}
	return eg.Wait()
}

func (app *App) Run(optFns ...func(*RunOptions) error) error {
	return app.RunWithContext(context.Background(), optFns...)
}

func (app *App) RunWithContext(ctx context.Context, optFns ...func(*RunOptions) error) error {
	opts := newRunOptions()
	for _, optFn := range optFns {
		if err := optFn(opts); err != nil {
			return err
		}
	}
	switch opts.Mode {
	case RunModeWebhook:
		logx.Println(ctx, "[info] run as webhook server")
		return app.runAsWebhookServer(ctx, opts)
	case RunModeMaintainer:
		logx.Println(ctx, "[info] run as channel maintainer")
		return app.runAsChannelMaintainer(ctx, opts)
	case RunModeSyncer:
		logx.Println(ctx, "[info] run as channel syncer")
		return app.runAsChannelSyncer(ctx, opts)
	case RunModeCLI:
		logx.Println(ctx, "[info] run as CLI")
		return app.runAsCLI(ctx, opts)
	}

	return errors.New("unknown run mode")
}

func (app *App) runAsWebhookServer(ctx context.Context, opts *RunOptions) error {
	var wg sync.WaitGroup
	if tunnelCmd := os.Getenv("HTTP_TUNNEL"); !isLambda() && tunnelCmd != "" {
		logx.Println(ctx, "[info] set HTTP_TUNNEL detected")
		var rendered string
		if tmpl, err := template.New("tunnel").Parse(tunnelCmd); err != nil {
			logx.Println(ctx, "[warn] failed HTTP_TUNNEL parse as go template: ", err)
			rendered = tunnelCmd
		} else {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, map[string]interface{}{
				"Address": opts.LocalAddress,
			}); err != nil {
				logx.Println(ctx, "[warn] failed HTTP_TUNNEL execute as go template: ", err)
				rendered = tunnelCmd
			} else {
				rendered = buf.String()
			}
		}
		args, err := shellwords.Parse(rendered)
		if err != nil {
			return fmt.Errorf("HTTP_TUNNEL parse failed: %w", err)
		}
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		tunnelLogFilename := "tunnel.log"
		fp, err := os.Create(tunnelLogFilename)
		if err != nil {
			return fmt.Errorf("can not create %s: %w", tunnelLogFilename, err)
		}
		defer fp.Close()
		cmd.Stdout = fp
		cmd.Stderr = fp
		logx.Printf(ctx, "[info] output HTTP_TUNNEL log to `%s`", tunnelLogFilename)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := cmd.Run(); err != nil {
				logx.Println(ctx, "[warn] tunnel command exit:", err)
			}
		}()
	}
	ridge.RunWithContext(ctx, opts.LocalAddress, "/", app)
	wg.Wait()
	return nil
}

func (app *App) runAsChannelMaintainer(ctx context.Context, _ *RunOptions) error {
	if isLambda() {
		logx.Println(ctx, "[info] run on lambda")
		lambda.StartWithOptions(func(ctx context.Context, event json.RawMessage) (interface{}, error) {
			if err := app.maintenanceChannels(ctx, false); err != nil {
				logx.Println(ctx, "[error] failed maintenance channels: ", err)
				return nil, err
			}
			return map[string]interface{}{
				"Status": 200,
			}, nil
		}, lambda.WithContext(ctx))
		return nil
	}
	logx.Println(ctx, "[info] run on local")
	return app.maintenanceChannels(ctx, false)
}

func (app *App) runAsChannelSyncer(ctx context.Context, _ *RunOptions) error {
	if isLambda() {
		logx.Println(ctx, "[info] run on lambda")
		lambda.StartWithOptions(func(ctx context.Context) (interface{}, error) {
			if err := app.syncChannels(ctx); err != nil {
				logx.Println(ctx, "[error] failed sync channels: ", err)
				return nil, err
			}
			return map[string]interface{}{
				"Status": 200,
			}, nil
		}, lambda.WithContext(ctx))
		return nil
	}
	logx.Println(ctx, "[info] run on local")
	return app.syncChannels(ctx)
}

func (app *App) runAsCLI(ctx context.Context, opts *RunOptions) error {
	if isLambda() {
		return errors.New("run_mode is CLI, can not run on AWS Lambda")
	}
	switch opts.CLICommand {
	case CLICommandList:
		return app.listChannels(ctx, os.Stdout)
	case CLICommandServe:
		return app.runAsWebhookServer(ctx, opts)
	case CLICommandRegister:
		return app.maintenanceChannels(ctx, true)
	case CLICommandMaintenance:
		return app.maintenanceChannels(ctx, false)
	case CLICommandCleanup:
		return app.cleanupChannels(ctx)
	case CLICommandSync:
		return app.syncChannels(ctx)
	default:
		return fmt.Errorf("unknown cli command `%s`", opts.CLICommand)
	}
}

func (app *App) maintenanceChannels(ctx context.Context, createOnly bool) error {
	if app.webhookAddress == "" {
		return errors.New("webhook address is empty, plz check configure")
	}
	itemsCh, err := app.storage.FindAllChannels(ctx)
	if err != nil {
		return fmt.Errorf("find all channels: %w", err)
	}
	existsDriveIDs := lo.FromEntries(lo.Map(lo.Keys(app.drives), func(driveID string, _ int) lo.Entry[string, bool] {
		return lo.Entry[string, bool]{
			Key:   driveID,
			Value: false,
		}
	}))
	channelsByDriveID := make(map[string][]*ChannelItem, len(existsDriveIDs))
	for items := range itemsCh {
		for _, item := range items {
			logx.Printf(ctx,
				"[info] find channel_id=%s, drive_id=%s, expiration=%s, created_at=%s",
				item.ChannelID, item.DriveID, item.Expiration.Format(time.RFC3339), item.CreatedAt.Format(time.RFC3339),
			)
			existsDriveIDs[item.DriveID] = true
			channels, ok := channelsByDriveID[item.DriveID]
			if !ok {
				channels = make([]*ChannelItem, 0)
			}
			channels = append(channels, item)
			channelsByDriveID[item.DriveID] = channels
		}
	}
	egForNew, egCtxForNew := errgroup.WithContext(ctx)
	for driveID, exists := range existsDriveIDs {
		if exists {
			continue
		}
		_driveID := driveID
		egForNew.Go(func() error {
			logx.Printf(egCtxForNew, "[info] channel not exist drive_id=%s, try create channel", _driveID)
			if err := app.CreateChannel(egCtxForNew, _driveID); err != nil {
				logx.Printf(egCtxForNew, "[error] failed CreateChannel drive_id=%s", _driveID)
				return fmt.Errorf("CreateChannel:%w", err)
			}
			return nil
		})
	}
	egForRotate, egCtxForRotate := errgroup.WithContext(ctx)
	for driveID, channels := range channelsByDriveID {
		_driveID := driveID
		noRotateExists := false
		rotationTargets := make([]*ChannelItem, 0)
		for _, channel := range channels {
			if channel.IsAboutToExpired(egCtxForRotate, app.rotateRemaining) {
				rotationTargets = append(rotationTargets, channel)
			} else {
				noRotateExists = true
			}
		}
		if noRotateExists && len(rotationTargets) == 0 {
			continue
		}
		egForRotate.Go(func() error {
			logx.Printf(egCtxForRotate, "[info] try rotation drive_id=%s", _driveID)
			if len(rotationTargets) == 0 {
				return nil
			}
			if err := app.RotateChannel(egCtxForRotate, rotationTargets[0]); err != nil {
				return err
			}
			if len(rotationTargets) == 1 {
				return nil
			}
			for _, cannel := range rotationTargets[1:] {
				if err := app.DeleteChannel(egCtxForRotate, cannel); err != nil {
					logx.Printf(egCtxForRotate, "[warn] cleanup failed drive_id=%s, channel_id=%s, resource_id=%s", _driveID, cannel.ChannelID, cannel.ResourceID)
				}
			}
			return nil
		})

	}
	if err := egForNew.Wait(); err != nil {
		return fmt.Errorf("NewChannel:%w", err)
	}
	if err := egForRotate.Wait(); err != nil {
		return fmt.Errorf("RotateChannel:%w", err)
	}
	return nil
}

func (app *App) CreateChannel(ctx context.Context, driveID string) error {
	token, err := app.getStartPageToken(ctx, driveID)
	if err != nil {
		return err
	}
	item := &ChannelItem{
		PageToken: token,
		DriveID:   driveID,
	}
	return app.createChannel(ctx, item)
}

func (app *App) getStartPageToken(ctx context.Context, driveID string) (string, error) {
	getStartPageTokenCell := app.driveSvc.Changes.GetStartPageToken().SupportsAllDrives(true)
	if driveID != DefaultDriveID {
		getStartPageTokenCell = getStartPageTokenCell.DriveId(driveID)
	}
	token, err := getStartPageTokenCell.Context(ctx).Do()
	if err != nil {
		logx.Println(ctx, "[debug] drive API changes:getStartPageToken failed:", err)
		return "", fmt.Errorf("drive API changes:getStartPageToken:%w", err)
	}
	if token.HTTPStatusCode != http.StatusOK {
		logx.Printf(ctx, "[debug] drive API changes:getStartPageToken response status not ok (status:%d)", token.HTTPStatusCode)
		return "", fmt.Errorf("drive API changes:getStartPageToken response status not ok (status:%d)", token.HTTPStatusCode)
	}
	return token.StartPageToken, nil
}

func (app *App) createChannel(ctx context.Context, item *ChannelItem) error {
	uuidObj, err := uuid.NewRandom()
	if err != nil {
		logx.Println(ctx, "[debug] create new uuid v4: ", err)
		return fmt.Errorf(" create new uuid v4: %w", err)
	}
	now := flextime.Now()
	item.ChannelID = uuidObj.String()
	item.Expiration = now.Add(app.expiration)
	item.CreatedAt = now
	item.UpdatedAt = now
	if item.PageTokenFetchedAt.IsZero() {
		item.PageTokenFetchedAt = now
	}

	watchCall := app.driveSvc.Changes.Watch(item.PageToken, &drive.Channel{
		Id:         item.ChannelID,
		Address:    app.webhookAddress,
		Expiration: item.Expiration.UnixMilli(),
		Type:       "web_hook",
		Payload:    true,
	}).SupportsAllDrives(true).IncludeItemsFromAllDrives(true)
	if item.DriveID != DefaultDriveID {
		watchCall = watchCall.DriveId(item.DriveID)
	}
	resp, err := watchCall.Context(ctx).Do()
	if err != nil {
		logx.Println(ctx, "[debug] drive API changes:watch failed:", err)
		return fmt.Errorf("drive API changes:watch:%w", err)
	}
	if err != nil {
		logx.Printf(ctx, "[debug] drive API changes:watch response status not ok (status:%d)", resp.HTTPStatusCode)
		return fmt.Errorf("drive API changes:watch response status not ok (status:%d)", resp.HTTPStatusCode)
	}
	item.ResourceID = resp.ResourceId
	item.Expiration = time.UnixMilli(resp.Expiration)
	logx.Printf(ctx, "[info] create channel id=%s, resource_id=%s, drive_id=%s page_token=%s, resource_uri=%s, expiration=%s",
		resp.Id, resp.ResourceId, item.DriveID, item.PageToken, resp.ResourceUri, item.Expiration,
	)
	if err := app.storage.SaveChannel(ctx, item); err != nil {
		logx.Println(ctx, "[debug] save channel failed", err)
		return fmt.Errorf("save channel:%w", err)
	}
	return nil
}

func (app *App) listChannels(ctx context.Context, w io.Writer) error {
	itemsCh, err := app.storage.FindAllChannels(ctx)
	if err != nil {
		return fmt.Errorf("find all channels: %w", err)
	}
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Channel ID", "Drive ID", "Page Token", "Expiration", "Resource ID", "Start Page Token Fetched At", "Created At", "Updated At"})
	for items := range itemsCh {
		for _, item := range items {
			table.Append([]string{
				item.ChannelID,
				item.DriveID,
				item.PageToken,
				item.Expiration.Format(time.RFC3339),
				item.ResourceID,
				item.PageTokenFetchedAt.Format(time.RFC3339),
				item.CreatedAt.Format(time.RFC3339),
				item.UpdatedAt.Format(time.RFC3339),
			})
		}
	}
	table.Render()
	return nil
}

func (app *App) cleanupChannels(ctx context.Context) error {
	itemsCh, err := app.storage.FindAllChannels(ctx)
	if err != nil {
		return fmt.Errorf("find all channels: %w", err)
	}
	for items := range itemsCh {
		for _, item := range items {
			logx.Printf(ctx,
				"[info] find channel_id=%s, drive_id=%s, expiration=%s, created_at=%s",
				item.ChannelID, item.DriveID, item.Expiration.Format(time.RFC3339), item.CreatedAt.Format(time.RFC3339),
			)
			if err := app.DeleteChannel(ctx, item); err != nil {
				logx.Printf(ctx, "[warn] failed DeleteChannel channel_id=%s, resource_id=%s, drive_id=%s", item.ChannelID, item.ResourceID, item.DriveID)
				continue
			}
			logx.Printf(ctx,
				"[info] deleted channel_id=%s, drive_id=%s, expiration=%s, created_at=%s",
				item.ChannelID, item.DriveID, item.Expiration.Format(time.RFC3339), item.CreatedAt.Format(time.RFC3339),
			)
		}
	}
	return nil
}

func (app *App) syncChannels(ctx context.Context) error {
	itemsCh, err := app.storage.FindAllChannels(ctx)
	if err != nil {
		return fmt.Errorf("find all channels: %w", err)
	}
	for items := range itemsCh {
		for _, item := range items {
			logx.Printf(ctx,
				"[info] find channel_id=%s, drive_id=%s, expiration=%s, created_at=%s",
				item.ChannelID, item.DriveID, item.Expiration.Format(time.RFC3339), item.CreatedAt.Format(time.RFC3339),
			)
			changes, _, err := app.changesList(ctx, item)
			if err != nil {
				logx.Printf(ctx, "[warn] failed sync channel_id=%s, resource_id=%s, drive_id=%s", item.ChannelID, item.ResourceID, item.DriveID)
				continue
			}
			if err != nil {
				logx.Printf(ctx, "[warn] get changes list failed channel_id:%s resource_id:%s err:%s",
					coalesce(item.ChannelID, "-"),
					coalesce(item.ResourceID, "-"),
					err.Error(),
				)
			}
			if len(changes) > 0 {
				logx.Printf(ctx, "[debug] send changes channel_id:%s resource_id:%s",
					coalesce(item.ChannelID, "-"),
					coalesce(item.ResourceID, "-"),
				)
				if err := app.notification.SendChanges(ctx, item, changes); err != nil {
					logx.Printf(ctx, "[error] send changes failed channel_id:%s resource_id:%s err:%s",
						coalesce(item.ChannelID, "-"),
						coalesce(item.ResourceID, "-"),
						err.Error(),
					)
				}
			} else {
				logx.Printf(ctx, "[debug] no changes channel_id:%s resource_id:%s",
					coalesce(item.ChannelID, "-"),
					coalesce(item.ResourceID, "-"),
				)
			}
		}
	}
	return nil
}

func (app *App) DeleteChannel(ctx context.Context, item *ChannelItem) error {
	logx.Printf(ctx, "[info] delete channel id=%s, resource_id=%s, drive_id=%s page_token=%s",
		item.ChannelID, item.ResourceID, item.DriveID, item.PageToken,
	)
	err := app.driveSvc.Channels.Stop(&drive.Channel{
		Id:         item.ChannelID,
		ResourceId: item.ResourceID,
	}).Context(ctx).Do()
	if err != nil {
		logx.Println(ctx, "[debug] drive API channels:stop failed:", err)
		var apiError *googleapi.Error
		if !errors.As(err, &apiError) {
			return fmt.Errorf("drive API channels:stop:%w", err)
		}
		if apiError.Code != http.StatusNotFound {
			return fmt.Errorf("drive API channels:stop:%w", apiError)
		}
		logx.Printf(ctx, "[warn] channel is already stopped continue and storage try delete: channel id=%s, resource_id=%s, drive_id=%s",
			item.ChannelID, item.ResourceID, item.DriveID,
		)
	}
	if err := app.storage.DeleteChannel(ctx, item); err != nil {
		logx.Println(ctx, "[debug] delete channel failed", err)
		return fmt.Errorf("delete channel:%w", err)
	}
	return nil
}

const (
	pageTokenRefreshIntervalDays = 90
)

func (app *App) RotateChannel(ctx context.Context, item *ChannelItem) error {
	logx.Printf(ctx, "[info] try rotate channel channel id=%s, resource_id=%s, drive_id=%s",
		item.ChannelID, item.ResourceID, item.DriveID,
	)
	newItem := *item
	now := flextime.Now()
	if now.Sub(item.PageTokenFetchedAt) >= pageTokenRefreshIntervalDays*24*time.Hour {
		logx.Printf(ctx, "[info] 90 days have passed since the first acquisition of the PageToken, so try to re-acquire the PageToken: channel id=%s, resource_id=%s, drive_id=%s",
			item.ChannelID, item.ResourceID, item.DriveID,
		)
		token, err := app.getStartPageToken(ctx, item.DriveID)
		if err != nil {
			logx.Printf(ctx, "[error] re-acquire the PageToken failed: channel id=%s, resource_id=%s, drive_id=%s: %s",
				item.ChannelID, item.ResourceID, item.DriveID, err.Error(),
			)
			logx.Printf(ctx, "[warn] PageToken is out of date and attempts to rotate: channel id=%s, resource_id=%s, drive_id=%s",
				item.ChannelID, item.ResourceID, item.DriveID,
			)
		} else {
			newItem.PageToken = token
			newItem.PageTokenFetchedAt = now
		}
	}
	if err := app.createChannel(ctx, &newItem); err != nil {
		logx.Printf(ctx, "[error] failed rotate channel id=%s, resource_id=%s, drive_id=%s: %s",
			item.ChannelID, item.ResourceID, item.DriveID, err.Error(),
		)
		return err
	}
	logx.Printf(ctx, "[info] success rotate channel old_channel_id=%s, new_channel_id=%s, drive_id=%s",
		item.ChannelID, newItem.ChannelID, item.DriveID,
	)
	if err := app.DeleteChannel(ctx, item); err != nil {
		logx.Printf(ctx, "[error] failed delete old channel id=%s, resource_id=%s, drive_id=%s: %s",
			item.ChannelID, item.ResourceID, item.DriveID, err.Error(),
		)
		return err
	}
	return nil
}

var driveFields = fmt.Sprintf("drive(%s)", strings.Join(
	[]string{"id", "name", "kind", "themeId", "orgUnitId", "createdTime", "hidden"},
	",",
))
var fileFields = fmt.Sprintf("file(%s)", strings.Join(
	[]string{"id", "name", "driveId", "kind", "mimeType", "modifiedTime", "lastModifyingUser", "trashed", "trashedTime", "trashingUser", "version", "size", "md5Checksum", "createdTime"},
	",",
))
var changesFields = fmt.Sprintf("changes(%s)", strings.Join(
	[]string{"time", "kind", "removed", "fileId", "changeType", "driveId", driveFields, fileFields},
	",",
))

func (app *App) ChangesList(ctx context.Context, channelID string) ([]*drive.Change, *ChannelItem, error) {
	logx.Printf(ctx, "[debug] try FindOneByChannelID  channel id=%s", channelID)
	item, err := app.storage.FindOneByChannelID(ctx, channelID)
	logx.Printf(ctx, "[debug] finish FindOneByChannelID  channel id=%s err=%#v", channelID, err)
	if err != nil {
		logx.Printf(ctx, "[debug] failed FindOneByChannelID channel_id=%s err=%s", channelID, err.Error())
		return nil, nil, err
	}
	logx.Printf(ctx, "[debug] try change list channel id=%s, resource_id=%s, drive_id=%s",
		item.ChannelID, item.ResourceID, item.DriveID,
	)
	return app.changesList(ctx, item)
}

func (app *App) changesList(ctx context.Context, item *ChannelItem) ([]*drive.Change, *ChannelItem, error) {
	changes := make([]*drive.Change, 0, 100)
	nextPageToken := ""
	newStartPageToken := ""
	process := func(ctx context.Context, pageToken string) error {
		call := app.driveSvc.Changes.List(pageToken).
			IncludeCorpusRemovals(true).
			IncludeItemsFromAllDrives(true).
			SupportsAllDrives(true).
			PageSize(100).
			Fields("newStartPageToken", "nextPageToken", googleapi.Field(changesFields))
		if item.DriveID != DefaultDriveID {
			call = call.DriveId(item.DriveID)
		}
		changeList, err := call.Context(ctx).Do()
		logx.Printf(ctx, "[debug] try Drive API changes:list: channel_id=%s drive_id=%s page_token=%s", item.ChannelID, item.DriveID, pageToken)
		if err != nil {
			logx.Printf(ctx, "[debug] failed Drive API changes:list channel id=%s, resource_id=%s, drive_id=%s: %s",
				item.ChannelID, item.ResourceID, item.DriveID, err.Error(),
			)
			return err
		}
		logx.Printf(ctx, "[debug] success Drive API changes:list: channel_id=%s drive_id=%s, pageToken=%s changes=%d", item.ChannelID, item.DriveID, pageToken, len(changeList.Changes))
		changes = append(changes, changeList.Changes...)
		nextPageToken = changeList.NextPageToken
		newStartPageToken = changeList.NewStartPageToken
		logx.Printf(ctx, "[debug] Drive API changes:list: channel_id=%s drive_id=%s, next_page_token=%s  new_start_page_token=%s", item.ChannelID, item.DriveID, pageToken, newStartPageToken)
		return nil
	}
	if err := process(ctx, item.PageToken); err != nil {
		return nil, nil, err
	}
	for nextPageToken != "" {
		time.Sleep(200 * time.Millisecond)
		if err := process(ctx, nextPageToken); err != nil {
			return nil, nil, err
		}
	}
	logx.Printf(ctx, "[info] PageToken refresh channel_id=%s old_page_token=%s new_page_token=%s", item.ChannelID, item.PageToken, newStartPageToken)
	newItem := *item
	newItem.PageToken = newStartPageToken
	newItem.UpdatedAt = flextime.Now()
	if err := app.storage.UpdatePageToken(ctx, &newItem); err != nil {
		return nil, nil, err
	}
	return changes, &newItem, nil
}
