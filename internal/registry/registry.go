package registry

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/bornholm/leash/pkg/skill"
)

// Registry est un registre thread-safe de skills.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*skill.Skill
}

// New crée un Registry vide.
func New() *Registry {
	return &Registry{skills: make(map[string]*skill.Skill)}
}

// Register enregistre un skill. Retourne une erreur si le nom est vide ou déjà pris.
func (r *Registry) Register(sk *skill.Skill) error {
	if sk == nil {
		return errors.New("skill cannot be nil")
	}
	if sk.Name == "" {
		return errors.New("skill name cannot be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[sk.Name]; exists {
		return fmt.Errorf("skill %q already registered", sk.Name)
	}
	r.skills[sk.Name] = sk
	return nil
}

// Get retourne un skill par son nom.
func (r *Registry) Get(name string) (*skill.Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sk, ok := r.skills[name]
	return sk, ok
}

// ListNames retourne les noms de tous les skills enregistrés (ordre alphabétique).
func (r *Registry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	return names
}

// ForEach appelle fn pour chaque skill enregistré.
func (r *Registry) ForEach(fn func(*skill.Skill)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, sk := range r.skills {
		fn(sk)
	}
}

// GenerateManifest produit la documentation Markdown à injecter dans le system prompt LLM.
func (r *Registry) GenerateManifest() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.skills) == 0 {
		return "No skills available.\n"
	}

	var sb strings.Builder
	sb.WriteString("# Available Skills\n\n")

	for _, sk := range r.skills {
		sb.WriteString("## `" + sk.Name + "`\n\n")
		if sk.Description != "" {
			sb.WriteString(sk.Description + "\n\n")
		}
		if sk.Usage != "" {
			sb.WriteString("**Usage:** `" + sk.Usage + "`\n\n")
		}
		if len(sk.Args) > 0 {
			sb.WriteString("**Arguments:**\n\n")
			for _, arg := range sk.Args {
				req := ""
				if arg.Required {
					req = " *(required)*"
				}
				sb.WriteString(fmt.Sprintf("- `%s`%s — %s\n", arg.Name, req, arg.Description))
			}
			sb.WriteString("\n")
		}
		if len(sk.Flags) > 0 {
			sb.WriteString("**Flags:**\n\n")
			for _, flag := range sk.Flags {
				short := ""
				if flag.Short != "" {
					short = ", `-" + flag.Short + "`"
				}
				def := ""
				if flag.Default != "" {
					def = " (default: `" + flag.Default + "`)"
				}
				sb.WriteString(fmt.Sprintf("- `--%s`%s%s — %s\n", flag.Name, short, def, flag.Description))
			}
			sb.WriteString("\n")
		}
		if len(sk.Examples) > 0 {
			sb.WriteString("**Examples:**\n\n")
			for _, ex := range sk.Examples {
				sb.WriteString("```sh\n# " + ex.Title + "\n" + ex.Command + "\n```\n\n")
			}
		}
	}

	return sb.String()
}
