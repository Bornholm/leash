package tengo

import (
	"bufio"
	"fmt"
	"io"

	tengosdk "github.com/d5/tengo/v2"
)

// makeWriteFn crée une UserFunction Tengo qui écrit sur w.
// Signature dans les scripts : write(text)
func makeWriteFn(w io.Writer) *tengosdk.UserFunction {
	return &tengosdk.UserFunction{
		Name: "write",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("write: expected 1 argument, got %d", len(args))
			}
			s, _ := tengosdk.ToString(args[0])
			if _, err := fmt.Fprint(w, s); err != nil {
				return nil, fmt.Errorf("write: %w", err)
			}
			return tengosdk.UndefinedValue, nil
		},
	}
}

// makeEwriteFn crée une UserFunction Tengo qui écrit sur w (stderr).
// Signature dans les scripts : ewrite(text)
func makeEwriteFn(w io.Writer) *tengosdk.UserFunction {
	return &tengosdk.UserFunction{
		Name: "ewrite",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("ewrite: expected 1 argument, got %d", len(args))
			}
			s, _ := tengosdk.ToString(args[0])
			if _, err := fmt.Fprint(w, s); err != nil {
				return nil, fmt.Errorf("ewrite: %w", err)
			}
			return tengosdk.UndefinedValue, nil
		},
	}
}

// makeEnvFn crée une UserFunction Tengo qui lit une variable d'environnement.
// Signature dans les scripts : env(key) -> string
func makeEnvFn(envFn func(string) string) *tengosdk.UserFunction {
	return &tengosdk.UserFunction{
		Name: "env",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("env: expected 1 argument, got %d", len(args))
			}
			key, _ := tengosdk.ToString(args[0])
			return &tengosdk.String{Value: envFn(key)}, nil
		},
	}
}

// makeStdinFn crée une UserFunction Tengo qui lit stdin ligne par ligne.
// Le scanner est créé une fois pour toute la durée de vie du handler invoqué.
// Signature dans les scripts : stdin() -> string (chaîne vide = EOF)
func makeStdinFn(r io.Reader) *tengosdk.UserFunction {
	scanner := bufio.NewScanner(r)
	return &tengosdk.UserFunction{
		Name: "stdin",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if scanner.Scan() {
				return &tengosdk.String{Value: scanner.Text()}, nil
			}
			if err := scanner.Err(); err != nil {
				return nil, fmt.Errorf("stdin: %w", err)
			}
			return &tengosdk.String{Value: ""}, nil
		},
	}
}
