package gdnotify

import (
	"strings"
	"testing"

	"github.com/mashiike/gdnotify/pkg/gdnotifyevent"
	"github.com/stretchr/testify/require"
)

func TestParseS3CopyConfig(t *testing.T) {
	env, err := NewCELEnv()
	require.NoError(t, err)

	yamlContent := `
bucket_name: my-bucket
object_key: entity.id + "/" + entity.name

rules:
  - when: change.file.mimeType == "application/pdf"

  - when: change.file.mimeType.startsWith("application/vnd.google-apps")
    export: pdf
    bucket_name: workspace-bucket
    object_key: '"exports/" + change.fileId + ".pdf"'

  - when: change.removed
    skip: true
`

	cfg, err := ParseS3CopyConfig(strings.NewReader(yamlContent), env)
	require.NoError(t, err)

	require.Equal(t, "my-bucket", cfg.BucketName.Raw())
	require.Len(t, cfg.Rules, 3)
	require.Equal(t, "pdf", cfg.Rules[1].Export)
	require.Equal(t, "workspace-bucket", cfg.Rules[1].BucketName.Raw())
	require.True(t, cfg.Rules[2].Skip)
}

func TestS3CopyConfigValidation(t *testing.T) {
	env, err := NewCELEnv()
	require.NoError(t, err)

	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "no rules",
			yaml: `
bucket_name: my-bucket
object_key: "test"
rules: []
`,
			wantErr: "at least one rule is required",
		},
		{
			name: "rule missing when",
			yaml: `
bucket_name: my-bucket
object_key: "test"
rules:
  - skip: true
`,
			wantErr: "rule[0]: when is required",
		},
		{
			name: "missing bucket_name in non-skip rule",
			yaml: `
object_key: "test"
rules:
  - when: "true"
`,
			wantErr: "rule[0]: bucket_name is required",
		},
		{
			name: "missing object_key in non-skip rule",
			yaml: `
bucket_name: my-bucket
rules:
  - when: "true"
`,
			wantErr: "rule[0]: object_key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseS3CopyConfig(strings.NewReader(tt.yaml), env)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestS3CopyConfigValidationSuccess(t *testing.T) {
	env, err := NewCELEnv()
	require.NoError(t, err)

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "top level defaults",
			yaml: `
bucket_name: my-bucket
object_key: "path/to/file"
rules:
  - when: "true"
`,
		},
		{
			name: "rule level overrides",
			yaml: `
rules:
  - when: "true"
    bucket_name: my-bucket
    object_key: "path/to/file"
`,
		},
		{
			name: "skip rule without bucket/key",
			yaml: `
rules:
  - when: change.removed
    skip: true
  - when: "true"
    bucket_name: my-bucket
    object_key: "path/to/file"
`,
		},
		{
			name: "mixed top level and rule level",
			yaml: `
bucket_name: default-bucket
rules:
  - when: change.file.mimeType == "application/pdf"
    object_key: "pdfs/file.pdf"
  - when: "true"
    bucket_name: other-bucket
    object_key: "other/file"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseS3CopyConfig(strings.NewReader(tt.yaml), env)
			require.NoError(t, err)
		})
	}
}

func TestS3CopyConfigMatch(t *testing.T) {
	env, err := NewCELEnv()
	require.NoError(t, err)

	yamlContent := `
bucket_name: default-bucket
object_key: entity.name

rules:
  - when: change.removed
    skip: true

  - when: change.file.mimeType.startsWith("application/vnd.google-apps")
    export: pdf
    bucket_name: workspace-bucket
    object_key: '"exports/" + entity.name + ".pdf"'

  - when: "true"
`

	cfg, err := ParseS3CopyConfig(strings.NewReader(yamlContent), env)
	require.NoError(t, err)

	t.Run("match skip rule", func(t *testing.T) {
		detail := &gdnotifyevent.Detail{
			Entity: &gdnotifyevent.Entity{Name: "test.txt"},
			Change: &gdnotifyevent.Change{
				Removed: true,
			},
		}
		rule, err := cfg.Match(env, detail)
		require.NoError(t, err)
		require.NotNil(t, rule)
		require.True(t, rule.Skip)
	})

	t.Run("match google workspace file", func(t *testing.T) {
		detail := &gdnotifyevent.Detail{
			Entity: &gdnotifyevent.Entity{Name: "My Document"},
			Change: &gdnotifyevent.Change{
				File: &gdnotifyevent.File{
					MimeType: "application/vnd.google-apps.document",
				},
			},
		}
		rule, err := cfg.Match(env, detail)
		require.NoError(t, err)
		require.NotNil(t, rule)
		require.Equal(t, "pdf", rule.Export)
		require.Equal(t, "workspace-bucket", rule.BucketName.Raw())
	})

	t.Run("match default rule", func(t *testing.T) {
		detail := &gdnotifyevent.Detail{
			Entity: &gdnotifyevent.Entity{Name: "report.pdf"},
			Change: &gdnotifyevent.Change{
				File: &gdnotifyevent.File{
					MimeType: "application/pdf",
				},
			},
		}
		rule, err := cfg.Match(env, detail)
		require.NoError(t, err)
		require.NotNil(t, rule)
		require.Equal(t, "", rule.Export)
	})
}

func TestS3CopyConfigGetBucketNameAndObjectKey(t *testing.T) {
	env, err := NewCELEnv()
	require.NoError(t, err)

	yamlContent := `
bucket_name: default-bucket
object_key: '"files/" + entity.id'

rules:
  - when: change.file.mimeType == "application/pdf"
    object_key: '"pdfs/" + entity.name'

  - when: "true"
    bucket_name: '"dynamic-" + entity.kind'
    object_key: entity.name
`

	cfg, err := ParseS3CopyConfig(strings.NewReader(yamlContent), env)
	require.NoError(t, err)

	t.Run("use default bucket with rule object_key", func(t *testing.T) {
		detail := &gdnotifyevent.Detail{
			Entity: &gdnotifyevent.Entity{ID: "123", Name: "report.pdf", Kind: "drive#file"},
			Change: &gdnotifyevent.Change{
				File: &gdnotifyevent.File{MimeType: "application/pdf"},
			},
		}
		rule, err := cfg.Match(env, detail)
		require.NoError(t, err)

		bucket, err := cfg.GetBucketName(env, rule, detail)
		require.NoError(t, err)
		require.Equal(t, "default-bucket", bucket)

		key, err := cfg.GetObjectKey(env, rule, detail)
		require.NoError(t, err)
		require.Equal(t, "pdfs/report.pdf", key)
	})

	t.Run("use dynamic bucket and object_key", func(t *testing.T) {
		detail := &gdnotifyevent.Detail{
			Entity: &gdnotifyevent.Entity{ID: "456", Name: "image.png", Kind: "drive#file"},
			Change: &gdnotifyevent.Change{
				File: &gdnotifyevent.File{MimeType: "image/png"},
			},
		}
		rule, err := cfg.Match(env, detail)
		require.NoError(t, err)

		bucket, err := cfg.GetBucketName(env, rule, detail)
		require.NoError(t, err)
		require.Equal(t, "dynamic-drive#file", bucket)

		key, err := cfg.GetObjectKey(env, rule, detail)
		require.NoError(t, err)
		require.Equal(t, "image.png", key)
	})
}
