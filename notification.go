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

func NewNotification(ctx context.Context, cfg *NotificationConfig) (Notification, func() error, error) {
	switch cfg.Type {
	case NotificationTypeEventBridge:
		return NewEventBridgeNotification(ctx, cfg)
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

func NewEventBridgeNotification(ctx context.Context, cfg *NotificationConfig) (Notification, func() error, error) {
	awsCfg, err := loadAWSConfig()
	if err != nil {
		return nil, nil, err
	}
	n := &EventBridgeNotification{
		client:   eventbridge.NewFromConfig(awsCfg),
		eventBus: *cfg.EventBus,
	}
	return n, nil, nil
}

type TargetEntity struct {
	Id          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	CreatedTime string `json:"createdTime"`
}
type ChangeEventDetail struct {
	Subject string        `json:"subject"`
	Entity  *TargetEntity `json:"entity"`
	Actor   *drive.User   `json:"actor"`
	Change  *drive.Change `json:"change"`
}

const (
	DetailTypeFileRemoved  = "File Removed"
	DetailTypeFileTrashed  = "File Move to trash"
	DetailTypeFileChanged  = "File Changed"
	DetailTypeDriveRemoved = "Shared Drive Removed"
	DetailTypeDriveChanged = "Drive Status Changed"
)

func (e *ChangeEventDetail) MarshalJSON() ([]byte, error) {
	switch e.DetailType() {
	case DetailTypeFileRemoved:
		e.Subject = fmt.Sprintf("FileID %s was removed at %s", e.Change.FileId, e.Change.Time)
	case DetailTypeFileTrashed:
		if e.Change.File != nil {
			if e.Change.File.TrashingUser != nil {
				var user string
				if e.Change.File.TrashingUser.EmailAddress == "" {
					user = e.Change.File.TrashingUser.DisplayName
				} else {
					user = fmt.Sprintf("%s [%s]", e.Change.File.TrashingUser.DisplayName, e.Change.File.TrashingUser.EmailAddress)
				}
				e.Subject = fmt.Sprintf("File %s (%s) moved to trash by %s at %s", e.Change.File.Name, e.Change.FileId, user, e.Change.File.TrashedTime)
				e.Actor = e.Change.File.TrashingUser
			} else {
				e.Subject = fmt.Sprintf("File %s (%s) moved to trash at %s", e.Change.File.Name, e.Change.FileId, e.Change.Time)
			}
		} else {
			e.Subject = fmt.Sprintf("FileID %s  moved to trash at %s", e.Change.FileId, e.Change.Time)
		}
	case DetailTypeFileChanged:
		if e.Change.File != nil {
			if e.Change.File.LastModifyingUser != nil {
				var user string
				if e.Change.File.LastModifyingUser.EmailAddress == "" {
					user = e.Change.File.LastModifyingUser.DisplayName
				} else {
					user = fmt.Sprintf("%s [%s]", e.Change.File.LastModifyingUser.DisplayName, e.Change.File.LastModifyingUser.EmailAddress)
				}
				e.Subject = fmt.Sprintf("File %s (%s) changed by %s at %s", e.Change.File.Name, e.Change.FileId, user, e.Change.File.ModifiedTime)
				e.Actor = e.Change.File.LastModifyingUser
			} else {
				e.Subject = fmt.Sprintf("File %s (%s) changed at %s", e.Change.File.Name, e.Change.FileId, e.Change.Time)
			}
		} else {
			e.Subject = fmt.Sprintf("FileID %s changed at %s", e.Change.FileId, e.Change.Time)
		}
	case DetailTypeDriveRemoved:
		e.Subject = fmt.Sprintf("DriveId %s was removed at %s", e.Change.DriveId, e.Change.Time)
	case DetailTypeDriveChanged:
		if e.Change.Drive != nil {
			e.Subject = fmt.Sprintf("Drive %s (%s) changed at %s", e.Change.Drive.Name, e.Change.DriveId, e.Change.Time)
		} else {
			e.Subject = fmt.Sprintf("DriveId %s changed at %s", e.Change.DriveId, e.Change.Time)
		}
	}
	if e.Actor == nil {
		e.Actor = &drive.User{
			Kind:        "drive#user",
			DisplayName: "Unknown User",
		}
	}
	e.Actor.ForceSendFields = []string{"EmailAddress", "DisplayName", "Kind"}
	switch {
	case e.Change.Drive != nil:
		e.Entity = &TargetEntity{
			Id:          e.Change.Drive.Id,
			Kind:        e.Change.Drive.Kind,
			Name:        e.Change.Drive.Name,
			CreatedTime: e.Change.Drive.CreatedTime,
		}
	case e.Change.File != nil:
		e.Entity = &TargetEntity{
			Id:          e.Change.File.Id,
			Kind:        e.Change.File.Kind,
			Name:        e.Change.File.Name,
			CreatedTime: e.Change.File.CreatedTime,
		}
	case e.Change.DriveId != "":
		e.Entity = &TargetEntity{
			Id:   e.Change.DriveId,
			Kind: "drive#drive",
		}
	case e.Change.FileId != "":
		e.Entity = &TargetEntity{
			Id:   e.Change.FileId,
			Kind: "drive#file",
		}
	}
	type NoMethod ChangeEventDetail
	data := NoMethod(*e)
	return json.Marshal(data)
}

func (e *ChangeEventDetail) DetailType() string {
	switch e.Change.ChangeType {
	case "file":
		switch {
		case e.Change.Removed:
			return DetailTypeFileRemoved
		case e.Change.File != nil && e.Change.File.Trashed:
			return DetailTypeFileTrashed
		default:
			return DetailTypeFileChanged
		}
	case "drive":
		switch {
		case e.Change.Removed:
			return DetailTypeDriveRemoved
		default:
			return DetailTypeDriveChanged
		}
	default:
		return "Unexpected Changed"
	}
}
func (e *ChangeEventDetail) Source(sourcePrefix string) string {
	switch e.Change.ChangeType {
	case "file":
		return fmt.Sprintf("%s/file/%s", sourcePrefix, e.Change.FileId)
	case "drive":
		return fmt.Sprintf("%s/drive/%s", sourcePrefix, e.Change.DriveId)
	default:
		return fmt.Sprintf("%s/%s", sourcePrefix, e.Change.ChangeType)
	}
}

func (n *EventBridgeNotification) SendChanges(ctx context.Context, item *ChannelItem, changes []*drive.Change) error {
	sourcePrefix := fmt.Sprintf("oss.gdnotify/%s", item.DriveID)
	entriesChunk := lo.Chunk(lo.Map(changes, func(c *drive.Change, _ int) types.PutEventsRequestEntry {

		t, err := time.Parse(time.RFC3339Nano, c.Time)
		if err != nil {
			logx.Printf(ctx, "[warn] time Parse failed `%s`: %s", c.Time, err.Error())
			t = flextime.Now()
		}
		ced := &ChangeEventDetail{
			Change: c,
		}
		bs, err := json.Marshal(ced)
		if err != nil {
			logx.Printf(ctx, "[warn] change marshal failed: %s", err.Error())
			bs = []byte("{}")
		}
		detail := string(bs)
		source := ced.Source(sourcePrefix)
		detailType := ced.DetailType()
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
