package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"mvdan.cc/sh/v3/interp"

	"github.com/bornholm/leash/internal/registry"
	"github.com/bornholm/leash/internal/security"
	"github.com/bornholm/leash/pkg/skill"
)

// NewExecHandler construit le middleware d'exécution pour mvdan.cc/sh/v3.
//
// Ordre de résolution :
//  1. Skill enregistré → vérification policy + rate limit → appel skill.Handler
//  2. Binaire allowlisté → déléguer à next + audit
//  3. Sinon → bloquer, exit 127
func NewExecHandler(
	reg *registry.Registry,
	pol security.PolicyEngine,
	rl *security.RateLimiter,
	recorder *security.AuditRecorder,
) func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return next(ctx, args)
			}
			name := args[0]
			hc := interp.HandlerCtx(ctx)

			// 1. Skill enregistré ?
			if sk, ok := reg.Get(name); ok {
				// --help / -h : afficher l'aide sans exécuter le skill
				if isHelpRequest(args[1:]) {
					printSkillHelp(sk, hc.Stdout)
					return nil
				}

				if err := pol.CanExecuteSkill(ctx, name, args[1:]); err != nil {
					fmt.Fprintf(hc.Stderr, "leash: %s: %v\n", name, err)
					return interp.ExitStatus(126)
				}
				if rl != nil && !rl.Allow(name) {
					fmt.Fprintf(hc.Stderr, "leash: %s: rate limit exceeded\n", name)
					return interp.ExitStatus(126)
				}

				key := recorder.Start(name, args[1:], true)
				call := buildCall(args[1:], sk.Flags, hc, pol.SafeEnvironment())

				if err := skill.Validate(sk, call); err != nil {
					fmt.Fprintf(hc.Stderr, "leash: %s: %v\n", name, err)
					recorder.Finish(key, 1)
					return interp.ExitStatus(1)
				}

				err := sk.Handler(ctx, call)

				exitCode := 0
				if err != nil {
					if exitErr, ok := err.(skill.ExitError); ok {
						exitCode = exitErr.Code
						err = nil
					} else {
						fmt.Fprintf(hc.Stderr, "leash: %s: %v\n", name, err)
						exitCode = 1
					}
				}
				recorder.Finish(key, exitCode)
				if exitCode != 0 {
					return interp.ExitStatus(uint8(exitCode))
				}
				return nil
			}

			// 2. Binaire allowlisté ?
			if pol.IsAllowedBinary(name) {
				key := recorder.Start(name, args[1:], false)
				err := next(ctx, args)
				exitCode := 0
				var exitStatus interp.ExitStatus
				if errors.As(err, &exitStatus) {
					exitCode = int(exitStatus)
					err = nil
				}
				recorder.Finish(key, exitCode)
				if exitCode != 0 {
					return interp.ExitStatus(uint8(exitCode))
				}
				return err
			}

			// 3. Bloqué
			recorder.RecordBlocked(name, args[1:], "command not in allowlist")
			fmt.Fprintf(hc.Stderr, "leash: %s: command not allowed\n", name)
			return interp.ExitStatus(127)
		}
	}
}

// isHelpRequest retourne true si --help ou -h figure dans les arguments.
func isHelpRequest(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}

// printSkillHelp écrit l'aide formatée d'un skill sur w.
func printSkillHelp(sk *skill.Skill, w io.Writer) {
	// Ligne d'usage
	var usage strings.Builder
	usage.WriteString(sk.Name)
	for _, arg := range sk.Args {
		if arg.Required {
			fmt.Fprintf(&usage, " <%s>", arg.Name)
		} else {
			fmt.Fprintf(&usage, " [%s]", arg.Name)
		}
	}
	if len(sk.Flags) > 0 {
		usage.WriteString(" [flags]")
	}
	fmt.Fprintf(w, "Usage: %s\n", usage.String())

	if sk.Description != "" {
		fmt.Fprintf(w, "\n%s\n", sk.Description)
	}

	if len(sk.Args) > 0 {
		fmt.Fprintln(w, "\nArguments:")
		for _, arg := range sk.Args {
			req := ""
			if arg.Required {
				req = " (required)"
			}
			fmt.Fprintf(w, "  %-16s %s%s\n", arg.Name, arg.Description, req)
		}
	}

	if len(sk.Flags) > 0 {
		fmt.Fprintln(w, "\nFlags:")
		for _, flag := range sk.Flags {
			nameCol := "--" + flag.Name
			if flag.Short != "" {
				nameCol += ", -" + flag.Short
			}
			def := ""
			if flag.Default != "" {
				def = fmt.Sprintf(" (default: %q)", flag.Default)
			}
			fmt.Fprintf(w, "  %-20s %s%s\n", nameCol, flag.Description, def)
		}
	}

	if len(sk.Examples) > 0 {
		fmt.Fprintln(w, "\nExamples:")
		for _, ex := range sk.Examples {
			if ex.Title != "" {
				fmt.Fprintf(w, "  # %s\n", ex.Title)
			}
			fmt.Fprintf(w, "  %s\n", ex.Command)
		}
	}
}

// buildCall construit un skill.Call depuis les arguments et le contexte mvdan.
// Les streams sont câblés sur les streams actifs du shell (pour que les pipes fonctionnent).
func buildCall(args []string, flagDefs []skill.FlagDef, hc interp.HandlerContext, safeEnv map[string]string) *skill.Call {
	positional, flags, err := skill.ParseFlags(flagDefs, args)
	if err != nil {
		// En cas d'erreur de parsing, passer les args bruts comme positionnels
		positional = args
		flags = make(map[string]string)
	}
	return &skill.Call{
		Args:    positional,
		Flags:   flags,
		Stdin:   hc.Stdin,
		Stdout:  hc.Stdout,
		Stderr:  hc.Stderr,
		Env:     func(key string) string { return hc.Env.Get(key).String() },
		SafeEnv: safeEnv,
	}
}
