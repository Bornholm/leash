# Go library

LeaSH can be embedded directly in Go applications via the `pkg/leash` package.

```bash
go get github.com/bornholm/leash
```

```go
import (
    "github.com/bornholm/leash/pkg/leash"
    "github.com/bornholm/leash/pkg/builtin"
)

eng, cleanup, err := leash.New(ctx,
    leash.WithMaxDuration(30 * time.Second),
    leash.WithAllowedBinaries("grep", "awk", "sort", "head"),
    leash.WithEnvVar("HOME", "/tmp/sandbox"),
    leash.WithAuditWriter(os.Stderr),
)
if err != nil {
    log.Fatal(err)
}
defer cleanup()
```

## Loading a policy file

```go
eng, cleanup, err := leash.New(ctx,
    leash.WithPolicyFile("policies/restrictive.yaml"),
    leash.WithEnvVar("MY_VAR", "value"),
)
```

Options applied after `WithPolicyFile` override individual fields from the file.

## Executing scripts

```go
result, err := eng.Exec(ctx, `echo hello | tr a-z A-Z`)
fmt.Println(result.Stdout)
fmt.Println(result.ExitCode)
fmt.Println(result.Duration)

err = eng.ExecWithStreams(ctx, script, os.Stdin, os.Stdout, os.Stderr)
```

## Registering builtins

```go
wordCount := builtin.New("word-count").
    Description("Count words from stdin").
    Handle(func(ctx context.Context, c *builtin.Call) error {
        data, _ := io.ReadAll(c.Stdin)
        fmt.Fprintf(c.Stdout, "%d\n", len(strings.Fields(string(data))))
        return nil
    })

eng, cleanup, err := leash.New(ctx,
    leash.WithBuiltin(wordCount),
)
```

See [Builtins](builtins.md) for the full builtin API.

## Inspecting the audit trail

```go
result, _ := eng.Exec(ctx, `grep foo /var/log/app.log | head -20`)

for _, cmd := range result.Audit.Commands {
    if cmd.Blocked {
        fmt.Printf("BLOCKED %s: %s\n", cmd.Command, cmd.Reason)
    } else {
        fmt.Printf("OK      %s (exit %d, %s)\n", cmd.Command, cmd.ExitCode, cmd.Duration)
    }
}
```

## Option reference

| Option | Description |
|--------|-------------|
| `WithPolicyFile(path)` | Load base config from a YAML policy file |
| `WithMaxDuration(d)` | Maximum script execution time |
| `WithMaxOutputBytes(n)` | Maximum combined stdout+stderr size |
| `WithMaxCommandsPerScript(n)` | Maximum number of commands per script |
| `WithMaxSubshells(n)` | Maximum subshell nesting depth |
| `WithGlobalRateLimit(count, window)` | Global rate limit across all commands |
| `WithBuiltinRateLimit(name, count, window)` | Per-builtin rate limit |
| `WithAllowedBinaries(names...)` | Declare the complete binary allowlist |
| `WithBlockedPatterns(patterns...)` | Declare the complete blocked pattern list |
| `WithStaticEnv(map)` | Replace the entire static environment |
| `WithEnvVar(key, value)` | Add or override a single environment variable |
| `WithEnabledBuiltins(names...)` | Whitelist specific builtins (empty = all allowed) |
| `WithRequireConfirmation(names...)` | Builtins requiring `CONFIRM_<NAME>=yes` |
| `WithBuiltin(b)` | Register a single builtin |
| `WithBuiltins(b...)` | Register multiple builtins |
| `WithSandbox(cfg)` | Activate filesystem isolation with the given `sandbox.Config` |
| `WithPersistentTmp(enabled)` | Share `/tmp` across all commands within a script execution (bwrap only) |
| `WithMCPServer(cfg)` | Connect an external MCP server |
| `WithAuditWriter(w)` | Destination for JSON audit logs |

## Example

A runnable example is available in [`examples/basic/`](../examples/basic/main.go):

```bash
go run ./examples/basic/
```

It demonstrates custom builtins, pipelines, audit trail inspection, and blocked command handling.
