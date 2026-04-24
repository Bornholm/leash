package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bornholm/leash/internal/security/sandbox"
	"github.com/bornholm/leash/pkg/skill"
)

// MakeHandler construit un skill.HandlerFunc qui exécute le script shell src
// via l'interpréteur donné (ex : "sh", "bash").
//
// Chaque invocation lance un nouveau sous-processus :
//   - Les arguments positionnels sont passés comme $1, $2, …
//   - Les flags sont exposés comme LEASH_FLAG_<NOM_EN_MAJUSCULES>=valeur
//   - stdin/stdout/stderr sont câblés directement sur call.Stdin/Stdout/Stderr
func MakeHandler(skillName string, interpreter string, src []byte) skill.HandlerFunc {
	return func(ctx context.Context, c *skill.Call) error {
		// sh -c BODY skillName arg0 arg1 ...
		// → $0 = skillName, $1 = arg0, $2 = arg1, …
		cmdArgs := append([]string{"-c", string(src), skillName}, c.Args...)
		cmd := exec.CommandContext(ctx, interpreter, cmdArgs...)

		// Environnement : SafeEnv de la policy (static + passthrough) + LEASH_FLAG_*.
		// SafeEnv fournit PATH, HOME et toute variable explicitement autorisée (ex: SSH_AUTH_SOCK).
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
				return skill.ExitError{Code: exitErr.ExitCode()}
			}
			return fmt.Errorf("shell skill %q: %w", skillName, err)
		}
		return nil
	}
}
