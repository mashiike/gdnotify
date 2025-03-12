package gdnotify_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mashiike/gdnotify"
	"github.com/stretchr/testify/require"
)

func TestConfigLoadNoError(t *testing.T) {
	cases := []struct {
		casename string
		paths    []string
		check    func(t *testing.T, actual *gdnotify.Config)
	}{
		{
			casename: "default",
			paths:    []string{"testdata/default.yaml"},
			check: func(t *testing.T, actual *gdnotify.Config) {
				defaultCfg := gdnotify.DefaultConfig()
				defaultCfg.Restrict()
				require.EqualValues(t, defaultCfg.Expiration, actual.Expiration)
				require.EqualValues(t, defaultCfg.Storage, actual.Storage)
				require.EqualValues(t, defaultCfg.Notification, actual.Notification)
			},
		},
		{
			casename: "drives_only",
			paths:    []string{"testdata/drives_only.yaml"},
			check: func(t *testing.T, actual *gdnotify.Config) {
				defaultCfg := gdnotify.DefaultConfig()
				defaultCfg.Restrict()
				require.EqualValues(t, defaultCfg.Storage, actual.Storage)
				require.EqualValues(t, defaultCfg.Notification, actual.Notification)
			},
		},
		{
			casename: "minimal",
			paths:    []string{"testdata/minimal.yaml"},
			check: func(t *testing.T, actual *gdnotify.Config) {
				defaultCfg := gdnotify.DefaultConfig()
				defaultCfg.Restrict()
				require.EqualValues(t, defaultCfg.Storage, actual.Storage)
				require.EqualValues(t, defaultCfg.Notification, actual.Notification)
			},
		},
		{
			casename: "short",
			paths:    []string{"testdata/short.yaml"},
			check: func(t *testing.T, actual *gdnotify.Config) {
				defaultCfg := gdnotify.DefaultConfig()
				defaultCfg.Restrict()
				require.EqualValues(t, defaultCfg.Storage, actual.Storage)
				require.EqualValues(t, defaultCfg.Notification, actual.Notification)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.casename, func(t *testing.T) {
			cfg := gdnotify.DefaultConfig()
			err := cfg.Load(context.Background(), c.paths...)
			require.NoError(t, err)
			if c.check != nil {
				c.check(t, cfg)
			}
		})
	}
}

func TestConfigLoadInvalid(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	cases := []struct {
		casename string
		paths    []string
		expected string
	}{
		{
			casename: "invalid_storage_type",
			paths:    []string{"testdata/invalid_storage_type.yaml"},
			expected: "testdata/invalid_storage_type.yaml load failed: parse failed: Hoge does not belong to StorageType values",
		},
		{
			casename: "invalid_storage_type",
			paths:    []string{"testdata/invalid_notification_type.yaml"},
			expected: "testdata/invalid_notification_type.yaml load failed: parse failed: Hoge does not belong to NotificationType values",
		},
		{
			casename: "can not load from http",
			paths:    []string{"testdata/short.yaml", server.URL},
			expected: server.URL + " load failed: fetch failed: HTTP 404 Not Found",
		},
	}

	for _, c := range cases {
		t.Run(c.casename, func(t *testing.T) {
			cfg := gdnotify.DefaultConfig()
			err := cfg.Load(context.Background(), c.paths...)
			require.Error(t, err)
			if c.expected != "" {
				require.EqualError(t, err, c.expected)
			}
		})
	}
}
