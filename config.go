package gdnotify

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	gv "github.com/hashicorp/go-version"
	gc "github.com/kayac/go-config"
)

//Config for App
type Config struct {
	RequiredVersion string `yaml:"required_version,omitempty"`

	Webhook      string              `yaml:"webhook,omitempty"`
	Expiration   time.Duration       `yaml:"expiration,omitempty"`
	Storage      *StorageConfig      `yaml:"storage,omitempty"`
	Notification *NotificationConfig `yaml:"notification,omitempty"`
	Drives       []*DriveConfig      `yaml:"drives,omitempty"`

	versionConstraints gv.Constraints `yaml:"version_constraints,omitempty"`
}

type StorageType int

//go:generate enumer -type=StorageType -yaml -trimprefix StorageType -output storage_type_enumer.gen.go
const (
	StorageTypeDynamoDB StorageType = iota
	StorageTypeFile
)

type StorageConfig struct {
	Type      StorageType `yaml:"type,omitempty"`
	TableName *string     `yaml:"table_name,omitempty"`
	DataFile  *string     `yaml:"data_file,omitempty"`
	LockFile  *string     `yaml:"lock_file,omitempty"`
}

type NotificationType int

//go:generate enumer -type=NotificationType -yaml -trimprefix NotificationType -output notification_type_enumer.gen.go
const (
	NotificationTypeEventBridge NotificationType = iota
	NotificationTypeFile
)

type NotificationConfig struct {
	Type      NotificationType `yaml:"type,omitempty"`
	EventBus  *string          `yaml:"event_bus,omitempty"`
	EventFile *string          `yaml:"event_file,omitempty"`
}

const (
	DefaultDriveID = "__default__"
)

type DriveConfig struct {
	DriveID string `yaml:"drive_id,omitempty"`
}

func DefaultConfig() *Config {
	return &Config{
		Expiration: 7 * 24 * time.Hour,
		Storage: &StorageConfig{
			Type:      StorageTypeDynamoDB,
			TableName: aws.String("gdnotify"),
		},
		Notification: &NotificationConfig{
			Type:     NotificationTypeEventBridge,
			EventBus: aws.String("default"),
		},
		Drives: []*DriveConfig{
			{
				DriveID: DefaultDriveID,
			},
		},
	}
}

// Load loads configuration file from file paths.
func (cfg *Config) Load(paths ...string) error {
	if len(paths) == 0 {
		return errors.New("no config")
	}
	if err := gc.LoadWithEnv(cfg, paths...); err != nil {
		return err
	}
	return cfg.Restrict()
}

// Restrict restricts a configuration.
func (cfg *Config) Restrict() error {
	if cfg.RequiredVersion != "" {
		constraints, err := gv.NewConstraint(cfg.RequiredVersion)
		if err != nil {
			return fmt.Errorf("required_version has invalid format: %w", err)
		}
		cfg.versionConstraints = constraints
	}
	if cfg.Expiration == 0 {
		return errors.New("expiration is required")
	}
	if cfg.Storage == nil {
		return errors.New("storage does not configured")
	}
	if cfg.Notification == nil {
		return errors.New("notification does not configured")
	}
	if len(cfg.Drives) == 0 {
		return errors.New("dries does not configured")
	}
	if err := cfg.Storage.Restrict(); err != nil {
		return fmt.Errorf("storage:%w", err)
	}
	if err := cfg.Notification.Restrict(); err != nil {
		return fmt.Errorf("notification:%w", err)
	}
	for i, driveCfg := range cfg.Drives {
		if err := driveCfg.Restrict(); err != nil {
			return fmt.Errorf("drives[%d]:%w", i, err)
		}
	}
	return nil
}

// Restrict restricts a configuration.
func (cfg *StorageConfig) Restrict() error {
	if !cfg.Type.IsAStorageType() {
		return errors.New("invalid storage type")
	}
	switch cfg.Type {
	case StorageTypeDynamoDB:
		return cfg.restrictDynamoDB()
	case StorageTypeFile:
		return cfg.restrictFile()
	default:
		return errors.New("unknown storage type")
	}
}

func (cfg *StorageConfig) restrictDynamoDB() error {
	if cfg.TableName == nil || *cfg.TableName == "" {
		return errors.New("table_name is required, if type is DynamoDB")
	}
	return nil
}

func (cfg *StorageConfig) restrictFile() error {
	if cfg.DataFile == nil || *cfg.DataFile == "" {
		return errors.New("file_path is required, if type is File")
	}
	if cfg.LockFile == nil || *cfg.LockFile == "" {
		cfg.LockFile = aws.String("/tmp/gdnotify_file_storage.lock")
	}
	return nil
}

// Restrict restricts a configuration.
func (cfg *NotificationConfig) Restrict() error {
	if !cfg.Type.IsANotificationType() {
		return errors.New("invalid notification type")
	}
	switch cfg.Type {
	case NotificationTypeEventBridge:
		return cfg.restrictEventBridge()
	case NotificationTypeFile:
		return cfg.restrictFile()
	default:
		return errors.New("unknown notification type")
	}
}

func (cfg *NotificationConfig) restrictEventBridge() error {
	if cfg.EventBus == nil || *cfg.EventBus == "" {
		return errors.New("event_bus is required, if type is EventBridge")
	}
	return nil
}

func (cfg *NotificationConfig) restrictFile() error {
	if cfg.EventFile == nil || *cfg.EventFile == "" {
		return errors.New("event_file is required, if type is File")
	}
	return nil
}

// Restrict restricts a configuration.
func (cfg *DriveConfig) Restrict() error {
	if cfg.DriveID == "" {
		return errors.New("drive_id is required")
	}
	return nil
}

// ValidateVersion validates a version satisfies required_version.
func (c *Config) ValidateVersion(version string) error {
	if c.versionConstraints == nil {
		log.Println("[warn] required_version is empty. Skip checking required_version.")
		return nil
	}
	versionParts := strings.SplitN(version, "-", 2)
	v, err := gv.NewVersion(versionParts[0])
	if err != nil {
		log.Printf("[warn]: Invalid version format \"%s\". Skip checking required_version.", version)
		// invalid version string (e.g. "current") always allowed
		return nil
	}
	if !c.versionConstraints.Check(v) {
		return fmt.Errorf("version %s does not satisfy constraints required_version: %s", version, c.versionConstraints)
	}
	return nil
}
