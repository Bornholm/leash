# Skills — Tengo scripts

Skills can be written as [Tengo](https://github.com/d5/tengo) scripts (`.tengo` files) and loaded from a directory at engine startup. No Go compilation is required.

## Loading a directory

```go
eng, cleanup, err := leash.New(ctx,
    leash.WithTengoSkillDir("skills/"),
)
```

Every `.tengo` file found in `skills/` is loaded as a skill. The skill name is taken from the frontmatter (see below); if absent, the filename without extension is used.

## File structure

A Tengo skill file is a plain Tengo script with an optional YAML frontmatter block at the top.

```tengo
/* skill
name: my-skill
description: Short description shown in help and MCP manifests
category: text
rate_limit: 10       # calls/minute (0 = unlimited)
args:
  - name: input
    description: The input string
    required: true
    pattern: "^.+$"  # optional regexp validation
flags:
  - name: separator
    short: s
    default: "-"
    description: Word separator
    pattern: "^[-_.]$"
examples:
  - title: Basic usage
    command: my-skill "hello world"
*/

// Script body starts here
result := args[0] + flags["separator"] + "suffix"
write(result + "\n")
```

## Injected variables

| Variable | Type | Description |
|----------|------|-------------|
| `args` | `[]string` | Positional arguments in order |
| `flags` | `map[string]string` | Named flags, keyed by flag name |
| `exit_code` | `int` | Set to a non-zero value to return an error exit code |

## Injected functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `stdin()` | `() → string` | Read one line from stdin; returns `""` at EOF |
| `write(s)` | `(string)` | Write to stdout |
| `ewrite(s)` | `(string)` | Write to stderr |
| `env(key)` | `(string) → string` | Read an environment variable |

## Returning a non-zero exit code

Set `exit_code` before the script ends:

```tengo
if args[0] == "" {
    ewrite("error: empty input\n")
    exit_code = 1
} else {
    write(args[0] + "\n")
}
```

## Reading stdin line by line

`stdin()` returns one line per call (trailing newline stripped). An empty string signals EOF.

```tengo
for {
    line := stdin()
    if line == "" { break }
    write(line + "\n")
}
```

## Frontmatter reference

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Command name used in scripts |
| `description` | string | Shown in help output and MCP manifests |
| `category` | string | Groups the skill in listings |
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

```tengo
/* skill
name: slugify
description: Converts a string to a URL-friendly slug
category: text
args:
  - name: text
    description: Text to slugify
    required: true
flags:
  - name: separator
    short: s
    default: "-"
    description: Word separator
    pattern: "^[-_.]$"
examples:
  - title: Simple slug
    command: slugify "Hello World"
  - title: Underscore separator
    command: slugify --separator=_ "Hello World"
*/

text := import("text")

sep := flags["separator"]
result := text.to_lower(args[0])
result = text.re_replace(`[^a-z0-9]+`, result, sep)
result = text.trim(result, sep)

write(result + "\n")
```

Usage inside a LeaSH script:

```bash
slugify "Hello, World!"             # → hello-world
slugify --separator=_ "Hello World" # → hello_world
```
