package tengo_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bornholm/leash/pkg/builtin"
	builtinTengo "github.com/bornholm/leash/pkg/builtin/tengo"
)

func TestLoadScript_Basic(t *testing.T) {
	src := []byte(`/* builtin
name: upper
description: Converts stdin to uppercase
category: text
*/

text := import("text")
for {
    line := stdin()
    if is_undefined(line) { break }
    write(text.to_upper(line) + "\n")
}
`)
	sk, err := builtinTengo.LoadScript(src)
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

func TestLoadScript_StdinAndWrite(t *testing.T) {
	src := []byte(`/* builtin
name: echo_builtin
description: Echoes stdin lines
*/

for {
    line := stdin()
    if is_undefined(line) { break }
    write(line + "\n")
}
`)
	sk, err := builtinTengo.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	stdin := strings.NewReader("hello\nworld\n")
	var stdout strings.Builder
	var stderr strings.Builder

	call := &builtin.Call{
		Args:   []string{},
		Flags:  map[string]string{},
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
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

func TestLoadScript_StdinEmptyLinesPreserved(t *testing.T) {
	src := []byte(`/* builtin
name: echo_blank
description: Echoes stdin preserving blank lines
*/

for {
    line := stdin()
    if is_undefined(line) { break }
    write(line + "\n")
}
`)
	sk, err := builtinTengo.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	stdinContent := strings.NewReader("first\n\nsecond\n\nthird\n")
	var stdout strings.Builder

	call := &builtin.Call{
		Args:   []string{},
		Flags:  map[string]string{},
		Stdin:  stdinContent,
		Stdout: &stdout,
		Stderr: io.Discard,
		Env:    func(string) string { return "" },
	}

	if err := sk.Handler(context.Background(), call); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	got := stdout.String()
	want := "first\n\nsecond\n\nthird\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestLoadScript_ArgsAndFlags(t *testing.T) {
	src := []byte(`/* builtin
name: greet
description: Greets a person
args:
  - name: name
    description: Name to greet
    required: true
flags:
  - name: prefix
    short: p
    default: "Hello"
    description: Greeting prefix
*/

write(flags["prefix"] + ", " + args[0] + "!\n")
`)
	sk, err := builtinTengo.LoadScript(src)
	if err != nil {
		t.Fatalf("LoadScript: %v", err)
	}

	if len(sk.Args) != 1 || sk.Args[0].Name != "name" {
		t.Errorf("Args = %+v", sk.Args)
	}
	if len(sk.Flags) != 1 || sk.Flags[0].Name != "prefix" {
		t.Errorf("Flags = %+v", sk.Flags)
	}

	var stdout strings.Builder
	call := &builtin.Call{
		Args:   []string{"Alice"},
		Flags:  map[string]string{"prefix": "Bonjour"},
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Env:    func(string) string { return "" },
	}

	if err := sk.Handler(context.Background(), call); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	got := stdout.String()
	want := "Bonjour, Alice!\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestLoadScript_ExitCode(t *testing.T) {
	src := []byte(`/* builtin
name: fail_builtin
description: Always fails
*/

exit_code = 42
`)
	sk, err := builtinTengo.LoadScript(src)
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
	if !strings.Contains(err.Error(), "exit status 42") {
		t.Errorf("err = %v, want ExitError with code 42", err)
	}
}

func TestLoadScript_Ewrite(t *testing.T) {
	src := []byte(`/* builtin
name: stderr_builtin
description: Writes to stderr
*/

ewrite("error message\n")
`)
	sk, err := builtinTengo.LoadScript(src)
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

	got := stderr.String()
	if got != "error message\n" {
		t.Errorf("stderr = %q, want %q", got, "error message\n")
	}
}

func TestLoadScript_NoFrontmatter_Error(t *testing.T) {
	src := []byte(`write("hello\n")`)
	_, err := builtinTengo.LoadScript(src)
	if err == nil {
		t.Fatal("expected error for script without frontmatter name, got nil")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()

	script1 := []byte(`/* builtin
name: builtin_one
description: First builtin
*/
write("one\n")
`)
	script2 := []byte(`/* builtin
name: builtin_two
description: Second builtin
*/
write("two\n")
`)
	if err := os.WriteFile(filepath.Join(dir, "one.tengo"), script1, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "two.tengo"), script2, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	builtins, err := builtinTengo.LoadDir(dir)
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
		t.Errorf("names = %v", names)
	}
}
