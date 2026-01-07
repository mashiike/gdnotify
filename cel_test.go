package gdnotify_test

import (
	"testing"

	"github.com/mashiike/gdnotify"
	"github.com/mashiike/gdnotify/pkg/gdnotifyevent"
	"github.com/stretchr/testify/require"
)

func TestCELEnv(t *testing.T) {
	env, err := gdnotify.NewCELEnv()
	require.NoError(t, err)

	cases := []struct {
		name     string
		expr     string
		detail   *gdnotifyevent.Detail
		expected bool
	}{
		{
			name: "simple true",
			expr: "true",
			detail: &gdnotifyevent.Detail{
				Subject: "test",
			},
			expected: true,
		},
		{
			name: "simple false",
			expr: "false",
			detail: &gdnotifyevent.Detail{
				Subject: "test",
			},
			expected: false,
		},
		{
			name: "check subject",
			expr: `subject == "File Changed"`,
			detail: &gdnotifyevent.Detail{
				Subject: "File Changed",
			},
			expected: true,
		},
		{
			name: "check change type",
			expr: `change.changeType == "file"`,
			detail: &gdnotifyevent.Detail{
				Change: &gdnotifyevent.Change{
					ChangeType: "file",
				},
			},
			expected: true,
		},
		{
			name: "check file mime type",
			expr: `change.file.mimeType.startsWith("application/vnd.google-apps.")`,
			detail: &gdnotifyevent.Detail{
				Change: &gdnotifyevent.Change{
					ChangeType: "file",
					File: &gdnotifyevent.File{
						MimeType: "application/vnd.google-apps.spreadsheet",
					},
				},
			},
			expected: true,
		},
		{
			name: "check entity name",
			expr: `entity.name.endsWith(".xlsx")`,
			detail: &gdnotifyevent.Detail{
				Entity: &gdnotifyevent.Entity{
					Name: "report.xlsx",
				},
			},
			expected: true,
		},
		{
			name: "complex condition",
			expr: `change.changeType == "file" && !change.removed && change.file != null`,
			detail: &gdnotifyevent.Detail{
				Change: &gdnotifyevent.Change{
					ChangeType: "file",
					Removed:    false,
					File: &gdnotifyevent.File{
						Name: "test.txt",
					},
				},
			},
			expected: true,
		},
		{
			name: "check file ID empty",
			expr: `change.file.id == ""`,
			detail: &gdnotifyevent.Detail{
				Change: &gdnotifyevent.Change{
					ChangeType: "file",
					File:       nil,
				},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			compiled, err := env.Compile(c.expr)
			require.NoError(t, err, "compile")
			result, err := compiled.Eval(c.detail)
			require.NoError(t, err, "eval")
			require.Equal(t, c.expected, result)
		})
	}
}

func TestCELEnv_CompileError(t *testing.T) {
	env, err := gdnotify.NewCELEnv()
	require.NoError(t, err)

	// Invalid expression
	_, err = env.Compile("invalid syntax !!!")
	require.Error(t, err)

	// Non-bool return type
	_, err = env.Compile(`"string"`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must return bool")
}

func TestExprOrString(t *testing.T) {
	env, err := gdnotify.NewCELEnv()
	require.NoError(t, err)

	cases := []struct {
		name     string
		yaml     string
		detail   *gdnotifyevent.Detail
		expected string
		isExpr   bool
	}{
		{
			name:     "static value",
			yaml:     `"my-bucket"`,
			detail:   &gdnotifyevent.Detail{},
			expected: "my-bucket",
			isExpr:   false,
		},
		{
			name: "expression",
			yaml: `entity.name + ".backup"`,
			detail: &gdnotifyevent.Detail{
				Entity: &gdnotifyevent.Entity{Name: "file"},
			},
			expected: "file.backup",
			isExpr:   true,
		},
		{
			name: "expression with change",
			yaml: `change.fileId`,
			detail: &gdnotifyevent.Detail{
				Change: &gdnotifyevent.Change{FileID: "abc123"},
			},
			expected: "abc123",
			isExpr:   true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var ev gdnotify.ExprOrString
			err := ev.UnmarshalYAML([]byte(c.yaml))
			require.NoError(t, err, "unmarshal")

			err = ev.Bind(env)
			require.NoError(t, err, "bind")
			require.Equal(t, c.isExpr, ev.IsExpr())

			result, err := ev.Eval(env, c.detail)
			require.NoError(t, err, "eval")
			require.Equal(t, c.expected, result)
		})
	}
}

func TestCELEnv_EnvFunction(t *testing.T) {
	t.Setenv("TEST_BUCKET_NAME", "my-test-bucket")

	env, err := gdnotify.NewCELEnv()
	require.NoError(t, err)

	cases := []struct {
		name     string
		yaml     string
		detail   *gdnotifyevent.Detail
		expected string
	}{
		{
			name:     "env function",
			yaml:     `env("TEST_BUCKET_NAME")`,
			detail:   &gdnotifyevent.Detail{},
			expected: "my-test-bucket",
		},
		{
			name:     "env function with concatenation",
			yaml:     `env("TEST_BUCKET_NAME") + "/" + entity.name`,
			detail:   &gdnotifyevent.Detail{Entity: &gdnotifyevent.Entity{Name: "file.txt"}},
			expected: "my-test-bucket/file.txt",
		},
		{
			name:     "env function missing var returns empty",
			yaml:     `env("NONEXISTENT_VAR")`,
			detail:   &gdnotifyevent.Detail{},
			expected: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var ev gdnotify.ExprOrString
			err := ev.UnmarshalYAML([]byte(c.yaml))
			require.NoError(t, err, "unmarshal")

			err = ev.Bind(env)
			require.NoError(t, err, "bind")

			result, err := ev.Eval(env, c.detail)
			require.NoError(t, err, "eval")
			require.Equal(t, c.expected, result)
		})
	}
}

func TestExprOrBool(t *testing.T) {
	env, err := gdnotify.NewCELEnv()
	require.NoError(t, err)

	cases := []struct {
		name     string
		yaml     string
		detail   *gdnotifyevent.Detail
		expected bool
		isExpr   bool
	}{
		{
			name:     "literal true",
			yaml:     `true`,
			detail:   &gdnotifyevent.Detail{},
			expected: true,
			isExpr:   true, // "true" is a valid CEL expression
		},
		{
			name:     "literal false",
			yaml:     `false`,
			detail:   &gdnotifyevent.Detail{},
			expected: false,
			isExpr:   true, // "false" is a valid CEL expression
		},
		{
			name: "expression",
			yaml: `change.changeType == "file"`,
			detail: &gdnotifyevent.Detail{
				Change: &gdnotifyevent.Change{ChangeType: "file"},
			},
			expected: true,
			isExpr:   true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var ev gdnotify.ExprOrBool
			err := ev.UnmarshalYAML([]byte(c.yaml))
			require.NoError(t, err, "unmarshal")

			err = ev.Bind(env)
			require.NoError(t, err, "bind")
			require.Equal(t, c.isExpr, ev.IsExpr())

			result, err := ev.Eval(env, c.detail)
			require.NoError(t, err, "eval")
			require.Equal(t, c.expected, result)
		})
	}
}
