# Security model

LeaSH applies multiple independent enforcement layers so that a compromised or adversarial script cannot escape the sandbox.

## Enforcement layers

### 1. Blocked patterns (pre-parse)

Substring matches are evaluated against the raw script text before any parsing. This is a fast-path rejection for obvious attacks that does not rely on the shell parser.

```yaml
blocked_patterns:
  - "rm -rf"
  - "mkfs"
  - "dd if="
```

### 2. AST validation (post-parse)

After parsing with the shell AST, LeaSH enforces:

- **Command count** — rejects scripts exceeding `max_commands_per_script`
- **Subshell depth** — rejects scripts exceeding `max_subshells`
- **Background jobs** — `&` is rejected at the AST level regardless of blocked patterns

### 3. Binary allowlist (runtime)

During execution, every command lookup checks against `allowed_binaries`. Commands not in the list return exit code 127 — they are never passed to the OS. Skills are resolved separately and are not subject to the binary allowlist.

### 4. Environment isolation

The host environment is **never inherited**. The engine only exposes variables explicitly declared in `environment.static` or via `WithEnvVar`. This prevents leaking credentials, tokens, or system paths into scripts.

### 5. Rate limiting

Global and per-skill rate limits prevent abuse via tight loops or repeated expensive calls.

### 6. Timeout

Every script execution is wrapped in a `context.WithTimeout`. Scripts that exceed `max_duration` are forcibly cancelled.

### 7. Audit trail

Every command execution — including blocked ones — is recorded in the audit trail with the command name, exit code, duration, and block reason. Logs are emitted as structured JSON via `log/slog` to the configured writer.

## Skill confirmation

Sensitive skills can be protected with a confirmation mechanism. When a skill is listed under `require_confirmation`, it will only execute if the environment contains `CONFIRM_<SKILL_NAME>=yes`. This allows callers to explicitly opt in to high-impact operations.

```yaml
skills:
  require_confirmation:
    - deploy
    - delete-records
```

## What LeaSH does NOT provide

- **Filesystem isolation** — scripts can read/write any path accessible to the process. Use a chroot, container, or seccomp policy at the OS level if filesystem isolation is required.
- **Network isolation** — allowed binaries can make network calls. Restrict at the network level if needed.
- **Process isolation** — the shell runner shares the same process as LeaSH. Use containers for full isolation.
