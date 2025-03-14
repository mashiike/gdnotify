package gdnotify

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Songmu/flextime"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/fujiwara/ridge"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/olekukonko/tablewriter"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type App struct {
	storage            Storage
	notification       Notification
	rotateRemaining    time.Duration
	driveSvc           *drive.Service
	cleanupFns         []func() error
	expiration         time.Duration
	withinModifiedTime *time.Duration
	webhookAddress     string
	router             *mux.Router
	drivesCache        []*drive.Drive
	drivesFetchedAt    time.Time
	drivesMu           sync.Mutex
	webhookAddressMu   sync.Mutex
}

var awsCfg *aws.Config
var loadAWSConfig = sync.OnceValues(func() (aws.Config, error) {
	if awsCfg != nil {
		return *awsCfg, nil
	}
	ctx := context.Background()
	awsOpts := make([]func(*config.LoadOptions) error, 0)
	if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
		awsOpts = append(awsOpts, config.WithRegion(region))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, awsOpts...)
	if err != nil {
		return *aws.NewConfig(), err
	}
	return awsCfg, nil
})

func SetAWSConfig(cfg aws.Config) {
	awsCfg = &cfg
}

type AppOption struct {
	Webhook            string         `help:"webhook address" default:"" env:"GDNOTIFY_WEBHOOK"`
	Expiration         time.Duration  `help:"channel expiration" default:"168h" env:"GDNOTIFY_EXPIRATION"`
	WithinModifiedTime *time.Duration `help:"within modified time, If the edit time is not within this time, notifications will not be sent." env:"GDNOTIFY_WITHIN_MODIFIED_TIME"`
}

func New(cfg AppOption, storage Storage, notification Notification, gcpOpts ...option.ClientOption) (*App, error) {
	ctx := context.Background()
	cleanupFns := make([]func() error, 0)
	if closer, ok := storage.(io.Closer); ok {
		cleanupFns = append(cleanupFns, closer.Close)
	}
	if closer, ok := notification.(io.Closer); ok {
		cleanupFns = append(cleanupFns, closer.Close)
	}

	gcpOpts = append(
		gcpOpts,
		option.WithScopes(
			drive.DriveScope,
			drive.DriveFileScope,
		),
	)
	driveSvc, err := drive.NewService(ctx, gcpOpts...)
	if err != nil {
		return nil, fmt.Errorf("create Google Drive Service: %w", err)
	}

	rotateRemaining := time.Duration(0.2 * float64(cfg.Expiration))
	slog.DebugContext(ctx, "cfg.Expiration and rotateRemaining", "cfg.Expiration", cfg.Expiration, "rotateRemaining", rotateRemaining)

	app := &App{
		storage:            storage,
		notification:       notification,
		rotateRemaining:    rotateRemaining,
		driveSvc:           driveSvc,
		cleanupFns:         cleanupFns,
		webhookAddress:     cfg.Webhook,
		expiration:         cfg.Expiration,
		withinModifiedTime: cfg.WithinModifiedTime,
		router:             mux.NewRouter(),
	}
	app.setupRoute()
	return app, nil
}

func (app *App) Close() error {
	eg, ctx := errgroup.WithContext(context.Background())
	for i, cleanup := range app.cleanupFns {
		_i, _cleanup := i, cleanup
		eg.Go(func() error {
			slog.DebugContext(ctx, "start cleanup", "index", _i)
			if err := _cleanup(); err != nil {
				slog.DebugContext(ctx, "error cleanup", "index", _i, "error", err)
				return err
			}
			slog.DebugContext(ctx, "end cleanup", "index", _i)
			return nil
		})
	}
	return eg.Wait()
}

func (app *App) List(ctx context.Context, o ListOption) error {
	w := o.Output
	if w == nil {
		w = os.Stdout
	}
	itemsCh, err := app.storage.FindAllChannels(ctx)
	if err != nil {
		return fmt.Errorf("find all channels: %w", err)
	}
	drives, err := app.Drives(ctx)
	if err != nil {
		drives = []*drive.Drive{
			{
				Id:   DefaultDriveID,
				Name: DefaultDriveName,
			},
		}
		slog.WarnContext(ctx, "get DriveIDs failed", "error", err)
	}
	exitsDrive := make(map[string]bool, len(drives))
	driveNameById := make(map[string]string, len(drives))
	for _, drive := range drives {
		driveNameById[drive.Id] = drive.Name
		exitsDrive[drive.Id] = false
	}
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Channel ID", "Drive ID", "Drive Name", "Page Token", "Expiration", "Resource ID", "Start Page Token Fetched At", "Created At", "Updated At"})
	for items := range itemsCh {
		for _, item := range items {
			exitsDrive[item.DriveID] = true
			driveName, ok := driveNameById[item.DriveID]
			if !ok {
				driveName = "-"
			}
			table.Append([]string{
				item.ChannelID,
				item.DriveID,
				driveName,
				item.PageToken,
				item.Expiration.Format(time.RFC3339),
				item.ResourceID,
				item.PageTokenFetchedAt.Format(time.RFC3339),
				item.CreatedAt.Format(time.RFC3339),
				item.UpdatedAt.Format(time.RFC3339),
			})
		}
	}
	for driveID, exists := range exitsDrive {
		if exists {
			continue
		}
		table.Append([]string{
			"-",
			driveID,
			driveNameById[driveID],
			"-",
			"-",
			"-",
			"-",
			"-",
			"-",
		})
	}
	table.Render()
	return nil
}

func (app *App) Serve(ctx context.Context, o ServeOption) error {
	r := ridge.New(fmt.Sprintf(":%d", o.Port), "/", app)
	r.RequestBuilder = func(event json.RawMessage) (*http.Request, error) {
		req, err := ridge.NewRequest(event)
		if err == nil && req.Method != "" && req.URL != nil && req.URL.Path != "" {
			return req, nil
		}
		return http.NewRequest(http.MethodPost, "/sync", bytes.NewReader(event))
	}
	r.RunWithContext(ctx)
	return nil
}

func (app *App) Cleanup(ctx context.Context, _ CleanupOption) error {
	return app.cleanupChannels(ctx)
}

func (app *App) Sync(ctx context.Context, _ SyncOption) error {
	if err := app.maintenanceChannels(ctx); err != nil {
		return err
	}
	return app.syncChannels(ctx)
}

const (
	DefaultDriveID   = "__default__"
	DefaultDriveName = "My Drive and Individual Files"
)

func (app *App) DriveIDs(ctx context.Context) ([]string, error) {
	drives, err := app.Drives(ctx)
	if err != nil {
		return nil, err
	}
	return Map(drives, func(drive *drive.Drive) string {
		return drive.Id
	}), nil
}

func (app *App) Drives(ctx context.Context) ([]*drive.Drive, error) {
	app.drivesMu.Lock()
	defer app.drivesMu.Unlock()
	if !app.drivesFetchedAt.IsZero() && flextime.Since(app.drivesFetchedAt) < 5*time.Minute {
		return app.drivesCache, nil
	}
	drives := make([]*drive.Drive, 0, 1)
	drives = append(drives, &drive.Drive{
		Id:   DefaultDriveID,
		Name: DefaultDriveName,
	})
	nextPageToken := "__initial__"
	for nextPageToken != "" {
		cell := app.driveSvc.Drives.List().PageSize(10).Context(ctx)
		if nextPageToken != "__initial__" {
			cell = cell.PageToken(nextPageToken)
		}
		drivesListResp, err := cell.Do()
		if err != nil {
			return nil, fmt.Errorf("access Drives::list %w", err)
		}
		drives = append(drives, drivesListResp.Drives...)
		nextPageToken = drivesListResp.NextPageToken
	}
	slices.SortFunc(drives, func(a, b *drive.Drive) int {
		return cmp.Compare(a.Id, b.Id)
	})
	app.drivesCache = drives
	app.drivesCache = slices.Compact(drives)
	app.drivesFetchedAt = flextime.Now()
	return app.drivesCache, nil
}

func (app *App) maintenanceChannels(ctx context.Context) error {
	if app.webhookAddress == "" {
		return fmt.Errorf("webhook address is empty")
	}
	itemsCh, err := app.storage.FindAllChannels(ctx)
	if err != nil {
		return fmt.Errorf("find all channels: %w", err)
	}
	driveIDs, err := app.DriveIDs(ctx)
	if err != nil {
		return fmt.Errorf("get DriveIDs: %w", err)
	}
	existsDriveIDs := KeyValues(driveIDs, func(driveID string) (string, bool) {
		return driveID, false
	})
	notFoundDriveIds := make(map[string]bool, len(driveIDs))
	channelsByDriveID := make(map[string][]*ChannelItem, len(existsDriveIDs))
	for items := range itemsCh {
		for _, item := range items {
			slog.InfoContext(ctx, "find channel", "channel_id", item.ChannelID, "drive_id", item.DriveID, "expiration", item.Expiration, "created_at", item.CreatedAt)
			if _, ok := existsDriveIDs[item.DriveID]; !ok {
				notFoundDriveIds[item.DriveID] = true
			} else {
				existsDriveIDs[item.DriveID] = true
			}
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
			slog.InfoContext(egCtxForNew, "channel not exist, try create channel", "drive_id", _driveID)
			if err := app.CreateChannel(egCtxForNew, _driveID); err != nil {
				slog.ErrorContext(egCtxForNew, "failed CreateChannel", "drive_id", _driveID, "error", err)
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
			slog.InfoContext(egCtxForRotate, "try rotation", "drive_id", _driveID)
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
					slog.WarnContext(egCtxForRotate, "cleanup failed", "drive_id", _driveID, "channel_id", cannel.ChannelID, "resource_id", cannel.ResourceID)
				}
			}
			return nil
		})
	}
	egForDelete, egCtxForDelete := errgroup.WithContext(ctx)
	for driveID, exists := range notFoundDriveIds {
		if !exists {
			continue
		}
		_channels := channelsByDriveID[driveID]
		if _channels == nil {
			continue
		}
		_driveID := driveID
		egForDelete.Go(func() error {
			slog.InfoContext(egCtxForDelete, "drive not exist, try delete channel", "drive_id", _driveID)
			for _, channel := range _channels {
				if err := app.DeleteChannel(egCtxForDelete, channel); err != nil {
					slog.WarnContext(egCtxForDelete, "failed DeleteChannel", "drive_id", _driveID, "channel_id", channel.ChannelID, "resource_id", channel.ResourceID)
				}
				slog.InfoContext(egCtxForDelete, "deleted channel", "drive_id", _driveID, "channel_id", channel.ChannelID, "resource_id", channel.ResourceID)
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
		slog.DebugContext(ctx, "drive API changes:getStartPageToken failed", "error", err)
		return "", fmt.Errorf("drive API changes:getStartPageToken:%w", err)
	}
	if token.HTTPStatusCode != http.StatusOK {
		slog.DebugContext(ctx, "drive API changes:getStartPageToken response status not ok", "status", token.HTTPStatusCode)
		return "", fmt.Errorf("drive API changes:getStartPageToken response status not ok (status:%d)", token.HTTPStatusCode)
	}
	return token.StartPageToken, nil
}

func (app *App) createChannel(ctx context.Context, item *ChannelItem) error {
	uuidObj, err := uuid.NewRandom()
	if err != nil {
		slog.DebugContext(ctx, "create new uuid v4 failed", "error", err)
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
		slog.DebugContext(ctx, "drive API changes:watch failed", "error", err)
		return fmt.Errorf("drive API changes:watch:%w", err)
	}
	if err != nil {
		slog.DebugContext(ctx, "drive API changes:watch response status not ok", "status", resp.HTTPStatusCode)
		return fmt.Errorf("drive API changes:watch response status not ok (status:%d)", resp.HTTPStatusCode)
	}
	item.ResourceID = resp.ResourceId
	item.Expiration = time.UnixMilli(resp.Expiration)
	slog.InfoContext(ctx, "create channel", "id", resp.Id, "resource_id", resp.ResourceId, "drive_id", item.DriveID, "page_token", item.PageToken, "resource_uri", resp.ResourceUri, "expiration", item.Expiration)
	if err := app.storage.SaveChannel(ctx, item); err != nil {
		slog.DebugContext(ctx, "save channel failed", "error", err)
		return fmt.Errorf("save channel:%w", err)
	}
	return nil
}

func (app *App) cleanupChannels(ctx context.Context) error {
	itemsCh, err := app.storage.FindAllChannels(ctx)
	if err != nil {
		return fmt.Errorf("find all channels: %w", err)
	}
	for items := range itemsCh {
		for _, item := range items {
			slog.InfoContext(ctx, "find channel", "channel_id", item.ChannelID, "drive_id", item.DriveID, "expiration", item.Expiration.Format(time.RFC3339), "created_at", item.CreatedAt.Format(time.RFC3339))
			if err := app.DeleteChannel(ctx, item); err != nil {
				slog.WarnContext(ctx, "failed DeleteChannel", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID)
				continue
			}
			slog.InfoContext(ctx, "deleted channel", "channel_id", item.ChannelID, "drive_id", item.DriveID, "expiration", item.Expiration.Format(time.RFC3339), "created_at", item.CreatedAt.Format(time.RFC3339))
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
			slog.InfoContext(ctx, "find channel", "channel_id", item.ChannelID, "drive_id", item.DriveID, "expiration", item.Expiration.Format(time.RFC3339), "created_at", item.CreatedAt.Format(time.RFC3339))
			changes, _, err := app.changesList(ctx, item)
			if err != nil {
				slog.WarnContext(ctx, "failed sync", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID)
				continue
			}
			if err != nil {
				slog.WarnContext(ctx, "get changes list failed", "channel_id", coalesce(item.ChannelID, "-"), "resource_id", coalesce(item.ResourceID, "-"), "error", err.Error())
			}
			if len(changes) > 0 {
				slog.DebugContext(ctx, "send changes", "channel_id", coalesce(item.ChannelID, "-"), "resource_id", coalesce(item.ResourceID, "-"))
				if err := app.SendNotification(ctx, item, changes); err != nil {
					slog.ErrorContext(ctx, "send changes failed", "channel_id", coalesce(item.ChannelID, "-"), "resource_id", coalesce(item.ResourceID, "-"), "error", err.Error())
				}
			} else {
				slog.DebugContext(ctx, "no changes", "channel_id", coalesce(item.ChannelID, "-"), "resource_id", coalesce(item.ResourceID, "-"))
			}
		}
	}
	return nil
}

func (app *App) DeleteChannel(ctx context.Context, item *ChannelItem) error {
	slog.InfoContext(ctx, "delete channel", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID, "page_token", item.PageToken)
	err := app.driveSvc.Channels.Stop(&drive.Channel{
		Id:         item.ChannelID,
		ResourceId: item.ResourceID,
	}).Context(ctx).Do()
	if err != nil {
		slog.DebugContext(ctx, "drive API channels:stop failed", "error", err)
		var apiError *googleapi.Error
		if !errors.As(err, &apiError) {
			return fmt.Errorf("drive API channels:stop:%w", err)
		}
		if apiError.Code != http.StatusNotFound {
			return fmt.Errorf("drive API channels:stop:%w", apiError)
		}
		slog.WarnContext(ctx, "channel is already stopped, continue and storage try delete", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID)
	}
	if err := app.storage.DeleteChannel(ctx, item); err != nil {
		slog.DebugContext(ctx, "delete channel failed", "error", err)
		return fmt.Errorf("delete channel:%w", err)
	}
	return nil
}

const (
	pageTokenRefreshIntervalDays = 90
)

func (app *App) RotateChannel(ctx context.Context, item *ChannelItem) error {
	slog.InfoContext(ctx, "try rotate channel", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID)
	newItem := *item
	now := flextime.Now()
	if now.Sub(item.PageTokenFetchedAt) >= pageTokenRefreshIntervalDays*24*time.Hour {
		slog.InfoContext(ctx, "90 days have passed since the first acquisition of the PageToken, so try to re-acquire the PageToken", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID)
		token, err := app.getStartPageToken(ctx, item.DriveID)
		if err != nil {
			slog.ErrorContext(ctx, "re-acquire the PageToken failed", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID, "error", err)
			slog.WarnContext(ctx, "PageToken is out of date and attempts to rotate", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID)
		} else {
			newItem.PageToken = token
			newItem.PageTokenFetchedAt = now
		}
	}
	if err := app.createChannel(ctx, &newItem); err != nil {
		slog.ErrorContext(ctx, "failed rotate channel", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID, "error", err)
		return err
	}
	slog.InfoContext(ctx, "success rotate channel", "old_channel_id", item.ChannelID, "new_channel_id", newItem.ChannelID, "drive_id", item.DriveID)
	if err := app.DeleteChannel(ctx, item); err != nil {
		slog.ErrorContext(ctx, "failed delete old channel", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID, "error", err)
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
	slog.DebugContext(ctx, "try FindOneByChannelID", "channel_id", channelID)
	item, err := app.storage.FindOneByChannelID(ctx, channelID)
	slog.DebugContext(ctx, "finish FindOneByChannelID", "channel_id", channelID, "error", err)
	if err != nil {
		slog.DebugContext(ctx, "failed FindOneByChannelID", "channel_id", channelID, "error", err)
		return nil, nil, err
	}
	slog.DebugContext(ctx, "try change list", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID)
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
		slog.DebugContext(ctx, "Drive API changes:list", "channel_id", item.ChannelID, "drive_id", item.DriveID, "page_token", pageToken)
		if err != nil {
			slog.DebugContext(ctx, "Drive API changes:list failed", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "drive_id", item.DriveID, "error", err)
			return err
		}
		slog.DebugContext(ctx, "Drive API changes:list success", "channel_id", item.ChannelID, "drive_id", item.DriveID, "page_token", pageToken, "changes", len(changeList.Changes))
		changes = append(changes, changeList.Changes...)
		nextPageToken = changeList.NextPageToken
		newStartPageToken = changeList.NewStartPageToken
		slog.DebugContext(ctx, "Drive API changes:list", "channel_id", item.ChannelID, "drive_id", item.DriveID, "page_token", pageToken, "next_page_token", newStartPageToken)
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
	slog.InfoContext(ctx, "PageToken refresh", "channel_id", item.ChannelID, "old_page_token", item.PageToken, "new_page_token", newStartPageToken)
	newItem := *item
	newItem.PageToken = newStartPageToken
	newItem.UpdatedAt = flextime.Now()
	if err := app.storage.UpdatePageToken(ctx, &newItem); err != nil {
		return nil, nil, err
	}
	return changes, &newItem, nil
}

func (app *App) SendNotification(ctx context.Context, item *ChannelItem, changes []*drive.Change) error {
	slog.DebugContext(ctx, "send notification for channel", "channel_id", item.ChannelID)
	if app.withinModifiedTime == nil {
		slog.DebugContext(ctx, "no filter send", "channel_id", item.ChannelID)
		return app.notification.SendChanges(ctx, item, changes)
	}
	slog.DebugContext(ctx, "try filter", "channel_id", item.ChannelID)
	now := time.Now()
	filtered := make([]*drive.Change, 0, len(changes))
	for _, change := range changes {
		if change.File == nil {
			filtered = append(filtered, change)
			continue
		}
		slog.DebugContext(ctx, "try check modified time", "file_id", change.File.Id, "modified_time", change.File.ModifiedTime)
		t, err := time.Parse(time.RFC3339Nano, change.File.ModifiedTime)
		if err != nil {
			filtered = append(filtered, change)
			continue
		}
		if now.Sub(t) > *app.withinModifiedTime {
			slog.InfoContext(ctx, "filtered changes item", "file_id", change.File.Id, "modified_time", change.File.ModifiedTime)
			continue
		}
		filtered = append(filtered, change)
	}
	return app.notification.SendChanges(ctx, item, filtered)
}
