package builtin

import (
	"context"
	"fmt"
	"io"
)

type HandlerFunc func(ctx context.Context, call *Call) error

type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

type Builtin struct {
	Name        string
	Description string
	Usage       string
	Category    string
	Args        []ArgDef
	Flags       []FlagDef
	Examples    []Example
	Handler     HandlerFunc
	RateLimit   int
}

type Call struct {
	Args  []string
	Flags map[string]string
	Stdin io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Env func(string) string
	SafeEnv map[string]string
}

type ArgDef struct {
	Name        string
	Description string
	Required    bool
	Pattern     string
}

type FlagDef struct {
	Name        string
	Short       string
	Default     string
	Description string
	Pattern     string
}

type Example struct {
	Title   string
	Command string
}
