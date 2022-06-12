package gdnotify_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Songmu/flextime"
	"github.com/mashiike/gdnotify"
	"github.com/stretchr/testify/require"
)

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
