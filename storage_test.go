package gdnotify_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/Songmu/flextime"
	"github.com/google/uuid"
	"github.com/mashiike/gdnotify"
	"github.com/najeira/randstr"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestConvertChannelItemDynamoDBAttributeValues(t *testing.T) {
	N := 10
	items := make([]*gdnotify.ChannelItem, 0, N)
	r := rand.New(rand.NewSource(time.Now().Unix()))
	for i := 0; i < N; i++ {
		uuidObj, _ := uuid.NewRandom()

		items = append(items, &gdnotify.ChannelItem{
			ChannelID:          uuidObj.String(),
			DriveID:            randstr.CryptoString(10),
			PageToken:          fmt.Sprintf("%d", r.Intn(100)+1),
			Expiration:         time.Unix(1650000000+int64(r.Intn(5000000)), 0).In(time.Local),
			ResourceID:         randstr.CryptoString(12),
			PageTokenFetchedAt: time.Unix(1650000000+int64(r.Intn(5000000)), 0).In(time.Local),
			CreatedAt:          time.Unix(1650000000+int64(r.Intn(5000000)), 0).In(time.Local),
			UpdatedAt:          time.Unix(1650000000+int64(r.Intn(5000000)), 0).In(time.Local),
		})
	}
	expectedKeys := []string{
		"ChannelID",
		"DriveID",
		"PageToken",
		"Expiration",
		"ResourceID",
		"PageTokenFetchedAt",
		"CreatedAt",
		"UpdatedAt",
	}

	for i, item := range items {
		t.Run(fmt.Sprintf("item[%d]", i), func(t *testing.T) {
			t.Logf("%#v", item)
			values := item.ToDynamoDBAttributeValues()
			require.ElementsMatch(t, expectedKeys, lo.Keys(values))
			require.EqualValues(t, item, gdnotify.NewChannelItemWithDynamoDBAttributeValues(values))
		})
	}
}

func TestChannelItemIsAboutToExpired(t *testing.T) {

	cases := []struct {
		now       time.Time
		item      *gdnotify.ChannelItem
		remaining time.Duration
		expected  bool
	}{
		{
			now: time.Date(2022, 6, 1, 11, 0, 0, 0, time.UTC),
			item: &gdnotify.ChannelItem{
				Expiration: time.Date(2022, 6, 1, 12, 0, 0, 0, time.UTC),
			},
			remaining: time.Hour,
			expected:  true,
		},
		{
			now: time.Date(2022, 6, 1, 11, 0, 0, 0, time.UTC),
			item: &gdnotify.ChannelItem{
				Expiration: time.Date(2022, 6, 1, 13, 0, 0, 0, time.UTC),
			},
			remaining: time.Hour,
			expected:  false,
		},
		{
			now: time.Date(2022, 6, 1, 12, 0, 0, 0, time.UTC),
			item: &gdnotify.ChannelItem{
				Expiration: time.Date(2022, 6, 1, 12, 0, 0, 0, time.UTC),
			},
			remaining: time.Hour,
			expected:  true,
		},
		{
			now: time.Date(2022, 6, 1, 14, 0, 0, 0, time.UTC),
			item: &gdnotify.ChannelItem{
				Expiration: time.Date(2022, 6, 1, 12, 0, 0, 0, time.UTC),
			},
			remaining: time.Hour,
			expected:  true,
		},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s,%s", c.item.Expiration.Format(time.RFC3339), c.remaining), func(t *testing.T) {
			restore := flextime.Set(c.now)
			defer restore()
			actual := c.item.IsAboutToExpired(context.Background(), c.remaining)
			require.EqualValues(t, c.expected, actual)
		})
	}
}

func setupDynamoDB(t *testing.T) *gdnotify.DynamoDBStorage {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "dummy0000dummy")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "dummy0000dummy")
	t.Setenv("AWS_REGION", "us-west-2")

	tableName := fmt.Sprintf("gdnotify_test_%s", randstr.CryptoString(8))

	cfg := gdnotify.StorageOption{
		Type:             "dynamodb",
		TableName:        tableName,
		DynamoDBEndpoint: "http://localhost:8000",
		AutoCreate:       true,
	}

	ctx := context.Background()
	storage, err := gdnotify.NewDynamoDBStorage(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create DynamoDB storage: %v", err)
	}

	return storage
}

func TestDynamoDBStorage_SaveChannel(t *testing.T) {
	storage := setupDynamoDB(t)
	ctx := context.Background()

	item := &gdnotify.ChannelItem{
		ChannelID:  "test-channel",
		Expiration: time.Now().Add(1 * time.Hour),
		PageToken:  "test-token",
		ResourceID: "test-resource",
		DriveID:    "test-drive",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := storage.SaveChannel(ctx, item)
	require.NoError(t, err)

	savedItem, err := storage.FindOneByChannelID(ctx, "test-channel")
	require.NoError(t, err)
	require.Equal(t, item.ChannelID, savedItem.ChannelID)
	require.Equal(t, item.PageToken, savedItem.PageToken)
}

func TestDynamoDBStorage_UpdatePageToken(t *testing.T) {
	storage := setupDynamoDB(t)
	ctx := context.Background()

	item := &gdnotify.ChannelItem{
		ChannelID:  "test-channel",
		Expiration: time.Now().Add(1 * time.Hour),
		PageToken:  "test-token",
		ResourceID: "test-resource",
		DriveID:    "test-drive",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := storage.SaveChannel(ctx, item)
	require.NoError(t, err)

	item.PageToken = "updated-token"
	item.UpdatedAt = time.Now()

	err = storage.UpdatePageToken(ctx, item)
	require.NoError(t, err)

	updatedItem, err := storage.FindOneByChannelID(ctx, "test-channel")
	require.NoError(t, err)
	require.Equal(t, "updated-token", updatedItem.PageToken)
}

func TestDynamoDBStorage_DeleteChannel(t *testing.T) {
	storage := setupDynamoDB(t)
	ctx := context.Background()

	item := &gdnotify.ChannelItem{
		ChannelID:  "test-channel",
		Expiration: time.Now().Add(1 * time.Hour),
		PageToken:  "test-token",
		ResourceID: "test-resource",
		DriveID:    "test-drive",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := storage.SaveChannel(ctx, item)
	require.NoError(t, err)

	err = storage.DeleteChannel(ctx, item)
	require.NoError(t, err)

	_, err = storage.FindOneByChannelID(ctx, "test-channel")
	require.Error(t, err)
	require.IsType(t, &gdnotify.ChannelNotFound{}, err)
}
