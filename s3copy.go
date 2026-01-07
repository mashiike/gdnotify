package gdnotify

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/mashiike/gdnotify/pkg/gdnotifyevent"
)

// S3CopyConfig is the top-level configuration loaded from --s3-copy-config flag.
// BucketName and ObjectKey serve as defaults when rules don't specify them.
// These can be CEL expressions or static values.
type S3CopyConfig struct {
	BucketName ExprOrString  `yaml:"bucket_name"`
	ObjectKey  ExprOrString  `yaml:"object_key"`
	Rules      []*S3CopyRule `yaml:"rules"`
}

// S3CopyRule defines when and how to copy a file to S3.
// When is a CEL expression that determines if this rule matches.
// If Skip is true, matching files are not copied.
// Export specifies the format for Google Workspace files (e.g., "pdf", "xlsx").
type S3CopyRule struct {
	When       ExprOrBool   `yaml:"when"`
	Skip       bool         `yaml:"skip,omitempty"`
	Export     string       `yaml:"export,omitempty"`
	BucketName ExprOrString `yaml:"bucket_name,omitempty"`
	ObjectKey  ExprOrString `yaml:"object_key,omitempty"`
}

// S3CopyResult is included in the EventBridge notification payload
// when a file is successfully copied to S3.
type S3CopyResult struct {
	S3URI       string    `json:"s3Uri"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	CopiedAt    time.Time `json:"copiedAt"`
}

// LoadS3CopyConfig loads and validates configuration from a YAML file.
func LoadS3CopyConfig(path string, env *CELEnv) (*S3CopyConfig, error) {
	cleanPath := filepath.Clean(path)
	f, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open s3 copy config file: %w", err)
	}
	defer f.Close()
	return ParseS3CopyConfig(f, env)
}

// ParseS3CopyConfig parses and validates configuration from a reader.
func ParseS3CopyConfig(r io.Reader, env *CELEnv) (*S3CopyConfig, error) {
	var cfg S3CopyConfig
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse s3 copy config: %w", err)
	}
	if err := cfg.Bind(env); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Bind validates and binds CEL expressions in the configuration.
// Rules must have a "when" expression, and non-skip rules must have
// bucket_name and object_key (either at top level or in the rule).
func (c *S3CopyConfig) Bind(env *CELEnv) error {
	if len(c.Rules) == 0 {
		return fmt.Errorf("at least one rule is required")
	}
	if err := c.BucketName.Bind(env); err != nil {
		return fmt.Errorf("bucket_name: %w", err)
	}
	if err := c.ObjectKey.Bind(env); err != nil {
		return fmt.Errorf("object_key: %w", err)
	}
	for i, rule := range c.Rules {
		if rule.When.Raw() == "" {
			return fmt.Errorf("rule[%d]: when is required", i)
		}
		if err := rule.When.Bind(env); err != nil {
			return fmt.Errorf("rule[%d].when: %w", i, err)
		}
		if err := rule.BucketName.Bind(env); err != nil {
			return fmt.Errorf("rule[%d].bucket_name: %w", i, err)
		}
		if err := rule.ObjectKey.Bind(env); err != nil {
			return fmt.Errorf("rule[%d].object_key: %w", i, err)
		}
		if !rule.Skip {
			if c.BucketName.Raw() == "" && rule.BucketName.Raw() == "" {
				return fmt.Errorf("rule[%d]: bucket_name is required (either at top level or in rule)", i)
			}
			if c.ObjectKey.Raw() == "" && rule.ObjectKey.Raw() == "" {
				return fmt.Errorf("rule[%d]: object_key is required (either at top level or in rule)", i)
			}
		}
	}
	return nil
}

// Match finds the first matching rule for the given detail and returns it.
// Returns nil if no rule matches.
func (c *S3CopyConfig) Match(env *CELEnv, detail *gdnotifyevent.Detail) (*S3CopyRule, error) {
	for _, rule := range c.Rules {
		matched, err := rule.When.Eval(env, detail)
		if err != nil {
			return nil, err
		}
		if matched {
			return rule, nil
		}
	}
	return nil, nil
}

// GetBucketName returns the effective bucket name for the rule.
// Uses the rule's bucket_name if set, otherwise falls back to config default.
func (c *S3CopyConfig) GetBucketName(env *CELEnv, rule *S3CopyRule, detail *gdnotifyevent.Detail) (string, error) {
	if rule.BucketName.Raw() != "" {
		return rule.BucketName.Eval(env, detail)
	}
	return c.BucketName.Eval(env, detail)
}

// GetObjectKey returns the effective object key for the rule.
// Uses the rule's object_key if set, otherwise falls back to config default.
func (c *S3CopyConfig) GetObjectKey(env *CELEnv, rule *S3CopyRule, detail *gdnotifyevent.Detail) (string, error) {
	if rule.ObjectKey.Raw() != "" {
		return rule.ObjectKey.Eval(env, detail)
	}
	return c.ObjectKey.Eval(env, detail)
}
