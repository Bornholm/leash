# Skills

Skills are Go functions registered as virtual shell commands. They are available inside scripts alongside real binaries, support positional arguments and named flags, and can be rate-limited independently.

## Defining a skill

```go
import (
    "github.com/bornholm/leash/pkg/skill"
)

wordCount := skill.New("word-count").
    Description("Count words from stdin").
    Category("text").
    Handle(func(ctx context.Context, c *skill.Call) error {
        data, _ := io.ReadAll(c.Stdin)
        fmt.Fprintf(c.Stdout, "%d\n", len(strings.Fields(string(data))))
        return nil
    })
```

## Arguments and flags

```go
skill.New("fetch").
    Description("Fetch a URL and return its body").
    Arg("url", "Target URL", true).          // positional, required
    Flag("timeout", "t", "10s", "Timeout").  // named flag, default "10s"
    RateLimit(30).                           // 30 calls/minute
    Handle(func(ctx context.Context, c *skill.Call) error {
        url := c.Args[0]
        timeout := c.Flags["timeout"]
        // ...
        return nil
    })
```

Inside a script, skills are called like regular commands:

```bash
echo "hello world" | word-count
fetch https://example.com --timeout=5s | grep title
```

## Returning errors

Use `skill.ExitError` for non-zero exit codes (normal errors). Returning a plain `error` aborts the entire script.

```go
Handle(func(ctx context.Context, c *skill.Call) error {
    if somethingWrong {
        return skill.ExitError{Code: 1}
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

## Registering skills

### Via the Go library

```go
eng, cleanup, err := leash.New(ctx,
    leash.WithSkill(wordCount),
    leash.WithSkills(skillA, skillB),
)
```

### As built-in skills (CLI)

1. Create `skills/<category>/<name>.go` with a constructor returning `*skill.Skill`.
2. Register in `cmd/leash/main.go` inside `registerBuiltinSkills`.

## Restricting skills via policy

```yaml
skills:
  enabled:               # Only these skills are available (empty = all)
    - word-count
    - fetch
  require_confirmation:  # These skills require CONFIRM_<NAME>=yes in env
    - fetch
```
