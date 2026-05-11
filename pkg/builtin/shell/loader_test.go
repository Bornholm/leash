package shell_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bornholm/leash/pkg/builtin"
	builtinShell "github.com/bornholm/leash/pkg/builtin/shell"
)

func TestLoadScript_Basic(t *testing.T) {
	src := []byte(`#!/bin/sh
: <<'BUILTIN'
name: upper
description: Converts stdin to uppercase
category: text
BUILTIN

tr 'a-z' 'A-Z'
`)
	sk, err := builtinShell.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if sk.Name != "upper" {
		t.Errorf("Name = %q, want %q", sk.Name, "upper")
	}
	if sk.Description != "Converts stdin to uppercase" {
		t.Errorf("Description = %q", sk.Description)
	}
	if sk.Category != "text" {
		t.Errorf("Category = %q", sk.Category)
	}
}

func TestLoadScript_StdinAndStdout(t *testing.T) {
	src := []byte(`#!/bin/sh
: <<'BUILTIN'
name: echo_lines
description: Echoes stdin lines
BUILTIN

while IFS= read -r line; do
    printf '%s\n' "$line"
done
`)
	sk, err := builtinShell.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	var stdout strings.Builder
	call := &builtin.Call{
		Args:   []string{},
		Flags:  map[string]string{},
		Stdin:  strings.NewReader("hello\nworld\n"),
		Stdout: &stdout,
		Stderr: io.Discard,
		Env:    func(string) string { return "" },
	}

	if err := sk.Handler(context.Background(), call); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	got := stdout.String()
	want := "hello\nworld\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestLoadScript_Args(t *testing.T) {
	src := []byte(`#!/bin/sh
: <<'BUILTIN'
name: greet
description: Greets a person
args:
  - name: name
    description: Name to greet
    required: true
BUILTIN

printf 'Hello, %s!\n' "$1"
`)
	sk, err := builtinShell.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	if len(sk.Args) != 1 || sk.Args[0].Name != "name" {
		t.Errorf("Args = %+v", sk.Args)
	}

	var stdout strings.Builder
	call := &builtin.Call{
		Args:   []string{"Alice"},
		Flags:  map[string]string{},
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Env:    func(string) string { return "" },
	}

	if err := sk.Handler(context.Background(), call); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	got := stdout.String()
	want := "Hello, Alice!\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestLoadScript_Flags(t *testing.T) {
	src := []byte(`#!/bin/sh
: <<'BUILTIN'
name: prefixed
description: Adds a prefix
flags:
  - name: prefix
    short: p
    default: ""
    description: Prefix to add
BUILTIN

while IFS= read -r line; do
    printf '%s%s\n' "$LEASH_FLAG_PREFIX" "$line"
done
`)
	sk, err := builtinShell.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	if len(sk.Flags) != 1 || sk.Flags[0].Name != "prefix" {
		t.Errorf("Flags = %+v", sk.Flags)
	}

	var stdout strings.Builder
	call := &builtin.Call{
		Args:   []string{},
		Flags:  map[string]string{"prefix": ">> "},
		Stdin:  strings.NewReader("hello\n"),
		Stdout: &stdout,
		Stderr: io.Discard,
		Env:    func(string) string { return "" },
	}

	if err := sk.Handler(context.Background(), call); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	got := stdout.String()
	want := ">> hello\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestLoadScript_ExitCode(t *testing.T) {
	src := []byte(`#!/bin/sh
: <<'BUILTIN'
name: fail_builtin
description: Always fails
BUILTIN

exit 42
`)
	sk, err := builtinShell.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	call := &builtin.Call{
		Args:   []string{},
		Flags:  map[string]string{},
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Env:    func(string) string { return "" },
	}

	err = sk.Handler(context.Background(), call)
	if err == nil {
		t.Fatal("expected ExitError, got nil")
	}
	var exitErr builtin.ExitError
	if !strings.Contains(err.Error(), "42") {
		t.Errorf("err = %v, want ExitError with code 42 (got %T)", err, exitErr)
	}
}

func TestLoadScript_Stderr(t *testing.T) {
	src := []byte(`#!/bin/sh
: <<'BUILTIN'
name: stderr_builtin
description: Writes to stderr
BUILTIN

printf 'error message\n' >&2
`)
	sk, err := builtinShell.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	var stderr strings.Builder
	call := &builtin.Call{
		Args:   []string{},
		Flags:  map[string]string{},
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
		Env:    func(string) string { return "" },
	}

	if err := sk.Handler(context.Background(), call); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	if got := stderr.String(); got != "error message\n" {
		t.Errorf("stderr = %q, want %q", got, "error message\n")
	}
}

func TestLoadScript_NoFrontmatterName_Error(t *testing.T) {
	src := []byte(`#!/bin/sh
printf 'hello\n'
`)
	_, err := builtinShell.LoadScript(src)
	if err == nil {
		t.Fatal("expected error for script without frontmatter name, got nil")
	}
}

func TestLoadScript_HeredocVariants(t *testing.T) {
	variants := []struct {
		name string
		src  []byte
	}{
		{"quoted", []byte("#!/bin/sh\n: <<'BUILTIN'\nname: v1\nBUILTIN\nprintf ok\n")},
		{"space-quoted", []byte("#!/bin/sh\n: << 'BUILTIN'\nname: v2\nBUILTIN\nprintf ok\n")},
		{"unquoted", []byte("#!/bin/sh\n: <<BUILTIN\nname: v3\nBUILTIN\nprintf ok\n")},
		{"space-unquoted", []byte("#!/bin/sh\n: << BUILTIN\nname: v4\nBUILTIN\nprintf ok\n")},
	}
	for _, tt := range variants {
		t.Run(tt.name, func(t *testing.T) {
			sk, err := builtinShell.LoadScript(tt.src)
			if err != nil {
				t.Fatalf("LoadScript: %v", err)
			}
			if sk.Name == "" {
				t.Error("Name is empty")
			}
		})
	}
}

func TestLoadScript_BashShebang(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	src := []byte(`#!/bin/bash
: <<'BUILTIN'
name: bash_upper
description: Uppercase via bash
BUILTIN

while IFS= read -r line; do
    printf '%s\n' "${line^^}"
done
`)
	sk, err := builtinShell.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	var stdout strings.Builder
	call := &builtin.Call{
		Args:   []string{},
		Flags:  map[string]string{},
		Stdin:  strings.NewReader("hello\n"),
		Stdout: &stdout,
		Stderr: io.Discard,
		Env:    func(string) string { return "" },
	}

	if err := sk.Handler(context.Background(), call); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	if got := stdout.String(); got != "HELLO\n" {
		t.Errorf("stdout = %q, want %q", got, "HELLO\n")
	}
}

func TestLoadScript_ContextCancel(t *testing.T) {
	src := []byte(`#!/bin/sh
: <<'BUILTIN'
name: sleep_builtin
description: Sleeps forever
BUILTIN

sleep 60
`)
	sk, err := builtinShell.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call := &builtin.Call{
		Args:   []string{},
		Flags:  map[string]string{},
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Env:    func(string) string { return "" },
	}

	err = sk.Handler(ctx, call)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()

	script1 := []byte("#!/bin/sh\n: <<'BUILTIN'\nname: builtin_one\ndescription: First\nBUILTIN\nprintf one\n")
	script2 := []byte("#!/bin/sh\n: <<'BUILTIN'\nname: builtin_two\ndescription: Second\nBUILTIN\nprintf two\n")

	if err := os.WriteFile(filepath.Join(dir, "one.sh"), script1, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "two.sh"), script2, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	builtins, err := builtinShell.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(builtins) != 2 {
		t.Errorf("len(builtins) = %d, want 2", len(builtins))
	}

	names := map[string]bool{}
	for _, sk := range builtins {
		names[sk.Name] = true
	}
	if !names["builtin_one"] || !names["builtin_two"] {
		t.Errorf("unexpected builtin names: %v", names)
	}
}

func TestLoadDir_FallbackName(t *testing.T) {
	dir := t.TempDir()

	src := []byte("#!/bin/sh\nprintf hello\n")
	if err := os.WriteFile(filepath.Join(dir, "my_tool.sh"), src, 0o644); err != nil {
		t.Fatal(err)
	}

	builtins, err := builtinShell.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(builtins) != 1 {
		t.Fatalf("len(builtins) = %d, want 1", len(builtins))
	}
	if builtins[0].Name != "my_tool" {
		t.Errorf("Name = %q, want %q", builtins[0].Name, "my_tool")
	}
}
