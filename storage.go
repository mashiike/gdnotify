package gdnotify

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Songmu/flextime"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
	"github.com/gofrs/flock"
	"github.com/shogo82148/go-retry"
)

type StorageOption struct {
	Type             string `help:"storage type" default:"dynamodb" enum:"dynamodb,file" env:"GDNOTIFY_STORAGE_TYPE"`
	TableName        string `help:"dynamodb table name" default:"gdnotify" env:"GDNOTIFY_DDB_TABLE_NAME"`
	AutoCreate       bool   `help:"auto create dynamodb table" default:"false" env:"GDNOTIFY_DDB_AUTO_CREATE" negatable:""`
	DynamoDBEndpoint string `help:"dynamodb endpoint" env:"GDNOTIFY_DDB_ENDPOINT"`
	DataFile         string `help:"file storage data file" default:"gdnotify.dat" env:"GDNOTIFY_FILE_STORAGE_DATA_FILE"`
	LockFile         string `help:"file storage lock file" default:"gdnotify.lock" env:"GDNOTIFY_FILE_STORAGE_LOCK_FILE"`
}

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
	slog.DebugContext(ctx, "IsAboutToExpired",
		"remaining", remaining,
		"expiration", item.Expiration.Format(time.RFC3339),
		"now", now.Format(time.RFC3339),
		"channel_id", item.ChannelID,
		"resource_id", item.ResourceID,
		"drive_id", item.DriveID,
	)
	return d <= remaining
}

func GetAttributeValueAs[T types.AttributeValue](key string, values map[string]types.AttributeValue) (T, bool) {
	var empty T
	value, ok := values[key]
	if !ok {
		return empty, false
	}
	if v, ok := value.(T); ok {
		return v, true
	}
	return empty, false
}

func NewChannelItemWithDynamoDBAttributeValues(values map[string]types.AttributeValue) *ChannelItem {
	item := &ChannelItem{}
	channelIDValue, ok := GetAttributeValueAs[*types.AttributeValueMemberS]("ChannelID", values)
	if ok {
		item.ChannelID = channelIDValue.Value
	}
	expirationValue, ok := GetAttributeValueAs[*types.AttributeValueMemberN]("Expiration", values)
	if ok {
		if expiration, err := strconv.ParseFloat(expirationValue.Value, 64); err == nil {
			item.Expiration = time.UnixMilli(int64(expiration))
		}
	}
	pageTokenValue, ok := GetAttributeValueAs[*types.AttributeValueMemberS]("PageToken", values)
	if ok {
		item.PageToken = pageTokenValue.Value
	}
	resourceIDValue, ok := GetAttributeValueAs[*types.AttributeValueMemberS]("ResourceID", values)
	if ok {
		item.ResourceID = resourceIDValue.Value
	}
	driveIDValue, ok := GetAttributeValueAs[*types.AttributeValueMemberS]("DriveID", values)
	if ok {
		item.DriveID = driveIDValue.Value
	}
	pageTokenFetchedAtValue, ok := GetAttributeValueAs[*types.AttributeValueMemberN]("PageTokenFetchedAt", values)
	if ok {
		if pageTokenFetchedAt, err := strconv.ParseFloat(pageTokenFetchedAtValue.Value, 64); err == nil {
			item.PageTokenFetchedAt = time.UnixMilli(int64(pageTokenFetchedAt))
		}
	}
	createdAtValue, ok := GetAttributeValueAs[*types.AttributeValueMemberN]("CreatedAt", values)
	if ok {
		if createdAt, err := strconv.ParseFloat(createdAtValue.Value, 64); err == nil {
			item.CreatedAt = time.UnixMilli(int64(createdAt))
		}
	}
	updatedAtValue, ok := GetAttributeValueAs[*types.AttributeValueMemberN]("UpdatedAt", values)
	if ok {
		if updatedAt, err := strconv.ParseFloat(updatedAtValue.Value, 64); err == nil {
			item.UpdatedAt = time.UnixMilli(int64(updatedAt))
		}
	}
	return item
}

func (item *ChannelItem) ToDynamoDBAttributeValues() map[string]types.AttributeValue {
	expiration := strconv.FormatFloat(float64(item.Expiration.UnixMilli()), 'f', -1, 64)
	pageTokenFetchedAt := strconv.FormatFloat(float64(item.PageTokenFetchedAt.UnixMilli()), 'f', -1, 64)
	createdAt := strconv.FormatFloat(float64(item.CreatedAt.UnixMilli()), 'f', -1, 64)
	updatedAt := strconv.FormatFloat(float64(item.UpdatedAt.UnixMilli()), 'f', -1, 64)
	values := map[string]types.AttributeValue{
		"ChannelID": &types.AttributeValueMemberS{
			Value: item.ChannelID,
		},
		"Expiration": &types.AttributeValueMemberN{
			Value: expiration,
		},
		"PageToken": &types.AttributeValueMemberS{
			Value: item.PageToken,
		},
		"ResourceID": &types.AttributeValueMemberS{
			Value: item.ResourceID,
		},
		"DriveID": &types.AttributeValueMemberS{
			Value: item.DriveID,
		},
		"PageTokenFetchedAt": &types.AttributeValueMemberN{
			Value: pageTokenFetchedAt,
		},
		"CreatedAt": &types.AttributeValueMemberN{
			Value: createdAt,
		},
		"UpdatedAt": &types.AttributeValueMemberN{
			Value: updatedAt,
		},
	}
	return values
}

type Storage interface {
	FindAllChannels(context.Context) (<-chan []*ChannelItem, error)
	FindOneByChannelID(context.Context, string) (*ChannelItem, error)
	UpdatePageToken(context.Context, *ChannelItem) error
	SaveChannel(context.Context, *ChannelItem) error
	DeleteChannel(context.Context, *ChannelItem) error
}

type ChannelNotFoundError struct {
	ChannelID string
}

func (err *ChannelNotFoundError) Error() string {
	return fmt.Sprintf("channel_id:%s not found", err.ChannelID)
}

type ChannelAlreadyExists struct {
	ChannelID string
}

func (err *ChannelAlreadyExists) Error() string {
	return fmt.Sprintf("channel_id:%s already exists", err.ChannelID)
}

func NewStorage(ctx context.Context, cfg StorageOption) (Storage, error) {
	switch cfg.Type {
	case "dynamodb":
		return NewDynamoDBStorage(ctx, cfg)
	case "file":
		return NewFileStorage(ctx, cfg)
	}
	return nil, errors.New("unknown storage type")
}

type DynamoDBStorage struct {
	client    *dynamodb.Client
	tableName string
}

func NewDynamoDBStorage(ctx context.Context, cfg StorageOption) (*DynamoDBStorage, error) {
	awsCfg, err := loadAWSConfig()
	if err != nil {
		return nil, err
	}
	opts := []func(*dynamodb.Options){}
	if cfg.DynamoDBEndpoint != "" {
		opts = append(opts, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(cfg.DynamoDBEndpoint)
		})
	}
	s := &DynamoDBStorage{
		client:    dynamodb.NewFromConfig(awsCfg, opts...),
		tableName: cfg.TableName,
	}
	slog.InfoContext(ctx, "check describe dynamodb table", "table_name", s.tableName)
	exists, err := s.tableExists(ctx)
	if err != nil {
		return nil, err
	}
	if !exists && cfg.AutoCreate {
		if err := s.createTable(ctx); err != nil {
			return nil, err
		}
	}

	return s, nil
}

func (s *DynamoDBStorage) tableExists(ctx context.Context) (bool, error) {
	slog.DebugContext(ctx, "check describe dynamodb table", "table_name", s.tableName)
	table, err := s.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(s.tableName),
	})
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) {
			if ae.ErrorCode() == "ResourceNotFoundException" {
				return false, nil
			}
		}
		slog.DebugContext(ctx, "DescribeTable", "error", err)
		return false, err
	}
	slog.DebugContext(ctx, "exists table", "table_name", s.tableName, "status", table.Table.TableStatus)
	if table.Table.TableStatus == types.TableStatusActive || table.Table.TableStatus == types.TableStatusUpdating {
		return true, nil
	}
	return false, nil
}

func (s *DynamoDBStorage) waitTableActive(ctx context.Context) error {
	policy := retry.Policy{
		MinDelay: 200 * time.Millisecond,
		MaxDelay: 2 * time.Second,
		MaxCount: 20,
		Jitter:   100 * time.Millisecond,
	}

	retrier := policy.Start(ctx)
	var err error
	var exists bool
	slog.DebugContext(ctx, "start wait dynamodb table active", "table_name", s.tableName)
	for retrier.Continue() {
		exists, err = s.tableExists(ctx)
		if err == nil && exists {
			return nil
		}
	}
	slog.DebugContext(ctx, "timeout wait dynamodb table active", "table_name", s.tableName)
	if err == nil {
		return fmt.Errorf("table not active")
	}
	return fmt.Errorf("table not active: %w", err)
}

func (s *DynamoDBStorage) createTable(ctx context.Context) error {
	slog.DebugContext(ctx, "create dynamodb table", "table_name", s.tableName)
	output, err := s.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(s.tableName),
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("ChannelID"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("ChannelID"),
				KeyType:       types.KeyTypeHash,
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) {
			if ae.ErrorCode() == "ResourceInUseException" {
				slog.DebugContext(ctx, "crate dynamodb table ResourceInUseException: wait table active", "table_name", s.tableName)
				if err := s.waitTableActive(ctx); err != nil {
					return err
				}
				return nil
			}
		}
		slog.DebugContext(ctx, "CreateTable failed", "error", err)
		return err
	}
	slog.InfoContext(ctx, "create dynamodb table", "table_arn", *output.TableDescription.TableArn)
	if err := s.waitTableActive(ctx); err != nil {
		return err
	}
	return nil
}

func (s *DynamoDBStorage) FindAllChannels(ctx context.Context) (<-chan []*ChannelItem, error) {
	slog.DebugContext(ctx, "scan dynamodb table", "table_name", s.tableName)
	output, err := s.client.Scan(ctx, &dynamodb.ScanInput{
		TableName:      aws.String(s.tableName),
		Select:         types.SelectAllAttributes,
		ConsistentRead: aws.Bool(false),
	})
	if err != nil {
		slog.DebugContext(ctx, "scan dynamodb table failed", "error", err)
		return nil, err
	}
	slog.DebugContext(ctx, "scan dynamodb table success", "item_count", output.Count)
	ch := make(chan []*ChannelItem, 10)
	ch <- Map(output.Items, func(values map[string]types.AttributeValue) *ChannelItem {
		return NewChannelItemWithDynamoDBAttributeValues(values)
	})
	if output.LastEvaluatedKey == nil {
		slog.DebugContext(ctx, "LastEvaluatedKey is null return FindAllChannels")
		close(ch)
		return ch, nil
	}
	slog.DebugContext(ctx, "need background scan dynamodb table")
	go func() {
		slog.DebugContext(ctx, "start background scan dynamodb table", "table_name", s.tableName)
		defer func() {
			slog.DebugContext(ctx, "finish background scan dynamodb table", "table_name", s.tableName)
			close(ch)
		}()
		for output.LastEvaluatedKey != nil {
			output, err = s.client.Scan(ctx, &dynamodb.ScanInput{
				TableName:      aws.String(s.tableName),
				Select:         types.SelectAllAttributes,
				ConsistentRead: aws.Bool(false),
			})
			if err != nil {
				slog.ErrorContext(ctx, "background scan dynamodb table failed", "error", err)
				return
			}
			slog.DebugContext(ctx, "background scan dynamodb table success", "item_count", output.Count)
			ch <- Map(output.Items, func(values map[string]types.AttributeValue) *ChannelItem {
				return NewChannelItemWithDynamoDBAttributeValues(values)
			})
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return ch, nil
}

func (s *DynamoDBStorage) SaveChannel(ctx context.Context, item *ChannelItem) error {
	slog.DebugContext(ctx, "put item to dynamodb table", "channel_id", item.ChannelID, "table_name", s.tableName)
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.tableName),
		Item:                item.ToDynamoDBAttributeValues(),
		ConditionExpression: aws.String("attribute_not_exists(ChannelID)"),
	})
	if err != nil {
		var ae smithy.APIError
		slog.WarnContext(ctx, "failed put item to dynamodb table", "channel_id", item.ChannelID, "resource_id", item.ResourceID, "table_name", s.tableName, "error", err)
		if errors.As(err, &ae) {
			if ae.ErrorCode() == "ConditionalCheckFailedException" {
				return &ChannelAlreadyExists{ChannelID: item.ChannelID}
			}
		}
		return err
	}
	slog.InfoContext(ctx, "put item to dynamodb table", "channel_id", item.ChannelID, "table_name", s.tableName)
	return nil
}

func (s *DynamoDBStorage) UpdatePageToken(ctx context.Context, target *ChannelItem) error {
	slog.DebugContext(ctx, "update item to dynamodb table", "channel_id", target.ChannelID, "table_name", s.tableName)
	values := target.ToDynamoDBAttributeValues()
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"ChannelID": &types.AttributeValueMemberS{
				Value: target.ChannelID,
			},
		},
		UpdateExpression:    aws.String("SET #PageToken=:PageToken,#UpdatedAt=:UpdatedAt"),
		ConditionExpression: aws.String("attribute_exists(ChannelID) AND UpdatedAt < :UpdatedAt"),
		ExpressionAttributeNames: map[string]string{
			"#PageToken": "PageToken",
			"#UpdatedAt": "UpdatedAt",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":UpdatedAt": values["UpdatedAt"],
			":PageToken": values["PageToken"],
		},
	})
	if err != nil {
		slog.WarnContext(ctx, "failed update item to dynamodb table", "channel_id", target.ChannelID, "table_name", s.tableName, "page_token", target.PageToken)
		return err
	}
	slog.InfoContext(ctx, "update item to dynamodb table", "channel_id", target.ChannelID, "table_name", s.tableName, "page_token", target.PageToken)
	return nil
}

func (s *DynamoDBStorage) DeleteChannel(ctx context.Context, target *ChannelItem) error {
	slog.DebugContext(ctx, "delete item from dynamodb table", "channel_id", target.ChannelID, "table_name", s.tableName)
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"ChannelID": &types.AttributeValueMemberS{
				Value: target.ChannelID,
			},
		},
		ConditionExpression: aws.String("attribute_exists(ChannelID)"),
	})
	if err != nil {
		slog.WarnContext(ctx, "failed delete item from dynamodb table", "channel_id", target.ChannelID, "resource_id", target.ResourceID, "table_name", s.tableName)
		return err
	}
	slog.InfoContext(ctx, "delete item from dynamodb table", "channel_id", target.ChannelID, "resource_id", target.ChannelID, "table_name", s.tableName)
	return nil
}

func (s *DynamoDBStorage) FindOneByChannelID(ctx context.Context, channelID string) (*ChannelItem, error) {
	slog.DebugContext(ctx, "get item from dynamodb table", "channel_id", channelID, "table_name", s.tableName)
	output, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"ChannelID": &types.AttributeValueMemberS{
				Value: channelID,
			},
		},
	})
	if err != nil {
		slog.WarnContext(ctx, "get item failed", "channel_id", channelID, "table_name", s.tableName, "error", err)
		return nil, err
	}
	if output.Item == nil {
		slog.WarnContext(ctx, "not found item", "channel_id", channelID, "table_name", s.tableName)
		return nil, &ChannelNotFoundError{ChannelID: channelID}
	}
	slog.DebugContext(ctx, "success get item", "channel_id", channelID, "table_name", s.tableName)
	return NewChannelItemWithDynamoDBAttributeValues(output.Item), nil
}

type FileStorage struct {
	mu    sync.Mutex
	Items []*ChannelItem

	LockFile string
	FilePath string
}

func NewFileStorage(ctx context.Context, cfg StorageOption) (*FileStorage, error) {
	s := &FileStorage{
		FilePath: cfg.DataFile,
		LockFile: cfg.LockFile,
	}

	return s, nil
}

func (s *FileStorage) FindAllChannels(ctx context.Context) (<-chan []*ChannelItem, error) {
	ch := make(chan []*ChannelItem, 1)
	go func() {
		if err := s.transactional(ctx, func(context.Context) error {
			ch <- s.Items
			return nil
		}); err != nil {
			slog.ErrorContext(ctx, "failed background channels read", "error", err)
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
				slog.DebugContext(ctx, "update PageToken", "channel_id", s.Items[i].ChannelID, "old_page_token", s.Items[i].PageToken, "new_page_token", target.PageToken)
				s.Items[i].PageToken = target.PageToken

				return nil
			}
		}
		return &ChannelNotFoundError{ChannelID: target.ChannelID}
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
				slog.DebugContext(ctx, "found ChannelItem", "channel_id", ret.ChannelID, "resource_id", ret.ResourceID, "drive_id", ret.DriveID)
				return nil
			}
		}
		return &ChannelNotFoundError{ChannelID: channelID}
	}); err != nil {
		slog.DebugContext(ctx, "failed read", "error", err)
		return nil, err
	}
	slog.DebugContext(ctx, "return ChannelItem", "channel_id", ret.ChannelID, "resource_id", ret.ResourceID, "drive_id", ret.DriveID)
	return ret, nil
}

func (s *FileStorage) transactional(ctx context.Context, fn func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fileLock := flock.New(s.LockFile)
	policy := retry.Policy{
		MinDelay: 100 * time.Millisecond,
		MaxDelay: 1 * time.Second,
		MaxCount: 10,
		Jitter:   35 * time.Millisecond,
	}

	retrier := policy.Start(ctx)
	var err error
	var locked bool
	for retrier.Continue() {
		slog.DebugContext(ctx, "try file storage lock", "lock_file", s.LockFile)
		locked, err = fileLock.TryLock()
		if err != nil {
			slog.DebugContext(ctx, "get file storage lock failed", "error", err)
			continue
		}
		if locked {
			slog.DebugContext(ctx, "get file storage lock success")
			break
		}
	}
	if !locked {
		return fmt.Errorf("cannot get lock: %w", err)
	}
	defer func() {
		if err := fileLock.Unlock(); err != nil {
			slog.DebugContext(ctx, "file storage unlock failed", "error", err)
			return
		}
		slog.DebugContext(ctx, "file storage unlock success")
	}()
	if err := s.restore(ctx); err != nil {
		return err
	}
	if err := fn(ctx); err != nil {
		slog.DebugContext(ctx, "transactional function failed", "error", err)
		return err
	}
	if err := s.store(ctx); err != nil {
		return err
	}
	slog.DebugContext(ctx, "file storage store success")
	return nil
}

func (s *FileStorage) restore(ctx context.Context) error {
	fp, err := os.Open(s.FilePath)
	if err != nil {
		slog.WarnContext(ctx, "failed restore", "error", err)
		return nil
	}
	defer fp.Close()
	decoder := gob.NewDecoder(fp)
	if err := decoder.Decode(s); err != nil && err != io.EOF {
		slog.ErrorContext(ctx, "failed restore file storage", "error", err)
		return err
	}
	return nil
}

func (s *FileStorage) store(ctx context.Context) error {
	fp, err := os.Create(s.FilePath)
	if err != nil {
		slog.ErrorContext(ctx, "failed store to file storage: create file", "error", err)
		return err
	}
	defer fp.Close()
	encoder := gob.NewEncoder(fp)
	if err := encoder.Encode(s); err != nil {
		slog.ErrorContext(ctx, "failed store to file storage: encode gob", "error", err)
		return err
	}
	slog.DebugContext(ctx, "file storage store", "file_path", s.FilePath)
	return nil
}
