# AGENTS.md

This file provides guidance to LLM agents when working with code in this repository.

## Commands

```bash
# Build
make build                          # Compile → ./leash binary
go build ./...                      # Check compilation without producing binary

# Test & lint
make test                           # go test ./...
make lint                           # go vet + golangci-lint (if installed)
go test ./internal/engine/...       # Run tests for a specific package
go test -run TestExecBasic ./...    # Run a single test by name

# Run
make run-repl                       # REPL with policies/default.yaml
make run-mcp                        # MCP stdio mode
POLICY=policies/restrictive.yaml make run-repl

# One-liner exec
echo 'echo hello | tr a-z A-Z' | ./leash --policy policies/default.yaml exec
./leash --policy policies/default.yaml exec --exec 'timestamp'
```

## Architecture

### Dependency graph (import order)

```
pkg/builtin  ──────────────────────────────────────────┐
internal/security  (+ audit_types.go)                │
internal/registry  (imports pkg/builtin)            │
internal/engine    (imports security, registry, pkg/builtin)
builtins/*        (import pkg/builtin)
internal/transport/mcp   (imports engine)
internal/transport/repl  (imports engine, registry)
cmd/leash/main.go        (wires everything together)
```

`AuditTrail` and `CommandRecord` live in `internal/security` (not `engine`) to break a potential import cycle.

### Execution pipeline

Every script goes through this sequence in `Runner.ExecWithStreams`:

1. **Text pattern check** — `policy.IsBlockedPattern()` before any parsing
2. **AST parse** — `mvdan.cc/sh/v3/syntax`
3. **AST validation** — `policy.ValidateAST()`: command count, subshell depth, background jobs (`&`)
4. **Timeout** — `context.WithTimeout` wrapping the whole execution
5. **mvdan runner** — wired with `ExecHandlers`, `OpenHandler`, `ReadDirHandler2`
6. **Audit** — `AuditRecorder` collects per-command records, `AuditLogger` persists via `log/slog` (JSON to stderr)

### Command routing (`ExecHandler`)

Each command is resolved in priority order:

1. Registered builtin → `policy.CanExecuteBuiltin` → rate limit check → `builtin.Handler`
2. Allowed binary (allowlist) → delegate to `next` (OS execution) + audit
3. Everything else → blocked, `exit 127`

**Critical**: `call.Stdout`/`call.Stderr` in builtin handlers must be wired to `interp.HandlerCtx(ctx).Stdout`/`.Stderr`, not the Engine-level streams — otherwise shell pipes break.

**Critical**: handlers must return `interp.ExitStatus(n)`, never `fmt.Errorf(...)`, for normal errors — returning a plain error causes mvdan to abort the entire script.

### Adding a new builtin

1. Create `builtins/<category>/<name>.go`
2. Use the builder API:
   ```go
   func NewMyBuiltin() *builtin.Builtin {
       return builtin.New("my_builtin").
           Description("...").
           Arg("input", "...", true).
           Flag("format", "f", "text", "Output format").
           Handle(func(ctx context.Context, c *builtin.Call) error {
               // c.Args[0], c.Flags["format"]
               // c.Stdin, c.Stdout, c.Stderr, c.Env("VAR")
               // return builtin.ExitError{Code: 1} for non-zero exit
               return nil
           })
   }
   ```
3. Register in `cmd/leash/main.go` `registerBuiltinBuiltins`

### Policy files

Three presets in `policies/`: `default.yaml`, `restrictive.yaml`, `permissive.yaml`.

Key YAML fields:

- `execution.max_duration`: go duration string (`30s`, `2m`)
- `rate_limits.global` / `per_builtin`: format `N/minute` or `N/second`
- `allowed_binaries`: system binaries allowed to run; everything else is blocked
- `blocked_patterns`: substring matches checked before parse (and `&` background jobs checked in AST)
- `environment.static`: the _only_ env vars injected — host env is never inherited
- `builtins.enabled`: whitelist (empty list = all registered builtins allowed)
- `builtins.require_confirmation`: builtin names requiring `CONFIRM_<NAME>=yes` env var

### MCP transport (`internal/transport/mcp`)

Exposes three tool categories:

- `execute_shell` — runs an arbitrary script, returns stdout/stderr/exit code/BLOCKED section
- `list_commands` — returns `Registry.GenerateManifest()` (Markdown for LLM system prompt)

### Key library notes (mvdan.cc/sh/v3)

- Use `interp.ReadDirHandler2` (not the deprecated `ReadDirHandler`)
- Use `errors.As(err, &interp.ExitStatus{})` instead of the deprecated `interp.IsExitStatus`
- Timeout detection: check `errors.Is(err, context.DeadlineExceeded)` **before** checking exit status — timeout does not produce an `ExitStatus`
- `interp.HandlerCtx(ctx)` returns `interp.HandlerContext` (struct), not a type to use as parameter type
