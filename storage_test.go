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
	rand.Seed(time.Now().Unix())
	for i := 0; i < N; i++ {
		uuidObj, _ := uuid.NewRandom()

		items = append(items, &gdnotify.ChannelItem{
			ChannelID:          uuidObj.String(),
			DriveID:            randstr.CryptoString(10),
			PageToken:          fmt.Sprintf("%d", rand.Intn(100)+1),
			Expiration:         time.Unix(1650000000+int64(rand.Intn(5000000)), 0).In(time.Local),
			ResourceID:         randstr.CryptoString(12),
			PageTokenFetchedAt: time.Unix(1650000000+int64(rand.Intn(5000000)), 0).In(time.Local),
			CreatedAt:          time.Unix(1650000000+int64(rand.Intn(5000000)), 0).In(time.Local),
			UpdatedAt:          time.Unix(1650000000+int64(rand.Intn(5000000)), 0).In(time.Local),
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
