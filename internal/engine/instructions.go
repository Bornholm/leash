package engine

import (
	"strings"

	"github.com/bornholm/leash/internal/registry"
	"github.com/bornholm/leash/internal/security"
)

// availableBuiltins retourne, parmi les builtins enregistrés, ceux que la
// policy autorise effectivement à s'exécuter — pour ne pas afficher comme
// disponible un builtin enregistré mais bloqué par "builtins.disabled" ou
// absent de "builtins.enabled". EnabledBuiltins() seul est ambigu : une
// whitelist vide signifie "tous autorisés" quand les builtins ne sont pas
// désactivés, donc BuiltinsDisabled() doit être vérifié séparément.
func availableBuiltins(reg *registry.Registry, pol security.PolicyEngine) []string {
	if pol.BuiltinsDisabled() {
		return nil
	}

	all := reg.ListNames()

	enabled := pol.EnabledBuiltins()
	if len(enabled) == 0 {
		return all
	}

	enabledSet := make(map[string]bool, len(enabled))
	for _, name := range enabled {
		enabledSet[name] = true
	}

	var allowed []string
	for _, name := range all {
		if enabledSet[name] {
			allowed = append(allowed, name)
		}
	}
	return allowed
}

// Instructions implémente Engine.Instructions. Le texte est généré à partir
// de l'état réel de ce Runner (registre de builtins + policy de binaires
// autorisés), donc différent par Engine — important en multi-tenant où
// chaque workspace a sa propre policy (cf. internal/transport/mcphttp).
func (r *Runner) Instructions() string {
	var sb strings.Builder

	sb.WriteString(`LeaSH is a policy-enforced shell sandbox.

## The ONLY MCP tool is: execute_shell

There is exactly one MCP tool: **execute_shell**. Do NOT attempt to call any other name as an MCP tool.
All operations are performed by passing shell scripts to execute_shell.

## Shell commands vs MCP tools

LeaSH registers domain-specific commands as SHELL commands (not MCP tools).
These commands are invoked exclusively inside shell scripts via execute_shell.

To discover available shell commands:
  execute_shell { "script": "leash-help" }

To get help on a specific command:
  execute_shell { "script": "leash-help <command>" }
  or:
  execute_shell { "script": "<command> --help" }

`)

	commands := availableBuiltins(r.Registry(), r.Policy())
	binaries := r.Policy().AllowedBinaries()

	if len(binaries) == 0 && len(commands) == 0 {
		sb.WriteString("## Available shell commands\nNo commands are currently available.\n")
	} else {
		sb.WriteString("## Available shell commands\n(These are shell commands — invoke them via execute_shell, NOT as MCP tools)\n\n")
		for _, c := range commands {
			sb.WriteString("- " + c + "\n")
		}
		for _, b := range binaries {
			sb.WriteString("- " + b + " (system binary)\n")
		}
		sb.WriteString("\nAny other command will be blocked (exit code 127).\n")
	}

	sb.WriteString(`
## Rules

1. Use execute_shell for EVERYTHING — scripts, commands, pipes, loops, etc.
2. Command blocked (exit 127)? Run execute_shell { "script": "leash-help" } to see available commands.
3. Do NOT call shell command names as MCP tools. They are shell commands only.
`)

	return sb.String()
}
