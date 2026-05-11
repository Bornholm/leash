package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bornholm/leash/pkg/builtin"
)

func LoadScript(src []byte) (*builtin.Builtin, error) {
	return loadScript(src, "")
}

func LoadDir(dir string) ([]*builtin.Builtin, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load dir %q: %w", dir, err)
	}

	var builtins []*builtin.Builtin
	var errs []error

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sh") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}

		fallback := strings.TrimSuffix(entry.Name(), ".sh")
		sk, err := loadScript(src, fallback)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}

		builtins = append(builtins, sk)
	}

	return builtins, errors.Join(errs...)
}

func loadScript(src []byte, fallbackName string) (*builtin.Builtin, error) {
	meta, err := parseFrontmatter(src)
	if err != nil {
		return nil, fmt.Errorf("load script: %w", err)
	}

	name := meta.Name
	if name == "" {
		name = fallbackName
	}
	if name == "" {
		return nil, fmt.Errorf("load script: builtin name is required in : <<'BUILTIN' frontmatter")
	}

	interpreter := parseShebang(src)

	b := builtin.New(name)
	if meta.Description != "" {
		b = b.Description(meta.Description)
	}
	if meta.Usage != "" {
		b = b.Usage(meta.Usage)
	}
	if meta.Category != "" {
		b = b.Category(meta.Category)
	}
	if meta.RateLimit > 0 {
		b = b.RateLimit(meta.RateLimit)
	}
	for _, a := range meta.Args {
		b = b.Arg(a.Name, a.Description, a.Required)
		if a.Pattern != "" {
			if _, err := regexp.Compile(a.Pattern); err != nil {
				return nil, fmt.Errorf("load script %q: arg %q: invalid pattern: %w", name, a.Name, err)
			}
			b = b.ArgPattern(a.Pattern)
		}
	}
	for _, f := range meta.Flags {
		b = b.Flag(f.Name, f.Short, f.Default, f.Description)
		if f.Pattern != "" {
			if _, err := regexp.Compile(f.Pattern); err != nil {
				return nil, fmt.Errorf("load script %q: flag %q: invalid pattern: %w", name, f.Name, err)
			}
			b = b.FlagPattern(f.Pattern)
		}
	}
	for _, e := range meta.Examples {
		b = b.Example(e.Title, e.Command)
	}

	return b.Handle(MakeHandler(name, interpreter, src)), nil
}
