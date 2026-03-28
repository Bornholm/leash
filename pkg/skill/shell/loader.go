// Package shell permet de définir des skills Leash via des scripts shell.
//
// Chaque script .sh doit contenir un bloc de métadonnées YAML dans un heredoc no-op :
//
//	#!/bin/bash
//	: <<'SKILL'
//	name: upper
//	description: Converts stdin lines to uppercase
//	category: text
//	args:
//	  - name: pattern
//	    description: Pattern to search
//	    required: true
//	flags:
//	  - name: prefix
//	    short: p
//	    default: ""
//	    description: Prefix to add before each line
//	examples:
//	  - title: Simple conversion
//	    command: echo "hello" | upper
//	SKILL
//
//	# Corps du handler shell.
//	# Variables disponibles :
//	# $1, $2, ...                — arguments positionnels
//	# LEASH_FLAG_<NOM>           — valeur de chaque flag (nom en majuscules, - remplacé par _)
//	# stdin/stdout/stderr        — accès standard
//
//	while IFS= read -r line; do
//	    printf '%s%s\n' "$LEASH_FLAG_PREFIX" "${line^^}"
//	done
//
// L'interpréteur est déduit du shebang (ex. #!/bin/bash → bash). Fallback : sh.
package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bornholm/leash/pkg/skill"
)

// LoadScript compile un script shell et retourne un *skill.Skill prêt à l'enregistrement.
// src est le contenu complet du fichier (frontmatter heredoc + corps du script).
// Le nom du skill doit être déclaré dans le frontmatter.
func LoadScript(src []byte) (*skill.Skill, error) {
	return loadScript(src, "")
}

// LoadDir charge tous les fichiers *.sh d'un répertoire (non-récursif) et retourne
// la liste des skills. Si un fichier ne déclare pas de nom dans sa frontmatter,
// le nom du fichier (sans extension) est utilisé comme fallback.
// Les erreurs de chaque fichier sont agrégées.
func LoadDir(dir string) ([]*skill.Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load dir %q: %w", dir, err)
	}

	var skills []*skill.Skill
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

		skills = append(skills, sk)
	}

	return skills, errors.Join(errs...)
}

// loadScript est l'implémentation commune. fallbackName est utilisé comme nom du skill
// si le frontmatter n'en déclare pas ; une chaîne vide force l'obligation du frontmatter.
func loadScript(src []byte, fallbackName string) (*skill.Skill, error) {
	meta, err := parseFrontmatter(src)
	if err != nil {
		return nil, fmt.Errorf("load script: %w", err)
	}

	name := meta.Name
	if name == "" {
		name = fallbackName
	}
	if name == "" {
		return nil, fmt.Errorf("load script: skill name is required in : <<'SKILL' frontmatter")
	}

	interpreter := parseShebang(src)

	b := skill.New(name)
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
