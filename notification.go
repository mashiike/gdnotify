package gdnotify

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	logx "github.com/mashiike/go-logx"
	"google.golang.org/api/drive/v3"
)

type Notification interface {
	SendChanges(context.Context, []*drive.Change) error
}

func NewNotification(ctx context.Context, cfg *NotificationConfig) (Notification, func() error, error) {
	switch cfg.Type {
	case NotificationTypeEventBridge:
		return nil, nil, errors.New("not implemented yet")
	case NotificationTypeFile:
		return NewFileNotification(ctx, cfg)
	}
	return nil, nil, errors.New("unknown storage type")
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

func (n *FileNotification) SendChanges(ctx context.Context, changes []*drive.Change) error {
	if len(changes) == 0 {
		return nil
	}
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
