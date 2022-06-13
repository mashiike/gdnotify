package gdnotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Songmu/flextime"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	logx "github.com/mashiike/go-logx"
	"github.com/samber/lo"
	"google.golang.org/api/drive/v3"
)

type Notification interface {
	SendChanges(context.Context, *ChannelItem, []*drive.Change) error
}

func NewNotification(ctx context.Context, cfg *NotificationConfig, awsCfg aws.Config) (Notification, func() error, error) {
	switch cfg.Type {
	case NotificationTypeEventBridge:
		return NewEventBridgeNotification(ctx, cfg, awsCfg)
	case NotificationTypeFile:
		return NewFileNotification(ctx, cfg)
	}
	return nil, nil, errors.New("unknown storage type")
}

type EventBridgeClient interface {
	PutEvents(ctx context.Context, params *eventbridge.PutEventsInput, optFns ...func(*eventbridge.Options)) (*eventbridge.PutEventsOutput, error)
}

type EventBridgeNotification struct {
	client   EventBridgeClient
	eventBus string
}

func NewEventBridgeNotification(ctx context.Context, cfg *NotificationConfig, awsCfg aws.Config) (Notification, func() error, error) {
	n := &EventBridgeNotification{
		client:   eventbridge.NewFromConfig(awsCfg),
		eventBus: *cfg.EventBus,
	}
	return n, nil, nil
}

func (n *EventBridgeNotification) SendChanges(ctx context.Context, item *ChannelItem, changes []*drive.Change) error {
	sourcePrefix := fmt.Sprintf("oss.gdnotify/%s", item.DriveID)
	entriesChunk := lo.Chunk(lo.Map(changes, func(c *drive.Change, _ int) types.PutEventsRequestEntry {

		var source, detailType string
		switch c.ChangeType {
		case "file":
			source = fmt.Sprintf("%s/file/%s", sourcePrefix, c.FileId)
			switch {
			case c.Removed:
				detailType = "File Removed"
			case c.File != nil && c.File.Trashed:
				detailType = "File Move to trash"
			default:
				detailType = "File Changed"
			}
		case "drive":
			source = fmt.Sprintf("%s/drive/%s", sourcePrefix, c.DriveId)
			switch {
			case c.Removed:
				detailType = "Shared Drive Removed"
			default:
				detailType = "Drive Status Changed"
			}
		default:
			source = fmt.Sprintf("%s/%s", sourcePrefix, c.ChangeType)
			detailType = "Unexpected Changed"
			logx.Printf(ctx, "[warn] unexpected change type `%s`, check Drive API Document", c.ChangeType)
		}
		t, err := time.Parse(time.RFC3339Nano, c.Time)
		if err != nil {
			logx.Printf(ctx, "[warn] time Parse failed `%s`: %s", c.Time, err.Error())
			t = flextime.Now()
		}
		bs, err := json.Marshal(c)
		if err != nil {
			logx.Printf(ctx, "[warn] change marshal failed: %s", err.Error())
			bs = []byte("{}")
		}
		detail := string(bs)
		logx.Printf(ctx, "[debug] event source=%s, detail-type=%s detail: %s", source, detailType, detail)
		return types.PutEventsRequestEntry{
			EventBusName: aws.String(n.eventBus),
			Resources:    []string{},
			Source:       aws.String(source),
			DetailType:   aws.String(detailType),
			Time:         aws.Time(t),
			Detail:       aws.String(detail),
		}
	}), 10)
	var lastErr error
	for _, entries := range entriesChunk {
		output, err := n.client.PutEvents(ctx, &eventbridge.PutEventsInput{
			Entries: entries,
		})
		if err != nil {
			logx.Printf(ctx, "[error] PutEvents failed: %s", err.Error())
			lastErr = err
			continue
		}
		for i, entry := range output.Entries {
			if entry.ErrorCode != nil {
				logx.Printf(ctx, "[error] put event to %s error_code=%s, error_message=%s detail=%s", n.eventBus, *entry.ErrorCode, *entry.ErrorMessage, *entries[i].Detail)
				lastErr = fmt.Errorf("put events failed error_code=%s, error_message=%s", *entry.ErrorCode, *entry.ErrorMessage)
				continue
			}
			if entry.EventId != nil {
				logx.Printf(ctx, "[info] put event to %s event_id=%s", n.eventBus, *entry.EventId)
				continue
			}
		}
	}
	return lastErr
}

type FileNotification struct {
	eventFile string
}

func NewFileNotification(ctx context.Context, cfg *NotificationConfig) (*FileNotification, func() error, error) {
	n := &FileNotification{
		eventFile: *cfg.EventFile,
	}
	return n, nil, nil
}

func (n *FileNotification) SendChanges(ctx context.Context, _ *ChannelItem, changes []*drive.Change) error {
	fp, err := os.OpenFile(n.eventFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		logx.Printf(ctx, "[debug] can not crate notification event_file=%s:%s", n.eventFile, err.Error())
		return err
	}
	defer fp.Close()
	encoder := json.NewEncoder(fp)
	logx.Printf(ctx, "[info] output Changes events to `%s`", n.eventFile)
	var lastErr error
	for _, change := range changes {
		logx.Printf(ctx, "[debug] output changes event change_type:%s kind:%s file_id:%s drive_id:%s",
			coalesce(change.ChangeType, "-"),
			coalesce(change.Kind, "-"),
			coalesce(change.FileId, "-"),
			coalesce(change.DriveId, "-"),
		)
		if err := encoder.Encode(change); err != nil {
			lastErr = err
			logx.Printf(ctx, "[warn] FileNotification.SendChanges :%s", err.Error())
		}
	}
	return lastErr
}
