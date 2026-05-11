package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bornholm/leash/internal/security/sandbox"
	"github.com/bornholm/leash/pkg/builtin"
)

func MakeHandler(builtinName string, interpreter string, src []byte) builtin.HandlerFunc {
	return func(ctx context.Context, c *builtin.Call) error {
		cmdArgs := append([]string{"-c", string(src), builtinName}, c.Args...)
		cmd := exec.CommandContext(ctx, interpreter, cmdArgs...)

		env := make([]string, 0, len(c.SafeEnv)+len(c.Flags))
		for k, v := range c.SafeEnv {
			env = append(env, k+"="+v)
		}
		if _, hasPATH := c.SafeEnv["PATH"]; !hasPATH {
			if path, ok := os.LookupEnv("PATH"); ok {
				env = append(env, "PATH="+path)
			} else {
				env = append(env, "PATH=/usr/bin:/bin")
			}
		}
		for name, val := range c.Flags {
			envKey := "LEASH_FLAG_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
			env = append(env, envKey+"="+val)
		}
		cmd.Env = env

		cmd.Stdin = c.Stdin
		cmd.Stdout = c.Stdout
		cmd.Stderr = c.Stderr

		sb := sandbox.SandboxFromContext(ctx)
		wrapped, wrapErr := sb.Wrap(ctx, cmd)
		if wrapErr != nil {
			return fmt.Errorf("sandbox wrap: %w", wrapErr)
		}
		if err := wrapped.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return builtin.ExitError{Code: exitErr.ExitCode()}
			}
			return fmt.Errorf("shell builtin %q: %w", builtinName, err)
		}
		return nil
	}
}
