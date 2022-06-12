package gdnotify

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/Songmu/flextime"
	"github.com/gofrs/flock"
	logx "github.com/mashiike/go-logx"
	"github.com/shogo82148/go-retry"
)

type ChannelItem struct {
	ChannelID          string
	Expiration         time.Time
	PageToken          string
	ResourceID         string
	DriveID            string
	PageTokenFetchedAt time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (item *ChannelItem) IsAboutToExpired(ctx context.Context, remaining time.Duration) bool {
	now := flextime.Now()
	d := item.Expiration.Sub(now)
	logx.Printf(ctx, "[debug] IsAboutToExpired remaining=%s expiration=%s, now=%s, channel_id=%s, resource_id=%s, drive_id=%s ",
		d, item.Expiration.Format(time.RFC3339), now.Format(time.RFC3339), item.ChannelID, item.ResourceID, item.DriveID,
	)
	if d <= remaining {
		return true
	}
	return false
}

type Storage interface {
	FindAllChannels(context.Context) (<-chan []*ChannelItem, error)
	FindOneByChannelID(context.Context, string) (*ChannelItem, error)
	UpdatePageToken(context.Context, *ChannelItem) error
	SaveChannel(context.Context, *ChannelItem) error
	DeleteChannel(context.Context, *ChannelItem) error
}

type ChannelNotFound struct {
	ChannelID string
}

func (err *ChannelNotFound) Error() string {
	return fmt.Sprintf("channel_id:%s not found", err.ChannelID)
}

func NewStorage(ctx context.Context, cfg *StorageConfig) (Storage, func() error, error) {
	switch cfg.Type {
	case StorageTypeDynamoDB:
		return nil, nil, errors.New("not implemented yet")
	case StorageTypeFile:
		return NewFileStorage(ctx, cfg)
	}
	return nil, nil, errors.New("unknown storage type")
}

type FileStorage struct {
	Items []*ChannelItem

	LockFile string
	FilePath string
}

func NewFileStorage(ctx context.Context, cfg *StorageConfig) (*FileStorage, func() error, error) {
	s := &FileStorage{
		FilePath: *cfg.DataFile,
		LockFile: *cfg.LockFile,
	}

	return s, nil, nil
}

func (s *FileStorage) FindAllChannels(ctx context.Context) (<-chan []*ChannelItem, error) {
	ch := make(chan []*ChannelItem, 1)
	go func() {
		if err := s.transactional(ctx, func(context.Context) error {
			ch <- s.Items
			return nil
		}); err != nil {
			logx.Println(ctx, "[error] failed background channels read:", err)
		}
		close(ch)
	}()
	return ch, nil
}

func (s *FileStorage) SaveChannel(ctx context.Context, item *ChannelItem) error {
	return s.transactional(ctx, func(context.Context) error {
		for i, c := range s.Items {
			if c.ChannelID == item.ChannelID {
				s.Items[i] = item
				return nil
			}
		}
		s.Items = append(s.Items, item)
		return nil
	})
}

func (s *FileStorage) UpdatePageToken(ctx context.Context, target *ChannelItem) error {
	return s.transactional(ctx, func(context.Context) error {
		for i, c := range s.Items {
			if c.ChannelID == target.ChannelID {
				logx.Printf(ctx, "[debug] update PageToken channel_id=%s old_page_token=%s new_page_token=%s",
					s.Items[i].ChannelID, s.Items[i].PageToken, target.PageToken,
				)
				s.Items[i].PageToken = target.PageToken

				return nil
			}
		}
		return &ChannelNotFound{ChannelID: target.ChannelID}
	})
}

func (s *FileStorage) DeleteChannel(ctx context.Context, target *ChannelItem) error {
	return s.transactional(ctx, func(context.Context) error {
		for i, item := range s.Items {
			if target.ChannelID == item.ChannelID {
				s.Items = append(s.Items[:i], s.Items[i+1:]...)
				return nil
			}
		}
		return nil
	})
}

func (s *FileStorage) FindOneByChannelID(ctx context.Context, channelID string) (*ChannelItem, error) {
	var ret *ChannelItem
	if err := s.transactional(ctx, func(context.Context) error {
		for _, item := range s.Items {
			if item.ChannelID == channelID {
				ret = item
				logx.Printf(ctx, "[debug] found ChannelItem channel_id=%s resource_id=%s drive_id=%s:",
					ret.ChannelID, ret.ResourceID, ret.DriveID,
				)
				return nil
			}
		}
		return &ChannelNotFound{ChannelID: channelID}
	}); err != nil {
		logx.Println(ctx, "[debug] failed read:", err)
		return nil, err
	}
	logx.Printf(ctx, "[debug] return ChannelItem channel_id=%s resource_id=%s drive_id=%s:",
		ret.ChannelID, ret.ResourceID, ret.DriveID,
	)
	return ret, nil
}

func (s *FileStorage) transactional(ctx context.Context, fn func(context.Context) error) error {
	fileLock := flock.New(s.LockFile)
	policy := retry.Policy{
		MinDelay: 100 * time.Millisecond,
		MaxDelay: 1 * time.Second,
		MaxCount: 10,
		Jitter:   35 * time.Millisecond,
	}

	retirer := policy.Start(ctx)
	var err error
	var locked bool
	for retirer.Continue() {
		logx.Println(ctx, "[debug] try file storage lock:", s.LockFile)
		locked, err = fileLock.TryLock()
		if err != nil {
			logx.Println(ctx, "[debug] get file storage lock failed :", err)
			continue
		}
		if locked {
			logx.Println(ctx, "[debug] get file storage lock success")
			break
		}
	}
	if !locked {
		return fmt.Errorf("cannot get lock: %w", err)
	}
	defer func() {
		if err := fileLock.Unlock(); err != nil {
			logx.Println(ctx, "[debug] file storage unlock failed: ", err)
			return
		}
		logx.Println(ctx, "[debug] file storage unlock success")
	}()
	if err := s.restore(ctx); err != nil {
		return err
	}
	if err := fn(ctx); err != nil {
		logx.Println(ctx, "[debug] transactional function failed:", err)
		return err
	}
	if err := s.store(ctx); err != nil {
		return err
	}
	logx.Println(ctx, "[debug] file storage store success")
	return nil
}

func (s *FileStorage) restore(ctx context.Context) error {
	fp, err := os.Open(s.FilePath)
	if err != nil {
		logx.Println(ctx, "[warn] failed restore failed:", err)
		return nil
	}
	defer fp.Close()
	decoder := gob.NewDecoder(fp)
	if err := decoder.Decode(s); err != nil && err != io.EOF {
		log.Printf("[error] failed restore file storage: %s", err.Error())
		return err
	}
	return nil
}

func (s *FileStorage) store(ctx context.Context) error {
	fp, err := os.Create(s.FilePath)
	if err != nil {
		logx.Printf(ctx, "[error] failed store to file storage: create file: %s", err.Error())
		return err
	}
	defer fp.Close()
	encoder := gob.NewEncoder(fp)
	if err := encoder.Encode(s); err != nil {
		logx.Printf(ctx, "[error] failed store to file storage: encode gob: %s", err.Error())
		return err
	}
	log.Printf("[debug] file storage store to `%s`", s.FilePath)
	return nil
}
