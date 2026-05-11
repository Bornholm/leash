# Builtins

Builtins are Go functions registered as virtual shell commands. They are available inside scripts alongside real binaries, support positional arguments and named flags, and can be rate-limited independently.

## Defining a builtin

```go
import (
    "github.com/bornholm/leash/pkg/builtin"
)

wordCount := builtin.New("word-count").
    Description("Count words from stdin").
    Category("text").
    Handle(func(ctx context.Context, c *builtin.Call) error {
        data, _ := io.ReadAll(c.Stdin)
        fmt.Fprintf(c.Stdout, "%d\n", len(strings.Fields(string(data))))
        return nil
    })
```

## Arguments and flags

```go
builtin.New("fetch").
    Description("Fetch a URL and return its body").
    Arg("url", "Target URL", true).
    Flag("timeout", "t", "10s", "Timeout").
    RateLimit(30).
    Handle(func(ctx context.Context, c *builtin.Call) error {
        url := c.Args[0]
        timeout := c.Flags["timeout"]
        // ...
        return nil
    })
```

Inside a script, builtins are called like regular commands:

```bash
echo "hello world" | word-count
fetch https://example.com --timeout=5s | grep title
```

## Returning errors

Use `builtin.ExitError` for non-zero exit codes (normal errors). Returning a plain `error` aborts the entire script.

```go
Handle(func(ctx context.Context, c *builtin.Call) error {
    if somethingWrong {
        return builtin.ExitError{Code: 1}
    }
    return nil
})
```

## I/O streams

Always use the streams from the call context, not from the engine level — otherwise shell pipes break.

```go
c.Stdin   // io.Reader
c.Stdout  // io.Writer
c.Stderr  // io.Writer
c.Env("VAR")  // environment variable lookup
```

## Registering builtins

### Via the Go library

```go
eng, cleanup, err := leash.New(ctx,
    leash.WithBuiltin(wordCount),
    leash.WithBuiltins(builtinA, builtinB),
)
```

### As built-in builtins (CLI)

1. Create `builtins/<category>/<name>.go` with a constructor returning `*builtin.Builtin`.
2. Register in `cmd/leash/main.go`.

## Restricting builtins via policy

```yaml
builtins:
  enabled:               # Only these builtins are available (empty = all)
    - word-count
    - fetch
  require_confirmation:  # These builtins require CONFIRM_<NAME>=yes in env
    - fetch
```
