package gdnotify

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"github.com/goccy/go-yaml"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"github.com/mashiike/gdnotify/pkg/gdnotifyevent"
)

//go:embed cel_validation_patterns.json
var celValidationPatternsJSON []byte

// CELEnv provides a CEL environment configured for evaluating expressions
// against gdnotifyevent.Detail.
type CELEnv struct {
	env                *cel.Env
	validationPatterns []*gdnotifyevent.Detail
}

// NewCELEnv creates a new CEL environment with gdnotifyevent types registered.
// Field names in CEL expressions use lowerCamelCase (matching JSON tags),
// e.g., change.fileId, change.file.mimeType, entity.createdTime.
func NewCELEnv() (*CELEnv, error) {
	env, err := cel.NewEnv(
		ext.NativeTypes(
			ext.ParseStructTags(true),
			reflect.TypeOf(&gdnotifyevent.Detail{}),
			reflect.TypeOf(&gdnotifyevent.Entity{}),
			reflect.TypeOf(&gdnotifyevent.User{}),
			reflect.TypeOf(&gdnotifyevent.Change{}),
			reflect.TypeOf(&gdnotifyevent.File{}),
			reflect.TypeOf(&gdnotifyevent.Drive{}),
		),
		cel.Variable("detail", cel.ObjectType("gdnotifyevent.Detail")),
		cel.Variable("subject", cel.StringType),
		cel.Variable("entity", cel.ObjectType("gdnotifyevent.Entity")),
		cel.Variable("actor", cel.ObjectType("gdnotifyevent.User")),
		cel.Variable("change", cel.ObjectType("gdnotifyevent.Change")),
		ext.Strings(),
		cel.Function("env",
			cel.Overload("env_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(func(arg ref.Val) ref.Val {
					name, ok := arg.Value().(string)
					if !ok {
						return types.NewErr("env() requires a string argument")
					}
					return types.String(os.Getenv(name))
				}),
			),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}
	var patterns []*gdnotifyevent.Detail
	if err := json.Unmarshal(celValidationPatternsJSON, &patterns); err != nil {
		return nil, fmt.Errorf("failed to parse CEL validation patterns: %w", err)
	}
	return &CELEnv{env: env, validationPatterns: patterns}, nil
}

// CompiledExpression represents a compiled CEL expression.
type CompiledExpression struct {
	program cel.Program
}

// Compile compiles a CEL expression string.
func (e *CELEnv) Compile(expr string) (*CompiledExpression, error) {
	ast, issues := e.env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile CEL expression: %w", issues.Err())
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("CEL expression must return bool, got %s", ast.OutputType())
	}
	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL program: %w", err)
	}
	return &CompiledExpression{program: prg}, nil
}

// Eval evaluates the compiled expression against the given detail.
func (c *CompiledExpression) Eval(detail *gdnotifyevent.Detail) (bool, error) {
	if detail == nil {
		return false, nil
	}
	vars := map[string]any{
		"detail":  detail,
		"subject": detail.Subject,
		"entity":  detail.Entity,
		"actor":   detail.Actor,
		"change":  detail.Change,
	}
	result, _, err := c.program.Eval(vars)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate CEL expression: %w", err)
	}
	b, ok := result.Value().(bool)
	if !ok {
		return false, fmt.Errorf("CEL expression returned non-bool value: %T", result.Value())
	}
	return b, nil
}

// CompileString compiles a CEL expression that returns a string.
func (e *CELEnv) CompileString(expr string) (*StringExpression, error) {
	ast, issues := e.env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile CEL expression: %w", issues.Err())
	}
	if ast.OutputType() != cel.StringType {
		return nil, fmt.Errorf("CEL expression must return string, got %s", ast.OutputType())
	}
	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL program: %w", err)
	}
	return &StringExpression{program: prg}, nil
}

// StringExpression represents a compiled CEL expression that returns a string.
type StringExpression struct {
	program cel.Program
}

// Eval evaluates the string expression against the given detail.
func (s *StringExpression) Eval(detail *gdnotifyevent.Detail) (string, error) {
	if detail == nil {
		return "", nil
	}
	vars := map[string]any{
		"detail":  detail,
		"subject": detail.Subject,
		"entity":  detail.Entity,
		"actor":   detail.Actor,
		"change":  detail.Change,
	}
	result, _, err := s.program.Eval(vars)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate CEL expression: %w", err)
	}
	str, ok := result.Value().(string)
	if !ok {
		return "", fmt.Errorf("CEL expression returned non-string value: %T", result.Value())
	}
	return str, nil
}

// ExprOrString holds either a CEL string expression or a static string value.
type ExprOrString struct {
	raw    string
	value  string
	isExpr bool
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (e *ExprOrString) UnmarshalYAML(data []byte) error {
	return yaml.Unmarshal(data, &e.raw)
}

// Bind compiles the expression if valid, otherwise treats it as a static value.
// When it's an expression, validates it against all validation patterns to ensure it evaluates correctly.
func (e *ExprOrString) Bind(env *CELEnv) error {
	expr, err := env.CompileString(e.raw)
	if err != nil {
		// Not a valid expression, treat as static value
		e.value = e.raw
		return nil
	}
	// Validate against all patterns
	for i, pattern := range env.validationPatterns {
		if _, err := expr.Eval(pattern); err != nil {
			return fmt.Errorf("CEL expression validation failed on pattern[%d]: %w", i, err)
		}
	}
	e.isExpr = true
	return nil
}

// Eval evaluates the expression or returns the static value.
func (e *ExprOrString) Eval(env *CELEnv, detail *gdnotifyevent.Detail) (string, error) {
	if !e.isExpr {
		return e.value, nil
	}
	expr, err := env.CompileString(e.raw)
	if err != nil {
		return "", err
	}
	return expr.Eval(detail)
}

// IsExpr returns true if this holds an expression.
func (e *ExprOrString) IsExpr() bool {
	return e.isExpr
}

// Raw returns the raw string value.
func (e *ExprOrString) Raw() string {
	return e.raw
}

// ExprOrBool holds either a CEL bool expression or a static bool value.
type ExprOrBool struct {
	raw    string
	value  bool
	isExpr bool
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (e *ExprOrBool) UnmarshalYAML(data []byte) error {
	return yaml.Unmarshal(data, &e.raw)
}

// Bind compiles the expression if valid, otherwise parses as a static bool.
// When it's an expression, validates it against all validation patterns to ensure it evaluates correctly.
func (e *ExprOrBool) Bind(env *CELEnv) error {
	expr, err := env.Compile(e.raw)
	if err != nil {
		// Not a valid expression, try to parse as static bool
		switch e.raw {
		case "true":
			e.value = true
		case "false":
			e.value = false
		default:
			return fmt.Errorf("invalid bool value: %s", e.raw)
		}
		return nil
	}
	// Validate against all patterns
	for i, pattern := range env.validationPatterns {
		if _, err := expr.Eval(pattern); err != nil {
			return fmt.Errorf("CEL expression validation failed on pattern[%d]: %w", i, err)
		}
	}
	e.isExpr = true
	return nil
}

// Eval evaluates the expression or returns the static value.
func (e *ExprOrBool) Eval(env *CELEnv, detail *gdnotifyevent.Detail) (bool, error) {
	if !e.isExpr {
		return e.value, nil
	}
	expr, err := env.Compile(e.raw)
	if err != nil {
		return false, err
	}
	return expr.Eval(detail)
}

// IsExpr returns true if this holds an expression.
func (e *ExprOrBool) IsExpr() bool {
	return e.isExpr
}

// Raw returns the raw string value.
func (e *ExprOrBool) Raw() string {
	return e.raw
}
