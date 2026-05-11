package tengo

import (
	"bufio"
	"fmt"
	"io"

	tengosdk "github.com/d5/tengo/v2"
)

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

func makeStdinFn(r io.Reader) *tengosdk.UserFunction {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	return &tengosdk.UserFunction{
		Name: "stdin",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if scanner.Scan() {
				return &tengosdk.String{Value: scanner.Text()}, nil
			}
			if err := scanner.Err(); err != nil {
				return nil, fmt.Errorf("stdin: %w", err)
			}
			return tengosdk.UndefinedValue, nil
		},
	}
}
