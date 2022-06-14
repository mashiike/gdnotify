package gdnotify_test

import (
	"testing"

	"github.com/mashiike/gdnotify"
	"github.com/samber/lo"
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
				require.ElementsMatch(t, []string{gdnotify.DefaultDriveID, "0XXXXXXXXXXXXXXXXXX"}, lo.Map(actual.Drives, func(cfg *gdnotify.DriveConfig, _ int) string {
					return cfg.DriveID
				}))
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
				require.ElementsMatch(t, []string{gdnotify.DefaultDriveID, "0XXXXXXXXXXXXXXXXXX"}, lo.Map(actual.Drives, func(cfg *gdnotify.DriveConfig, _ int) string {
					return cfg.DriveID
				}))
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
				require.EqualValues(t, defaultCfg.Drives, actual.Drives)
			},
		},
		{
			casename: "with ssm",
			paths:    []string{"testdata/with_ssm.yaml"},
			check: func(t *testing.T, actual *gdnotify.Config) {
				require.EqualValues(t, gdnotify.CredentialsBackendTypeSSMParameterStore, actual.Credentials.BackendType)
				require.EqualValues(t, "/gdnotify/GOOGLE_APPLICATION_CREDENTIALS", *actual.Credentials.ParameterName)
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
				require.ElementsMatch(t, []string{gdnotify.DefaultDriveID, "0XXXXXXXXXXXXXXXXXX"}, lo.Map(actual.Drives, func(cfg *gdnotify.DriveConfig, _ int) string {
					return cfg.DriveID
				}))
			},
		},
	}

	for _, c := range cases {
		t.Run(c.casename, func(t *testing.T) {
			cfg := gdnotify.DefaultConfig()
			err := cfg.Load(c.paths...)
			require.NoError(t, err)
			if c.check != nil {
				c.check(t, cfg)
			}
		})
	}
}

func TestConfigLoadInvalid(t *testing.T) {
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
	}

	for _, c := range cases {
		t.Run(c.casename, func(t *testing.T) {
			cfg := gdnotify.DefaultConfig()
			err := cfg.Load(c.paths...)
			require.Error(t, err)
			if c.expected != "" {
				require.EqualError(t, err, c.expected)
			}
		})
	}
}
