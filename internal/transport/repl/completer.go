package repl

import (
	"sort"
	"strings"

	"github.com/bornholm/leash/internal/registry"
)

// Completer fournit l'auto-complétion des noms de skills et méta-commandes.
type Completer struct {
	reg *registry.Registry
}

// NewCompleter crée un Completer.
func NewCompleter(reg *registry.Registry) *Completer {
	return &Completer{reg: reg}
}

// Complete retourne les complétions pour le préfixe donné.
func (c *Completer) Complete(prefix string) []string {
	var matches []string

	if strings.HasPrefix(prefix, ":") {
		// Complétion des méta-commandes
		metaCmds := []string{
			":quit", ":help", ":commands", ":history", ":audit",
			":mode", ":policy",
		}
		for _, cmd := range metaCmds {
			if strings.HasPrefix(cmd, prefix) {
				matches = append(matches, cmd)
			}
		}
	} else {
		// Complétion des noms de skills
		for _, name := range c.reg.ListNames() {
			if strings.HasPrefix(name, prefix) {
				matches = append(matches, name)
			}
		}
	}

	sort.Strings(matches)
	return matches
}
