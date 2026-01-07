package gdnotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/Songmu/flextime"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/mashiike/gdnotify/pkg/gdnotifyevent"
)

// NotificationOption contains configuration for change event delivery.
//
// Supported notification types:
//   - "eventbridge": Sends events to Amazon EventBridge (default, recommended for production)
//   - "file": Writes events to a local JSON file (suitable for development)
type NotificationOption struct {
	Type      string `help:"notification type" default:"eventbridge" enum:"eventbridge,file" env:"GDNOTIFY_NOTIFICATION_TYPE"`
	EventBus  string `help:"event bus name (eventbridge type only)" default:"default" env:"GDNOTIFY_EVENTBRIDGE_EVENT_BUS"`
	EventFile string `help:"event file path (file type only)" default:"gdnotify.json" env:"GDNOTIFY_EVENT_FILE"`
}

// Notification defines the interface for delivering change events to downstream systems.
type Notification interface {
	// SendChanges delivers a batch of change events for the given channel.
	SendChanges(context.Context, *ChannelItem, []*gdnotifyevent.Detail) error
}

// NewNotification creates a Notification implementation based on the configuration type.
// Returns [EventBridgeNotification] for "eventbridge" or [FileNotification] for "file".
func NewNotification(ctx context.Context, cfg NotificationOption) (Notification, error) {
	switch cfg.Type {
	case "eventbridge":
		return NewEventBridgeNotification(ctx, cfg)
	case "file":
		return NewFileNotification(ctx, cfg)
	}
	return nil, errors.New("unknown storage type")
}

// EventBridgeClient is the interface for Amazon EventBridge operations.
// This is satisfied by *eventbridge.Client.
type EventBridgeClient interface {
	PutEvents(ctx context.Context, params *eventbridge.PutEventsInput, optFns ...func(*eventbridge.Options)) (*eventbridge.PutEventsOutput, error)
}

// EventBridgeNotification implements Notification using Amazon EventBridge.
//
// Each Google Drive change is sent as a separate EventBridge event with
// detail-type indicating the change type (e.g., "File Changed", "File Removed").
type EventBridgeNotification struct {
	client   EventBridgeClient
	eventBus string
}

// NewEventBridgeNotification creates a new EventBridge-based notification sender.
func NewEventBridgeNotification(_ context.Context, cfg NotificationOption) (Notification, error) {
	awsCfg, err := loadAWSConfig()
	if err != nil {
		return nil, err
	}
	n := &EventBridgeNotification{
		client:   eventbridge.NewFromConfig(awsCfg),
		eventBus: cfg.EventBus,
	}
	return n, nil
}

func (n *EventBridgeNotification) SendChanges(ctx context.Context, item *ChannelItem, details []*gdnotifyevent.Detail) error {
	sourcePrefix := fmt.Sprintf("oss.gdnotify/%s", item.DriveID)
	convertor := func(d *gdnotifyevent.Detail) types.PutEventsRequestEntry {
		var t time.Time
		if d.Change != nil && d.Change.Time != "" {
			var err error
			t, err = time.Parse(time.RFC3339Nano, d.Change.Time)
			if err != nil {
				slog.WarnContext(ctx, "time Parse failed", "time", d.Change.Time, "error", err)
				t = flextime.Now()
			}
		} else {
			t = flextime.Now()
		}
		bs, err := json.Marshal(d)
		if err != nil {
			slog.WarnContext(ctx, "detail marshal failed", "error", err)
			bs = []byte("{}")
		}
		detail := string(bs)
		source := eventSource(sourcePrefix, d.Change)
		detailType := DetailType(d.Change)
		slog.DebugContext(ctx, "event", "source", source, "detail-type", detailType, "detail", detail)
		return types.PutEventsRequestEntry{
			EventBusName: aws.String(n.eventBus),
			Resources:    []string{},
			Source:       aws.String(source),
			DetailType:   aws.String(detailType),
			Time:         aws.Time(t),
			Detail:       aws.String(detail),
		}
	}
	var lastErr error
	for entries := range slices.Chunk(Map(details, convertor), 10) {
		output, err := n.client.PutEvents(ctx, &eventbridge.PutEventsInput{
			Entries: entries,
		})
		if err != nil {
			slog.ErrorContext(ctx, "PutEvents failed", "error", err)
			lastErr = err
			continue
		}
		for i, entry := range output.Entries {
			if entry.ErrorCode != nil {
				slog.ErrorContext(ctx, "put event error", "event_bus", n.eventBus, "error_code", *entry.ErrorCode, "error_message", *entry.ErrorMessage, "detail", *entries[i].Detail)
				lastErr = fmt.Errorf("put events failed error_code=%s, error_message=%s", *entry.ErrorCode, *entry.ErrorMessage)
				continue
			}
			if entry.EventId != nil {
				slog.InfoContext(ctx, "put event", "event_bus", n.eventBus, "event_id", *entry.EventId)
				continue
			}
		}
	}
	return lastErr
}

// FileNotification implements Notification by writing events to a local JSON file.
//
// This is suitable for development and debugging. Events are appended to the file
// as newline-delimited JSON (NDJSON format).
type FileNotification struct {
	eventFile string
}

// NewFileNotification creates a new file-based notification writer.
func NewFileNotification(_ context.Context, cfg NotificationOption) (*FileNotification, error) {
	n := &FileNotification{
		eventFile: cfg.EventFile,
	}
	return n, nil
}

func (n *FileNotification) SendChanges(ctx context.Context, _ *ChannelItem, details []*gdnotifyevent.Detail) error {
	fp, err := os.OpenFile(n.eventFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		slog.DebugContext(ctx, "can not create notification event file", "event_file", n.eventFile, "error", err)
		return err
	}
	defer fp.Close()
	encoder := json.NewEncoder(fp)
	slog.InfoContext(ctx, "output Changes events", "event_file", n.eventFile)
	var lastErr error
	for _, d := range details {
		var changeType, fileID, driveID string
		if d.Change != nil {
			changeType = d.Change.ChangeType
			fileID = d.Change.FileID
			driveID = d.Change.DriveID
		}
		slog.DebugContext(ctx, "output changes event", "change_type", coalesce(changeType, "-"), "file_id", coalesce(fileID, "-"), "drive_id", coalesce(driveID, "-"))
		if err := encoder.Encode(d); err != nil {
			lastErr = err
			slog.WarnContext(ctx, "FileNotification.SendChanges", "error", err)
		}
	}
	return lastErr
}
