package gdnotify

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/Songmu/flextime"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
	"github.com/gofrs/flock"
	logx "github.com/mashiike/go-logx"
	"github.com/samber/lo"
	"github.com/shogo82148/go-retry"
)

type StorageOption struct {
	Type       string `help:"storage type" default:"dynamodb" enum:"dynamodb,file" env:"GDNOTIFY_STORAGE_TYPE"`
	TableName  string `help:"dynamodb table name" default:"gdnotify" env:"GDNOTIFY_DDB_TABLE_NAME"`
	AutoCreate bool   `help:"auto create dynamodb table" default:"false" env:"GDNOTIFY_DDB_AUTO_CREATE" negatable:""`
	DataFile   string `help:"file storage data file" default:"gdnotify.dat" env:"GDNOTIFY_FILE_STORAGE_DATA_FILE"`
	LockFile   string `help:"file storage lock file" default:"gdnotify.lock" env:"GDNOTIFY_FILE_STORAGE_LOCK_FILE"`
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
	logx.Printf(ctx, "[debug] IsAboutToExpired remaining=%s expiration=%s, now=%s, channel_id=%s, resource_id=%s, drive_id=%s ",
		d, item.Expiration.Format(time.RFC3339), now.Format(time.RFC3339), item.ChannelID, item.ResourceID, item.DriveID,
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

type ChannelNotFound struct {
	ChannelID string
}

func (err *ChannelNotFound) Error() string {
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
	s := &DynamoDBStorage{
		client:    dynamodb.NewFromConfig(awsCfg),
		tableName: cfg.TableName,
	}
	logx.Printf(ctx, "[info] check describe dynamodb table `%s`", s.tableName)
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

func (s *DynamoDBStorage) Close() error {
	return nil
}

func (s *DynamoDBStorage) tableExists(ctx context.Context) (bool, error) {
	logx.Printf(ctx, "[debug] check describe dynamodb table `%s`", s.tableName)
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
		logx.Println(ctx, "[debug] DescribeTable: ", err)
		return false, err
	}
	logx.Printf(ctx, "[debug] exists table `%s` status is `%s`", s.tableName, table.Table.TableStatus)
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
	logx.Printf(ctx, "[debug] start wait dynamodb table `%s` active", s.tableName)
	for retrier.Continue() {
		exists, err = s.tableExists(ctx)
		if err == nil && exists {
			return nil
		}
	}
	logx.Printf(ctx, "[debug] timeout wait dynamodb table `%s` active", s.tableName)
	if err == nil {
		return fmt.Errorf("table not active")
	}
	return fmt.Errorf("table not active: %w", err)
}

func (s *DynamoDBStorage) createTable(ctx context.Context) error {
	logx.Printf(ctx, "[debug] create dynamodb table `%s`", s.tableName)
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
				logx.Printf(ctx, "[debug] crate dynamodb table `%s` ResourceInUseException: wait table active", s.tableName)
				if err := s.waitTableActive(ctx); err != nil {
					return err
				}
				return nil
			}
		}
		logx.Println(ctx, "[debug] CreateTable failed: ", err)
		return err
	}
	logx.Printf(ctx, "[info] create dynamodb table `%s`", *output.TableDescription.TableArn)
	if err := s.waitTableActive(ctx); err != nil {
		return err
	}
	return nil
}

func (s *DynamoDBStorage) FindAllChannels(ctx context.Context) (<-chan []*ChannelItem, error) {
	logx.Printf(ctx, "[debug] scan dynamodb table `%s`", s.tableName)
	output, err := s.client.Scan(ctx, &dynamodb.ScanInput{
		TableName:      aws.String(s.tableName),
		Select:         types.SelectAllAttributes,
		ConsistentRead: aws.Bool(false),
	})
	if err != nil {
		logx.Printf(ctx, "[debug] scan dynamodb table failed: %s", err.Error())
		return nil, err
	}
	logx.Printf(ctx, "[debug] scan dynamodb table success item_count=%d", output.Count)
	ch := make(chan []*ChannelItem, 10)
	ch <- lo.Map(output.Items, func(values map[string]types.AttributeValue, _ int) *ChannelItem {
		return NewChannelItemWithDynamoDBAttributeValues(values)
	})
	if output.LastEvaluatedKey == nil {
		logx.Printf(ctx, "[debug] LastEvaluatedKey is null return FindAllChannels")
		close(ch)
		return ch, nil
	}
	logx.Printf(ctx, "[debug] need background scan dynamodb table")
	go func() {
		logx.Printf(ctx, "[debug] start background scan dynamodb table `%s`", s.tableName)
		defer func() {
			logx.Printf(ctx, "[debug] finish background scan dynamodb table `%s`", s.tableName)
			close(ch)
		}()
		for output.LastEvaluatedKey != nil {
			output, err = s.client.Scan(ctx, &dynamodb.ScanInput{
				TableName:      aws.String(s.tableName),
				Select:         types.SelectAllAttributes,
				ConsistentRead: aws.Bool(false),
			})
			if err != nil {
				logx.Printf(ctx, "[error] background scan dynamodb table failed: %s", err.Error())
				return
			}
			logx.Printf(ctx, "[debug] background scan dynamodb table success item_count=%d", output.Count)
			ch <- lo.Map(output.Items, func(values map[string]types.AttributeValue, _ int) *ChannelItem {
				return NewChannelItemWithDynamoDBAttributeValues(values)
			})
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return ch, nil
}

func (s *DynamoDBStorage) SaveChannel(ctx context.Context, item *ChannelItem) error {
	logx.Printf(ctx, "[debug] put item channel_id=`%s` to dynamodb table `%s`", item.ChannelID, s.tableName)
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.tableName),
		Item:                item.ToDynamoDBAttributeValues(),
		ConditionExpression: aws.String("attribute_not_exists(ChannelID)"),
	})
	if err != nil {
		var ae smithy.APIError
		logx.Printf(ctx, "[warn] failed put item channel_id=`%s` resource_id=%s to dynamodb table `%s`: %s", item.ChannelID, item.ResourceID, s.tableName, err.Error())
		if errors.As(err, &ae) {
			if ae.ErrorCode() == "ConditionalCheckFailedException" {
				return &ChannelAlreadyExists{ChannelID: item.ChannelID}
			}
		}
		return err
	}
	logx.Printf(ctx, "[info] put item channel_id=`%s` to dynamodb table `%s`", item.ChannelID, s.tableName)
	return nil
}

func (s *DynamoDBStorage) UpdatePageToken(ctx context.Context, target *ChannelItem) error {
	logx.Printf(ctx, "[debug] update item channel_id=`%s` to dynamodb table `%s`", target.ChannelID, s.tableName)
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
		logx.Printf(ctx, "[warn] failed update item channel_id=`%s` to dynamodb table `%s` page_token=%s", target.ChannelID, s.tableName, target.PageToken)
		return err
	}
	logx.Printf(ctx, "[info] update item channel_id=`%s` to dynamodb table `%s` page_token=%s", target.ChannelID, s.tableName, target.PageToken)
	return nil
}

func (s *DynamoDBStorage) DeleteChannel(ctx context.Context, target *ChannelItem) error {
	logx.Printf(ctx, "[debug] delete item channel_id=`%s` from dynamodb table `%s`", target.ChannelID, s.tableName)
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
		logx.Printf(ctx, "[warn] failed delete item channel_id=`%s` resource_id=%s from dynamodb table `%s`", target.ChannelID, target.ResourceID, s.tableName)
		return err
	}
	logx.Printf(ctx, "[info] delete item channel_id=`%s` resource_id=`%s` from dynamodb table `%s`", target.ChannelID, target.ChannelID, s.tableName)
	return nil
}

func (s *DynamoDBStorage) FindOneByChannelID(ctx context.Context, channelID string) (*ChannelItem, error) {
	logx.Printf(ctx, "[debug] get item channel_id=`%s` from dynamodb table `%s`", channelID, s.tableName)
	output, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"ChannelID": &types.AttributeValueMemberS{
				Value: channelID,
			},
		},
	})
	if err != nil {
		logx.Printf(ctx, "[warn] failed get item channel_id=`%s` from dynamodb table `%s`", channelID, s.tableName)
		return nil, err
	}
	logx.Printf(ctx, "[debug] success get item channel_id=`%s` from dynamodb table `%s`", channelID, s.tableName)
	return NewChannelItemWithDynamoDBAttributeValues(output.Item), nil
}

type FileStorage struct {
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

	retrier := policy.Start(ctx)
	var err error
	var locked bool
	for retrier.Continue() {
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
