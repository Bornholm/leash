// Package tengo permet de définir des skills Leash via des scripts Tengo.
//
// Chaque script .tengo doit commencer par un bloc de métadonnées YAML :
//
//	/* skill
//	name: my_skill
//	description: Description du skill
//	category: text
//	args:
//	  - name: input
//	    description: L'entrée à traiter
//	    required: true
//	flags:
//	  - name: prefix
//	    short: p
//	    default: ""
//	    description: Préfixe à ajouter
//	examples:
//	  - title: Exemple simple
//	    command: echo "hello" | my_skill
//	*/
//
//	// Variables disponibles dans le corps du script :
//	// - args      : array de strings (arguments positionnels)
//	// - flags     : map string->string (flags parsés avec leurs valeurs par défaut)
//	// - stdin()   : fonction retournant la prochaine ligne de stdin (vide = EOF)
//	// - write(s)  : écrire sur stdout
//	// - ewrite(s) : écrire sur stderr
//	// - env(key)  : lire une variable d'environnement du script en cours
//	// - exit_code : affecter un int non-nul pour signaler une erreur (défaut 0)
//
//	text := import("text")
//	result := text.to_upper(args[0])
//	write(result + "\n")
package tengo

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bornholm/leash/pkg/skill"
	"github.com/bornholm/leash/pkg/skill/tengo/modules"
	tengosdk "github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
)

// allowedModules est la liste des modules stdlib Tengo accessibles aux scripts.
var allowedModules = []string{"text", "math", "rand", "fmt", "times", "json", "base64", "hex", "enum", "os"}

// LoadScript compile un script Tengo et retourne un *skill.Skill prêt à l'enregistrement.
// src est le contenu complet du fichier (frontmatter YAML + corps du script).
func LoadScript(src []byte) (*skill.Skill, error) {
	meta, body, err := parseFrontmatter(src)
	if err != nil {
		return nil, fmt.Errorf("load script: %w", err)
	}

	if meta.Name == "" {
		return nil, fmt.Errorf("load script: skill name is required in /* skill */ frontmatter")
	}

	s := tengosdk.NewScript(body)
	moduleMap := stdlib.GetModuleMap(allowedModules...)
	moduleMap.AddBuiltinModule("http", modules.HTTPModule)
	s.SetImports(moduleMap)

	// Pré-déclarer toutes les variables injectées au runtime.
	// Tengo requiert que les variables soient déclarées avant la compilation
	// pour que Compiled.Set() puisse les mettre à jour à l'exécution.
	placeholders := map[string]tengosdk.Object{
		"args":      &tengosdk.Array{Value: []tengosdk.Object{}},
		"flags":     &tengosdk.Map{Value: map[string]tengosdk.Object{}},
		"stdin":     makeStdinFn(strings.NewReader("")),
		"write":     makeWriteFn(io.Discard),
		"ewrite":    makeEwriteFn(io.Discard),
		"env":       makeEnvFn(func(string) string { return "" }),
		"exit_code": &tengosdk.Int{Value: 0},
	}
	for name, val := range placeholders {
		if err := s.Add(name, val); err != nil {
			return nil, fmt.Errorf("load script %q: add placeholder %q: %w", meta.Name, name, err)
		}
	}

	compiled, err := s.Compile()
	if err != nil {
		return nil, fmt.Errorf("load script %q: compile: %w", meta.Name, err)
	}

	b := skill.New(meta.Name)
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
				return nil, fmt.Errorf("load script %q: arg %q: invalid pattern: %w", meta.Name, a.Name, err)
			}
			b = b.ArgPattern(a.Pattern)
		}
	}
	for _, f := range meta.Flags {
		b = b.Flag(f.Name, f.Short, f.Default, f.Description)
		if f.Pattern != "" {
			if _, err := regexp.Compile(f.Pattern); err != nil {
				return nil, fmt.Errorf("load script %q: flag %q: invalid pattern: %w", meta.Name, f.Name, err)
			}
			b = b.FlagPattern(f.Pattern)
		}
	}
	for _, e := range meta.Examples {
		b = b.Example(e.Title, e.Command)
	}

	return b.Handle(MakeHandler(meta.Name, compiled)), nil
}

// LoadDir charge tous les fichiers *.tengo d'un répertoire (non-récursif) et retourne
// la liste des skills compilés. Les erreurs de chaque fichier sont agrégées.
func LoadDir(dir string) ([]*skill.Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load dir %q: %w", dir, err)
	}

	var skills []*skill.Skill
	var errs []error

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tengo") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}

		sk, err := LoadScript(src)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}

		skills = append(skills, sk)
	}

	return skills, errors.Join(errs...)
}
