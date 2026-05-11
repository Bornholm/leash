package tengo

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bornholm/leash/pkg/builtin"
	"github.com/bornholm/leash/pkg/builtin/tengo/modules"
	tengosdk "github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
)

var allowedModules = []string{"text", "math", "rand", "fmt", "times", "json", "base64", "hex", "enum", "os"}

func LoadScript(src []byte) (*builtin.Builtin, error) {
	meta, body, err := parseFrontmatter(src)
	if err != nil {
		return nil, fmt.Errorf("load script: %w", err)
	}

	if meta.Name == "" {
		return nil, fmt.Errorf("load script: builtin name is required in /* builtin */ frontmatter")
	}

	s := tengosdk.NewScript(body)
	moduleMap := stdlib.GetModuleMap(allowedModules...)
	moduleMap.AddBuiltinModule("http", modules.HTTPModule)
	s.SetImports(moduleMap)

	placeholders := map[string]tengosdk.Object{
		"args":      &tengosdk.Array{Value: []tengosdk.Object{}},
		"flags":     &tengosdk.Map{Value: map[string]tengosdk.Object{}},
		"stdin":     makeStdinFn(strings.NewReader("")),
		"write":     makeWriteFn(io.Discard),
		"ewrite":    makeEwriteFn(io.Discard),
		"env":       makeEnvFn(func(string) string { return "" }),
		"exit_code": &tengosdk.Int{Value: 0},
		"sandbox":   &tengosdk.Map{Value: map[string]tengosdk.Object{}},
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

	b := builtin.New(meta.Name)
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

func LoadDir(dir string) ([]*builtin.Builtin, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load dir %q: %w", dir, err)
	}

	var builtins []*builtin.Builtin
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

		builtins = append(builtins, sk)
	}

	return builtins, errors.Join(errs...)
}
