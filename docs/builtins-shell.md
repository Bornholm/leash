# Builtins — POSIX shell scripts

Builtins can be written as POSIX shell scripts (`.sh` files) and loaded from a directory at engine startup. No Go code is required.

## Loading a directory

```go
eng, cleanup, err := leash.New(ctx,
    leash.WithShellBuiltinDir("builtins/"),
)
```

Every `.sh` file found in `builtins/` is loaded as a builtin. The builtin name comes from the frontmatter (see below); if absent, the filename without extension is used.

## File structure

A shell builtin is a plain shell script with an optional YAML frontmatter embedded in a heredoc at the top.

```sh
#!/bin/sh
: <<'BUILTIN'
name: my-builtin
description: Short description shown in help and MCP manifests
category: text
rate_limit: 10       # calls/minute (0 = unlimited)
args:
  - name: input
    description: The input string
    required: true
    pattern: "^.+$"  # optional regexp validation
flags:
  - name: format
    short: f
    default: "plain"
    description: Output format
    pattern: "^(plain|upper)$"
examples:
  - title: Basic usage
    command: my-builtin "hello"
BUILTIN

# Script body — $1, $2, … hold positional args
echo "$1"
```

The `: <<'BUILTIN' … BUILTIN` heredoc is a no-op in sh, so the file is a valid executable script.

## Shebang and interpreter

The interpreter is determined from the shebang line:

| Shebang | Interpreter used |
|---------|-----------------|
| `#!/bin/sh` | `sh` |
| `#!/bin/bash` | `bash` |
| `#!/usr/bin/env zsh` | `zsh` |
| _(none)_ | `sh` (default) |

## Accessing arguments and flags

**Positional arguments** are passed as `$1`, `$2`, … in order.

**Flags** are injected as environment variables: `LEASH_FLAG_<NAME_IN_UPPERCASE>`.

```sh
# Flag --prefix  →  $LEASH_FLAG_PREFIX
# Flag --output-format  →  $LEASH_FLAG_OUTPUT_FORMAT
printf '%s%s\n' "$LEASH_FLAG_PREFIX" "$1"
```

## stdin, stdout, stderr

stdin, stdout, and stderr are wired directly to the shell process — use them as normal:

```sh
while IFS= read -r line; do
    printf '%s\n' "$line" | tr 'a-z' 'A-Z'
done
```

## Returning a non-zero exit code

Use the standard shell `exit` built-in:

```sh
if [ -z "$1" ]; then
    echo "error: empty input" >&2
    exit 1
fi
```

## Frontmatter reference

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Command name used in scripts |
| `description` | string | Shown in help output and MCP manifests |
| `category` | string | Groups the builtin in listings |
| `rate_limit` | int | Max calls per minute (0 = no limit) |
| `args[].name` | string | Positional argument name |
| `args[].description` | string | Argument description |
| `args[].required` | bool | Whether the argument is mandatory |
| `args[].pattern` | string | Regexp the value must match |
| `flags[].name` | string | Long flag name (`--name`) |
| `flags[].short` | string | Short flag name (`-s`) |
| `flags[].default` | string | Default value |
| `flags[].description` | string | Flag description |
| `flags[].pattern` | string | Regexp the value must match |
| `examples[].title` | string | Example title |
| `examples[].command` | string | Example invocation |

## Complete example

```sh
#!/bin/sh
: <<'BUILTIN'
name: wrap
description: Wraps each stdin line between a prefix and a suffix
category: text
flags:
  - name: prefix
    short: p
    default: "["
    description: String to prepend to each line
    pattern: "^.{1,10}$"
  - name: suffix
    short: s
    default: "]"
    description: String to append to each line
    pattern: "^.{1,10}$"
examples:
  - title: Default brackets
    command: echo "hello" | wrap
  - title: Custom delimiters
    command: printf 'a\nb\nc\n' | wrap --prefix="<" --suffix=">"
BUILTIN

while IFS= read -r line; do
    printf '%s%s%s\n' "$LEASH_FLAG_PREFIX" "$line" "$LEASH_FLAG_SUFFIX"
done
```

Usage inside a LeaSH script:

```bash
echo "hello" | wrap                          # → [hello]
printf 'a\nb\n' | wrap --prefix="<" --suffix=">"  # → <a>  <b>
```
