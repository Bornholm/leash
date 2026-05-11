package registry

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/bornholm/leash/pkg/builtin"
)

type Registry struct {
	mu       sync.RWMutex
	builtins map[string]*builtin.Builtin
}

func New() *Registry {
	return &Registry{builtins: make(map[string]*builtin.Builtin)}
}

func (r *Registry) Register(sk *builtin.Builtin) error {
	if sk == nil {
		return errors.New("builtin cannot be nil")
	}
	if sk.Name == "" {
		return errors.New("builtin name cannot be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.builtins[sk.Name]; exists {
		return fmt.Errorf("builtin %q already registered", sk.Name)
	}
	r.builtins[sk.Name] = sk
	return nil
}

func (r *Registry) Get(name string) (*builtin.Builtin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sk, ok := r.builtins[name]
	return sk, ok
}

func (r *Registry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.builtins))
	for name := range r.builtins {
		names = append(names, name)
	}
	return names
}

func (r *Registry) ForEach(fn func(*builtin.Builtin)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, sk := range r.builtins {
		fn(sk)
	}
}

func (r *Registry) GenerateManifest() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.builtins) == 0 {
		return "No shell commands are available.\n"
	}

	var sb strings.Builder
	sb.WriteString("# Available Shell Commands\n\nThese are shell commands invoked via execute_shell, e.g.: execute_shell {\"script\": \"<command> [args]\"}\n\n")

	for _, sk := range r.builtins {
		sb.WriteString("## " + sk.Name + "\n\n")
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
