package modules

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bornholm/leash/internal/security/sandbox"
	tengosdk "github.com/d5/tengo/v2"
)

func MakeSandboxModule(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, safeEnv map[string]string) *tengosdk.Map {
	return &tengosdk.Map{
		Value: map[string]tengosdk.Object{
			"exec":     makeExecFn(ctx, stdin, stdout, stderr, safeEnv),
			"exec_out": makeExecOutFn(ctx, stdin, stdout, stderr, safeEnv),
		},
	}
}

func makeExecFn(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, safeEnv map[string]string) *tengosdk.UserFunction {
	return &tengosdk.UserFunction{
		Name: "exec",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("sandbox.exec: command name required")
			}
			if len(args) < 2 {
				return nil, fmt.Errorf("sandbox.exec: arguments array required")
			}

			cmdName, _ := tengosdk.ToString(args[0])
			if cmdName == "" {
				return nil, fmt.Errorf("sandbox.exec: command name cannot be empty")
			}

			argsSlice, ok := args[1].(*tengosdk.Array)
			if !ok {
				return nil, fmt.Errorf("sandbox.exec: second argument must be array")
			}

			cmdArgs := make([]string, len(argsSlice.Value))
			for i, arg := range argsSlice.Value {
				cmdArgs[i], _ = tengosdk.ToString(arg)
			}

			binaryPath, err := lookupInPath(cmdName)
			if err != nil {
				return &tengosdk.Map{
					Value: map[string]tengosdk.Object{
						"stdout":  &tengosdk.String{Value: ""},
						"stderr":  &tengosdk.String{Value: err.Error()},
						"code":    &tengosdk.Int{Value: 127},
					},
				}, nil
			}

			cmd := exec.CommandContext(ctx, binaryPath, cmdArgs...)
			cmd.Stdin = stdin
			cmd.Env = buildEnv(safeEnv)

			cmdStdout, cmdStderr := &stringWriter{}, &stringWriter{}
			cmd.Stdout = cmdStdout
			cmd.Stderr = cmdStderr

			sb := sandbox.SandboxFromContext(ctx)
			wrapped, wrapErr := sb.Wrap(ctx, cmd)
			if wrapErr != nil {
				return &tengosdk.Map{
					Value: map[string]tengosdk.Object{
						"stdout":  &tengosdk.String{Value: ""},
						"stderr":  &tengosdk.String{Value: fmt.Sprintf("sandbox wrap: %v", wrapErr)},
						"code":    &tengosdk.Int{Value: 1},
					},
				}, nil
			}

			runErr := wrapped.Run()

			var code int
			if runErr != nil {
				var exitErr *exec.ExitError
				if errors.As(runErr, &exitErr) {
					code = exitErr.ExitCode()
				} else {
					code = 1
				}
			}

			return &tengosdk.Map{
				Value: map[string]tengosdk.Object{
					"stdout":  &tengosdk.String{Value: cmdStdout.s},
					"stderr":  &tengosdk.String{Value: cmdStderr.s},
					"code":    &tengosdk.Int{Value: int64(code)},
				},
			}, nil
		},
	}
}

func makeExecOutFn(ctx context.Context, stdin io.Reader, _, stderr io.Writer, safeEnv map[string]string) *tengosdk.UserFunction {
	return &tengosdk.UserFunction{
		Name: "exec_out",
		Value: func(args ...tengosdk.Object) (tengosdk.Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("sandbox.exec_out: command name required")
			}
			if len(args) < 2 {
				return nil, fmt.Errorf("sandbox.exec_out: arguments array required")
			}

			cmdName, _ := tengosdk.ToString(args[0])
			if cmdName == "" {
				return nil, fmt.Errorf("sandbox.exec_out: command name cannot be empty")
			}

			argsSlice, ok := args[1].(*tengosdk.Array)
			if !ok {
				return nil, fmt.Errorf("sandbox.exec_out: second argument must be array")
			}

			cmdArgs := make([]string, len(argsSlice.Value))
			for i, arg := range argsSlice.Value {
				cmdArgs[i], _ = tengosdk.ToString(arg)
			}

			binaryPath, err := lookupInPath(cmdName)
			if err != nil {
				return &tengosdk.String{Value: ""}, nil
			}

			cmd := exec.CommandContext(ctx, binaryPath, cmdArgs...)
			cmd.Stdin = stdin
			cmd.Env = buildEnv(safeEnv)

			cmdStdout, cmdStderr := &stringWriter{}, &stringWriter{}
			cmd.Stdout = cmdStdout
			cmd.Stderr = cmdStderr

			sb := sandbox.SandboxFromContext(ctx)
			wrapped, wrapErr := sb.Wrap(ctx, cmd)
			if wrapErr != nil {
				return &tengosdk.String{Value: ""}, nil
			}

			runErr := wrapped.Run()
			if runErr != nil {
				io.WriteString(cmdStderr, fmt.Sprintf("exit code: %v\n", runErr))
			}

			if cmdStderr.s != "" {
				io.WriteString(stderr, cmdStderr.s)
			}

			return &tengosdk.String{Value: cmdStdout.s}, nil
		},
	}
}

type stringWriter struct {
	s string
}

func (sw *stringWriter) Write(p []byte) (int, error) {
	sw.s += string(p)
	return len(p), nil
}

func lookupInPath(name string) (string, error) {
	if filepath.IsAbs(name) {
		return name, nil
	}

	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/usr/bin:/bin"
	}

	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return full, nil
		}
	}

	return "", fmt.Errorf("%s: not found in PATH", name)
}

func buildEnv(safeEnv map[string]string) []string {
	env := make([]string, 0, len(safeEnv)+1)
	for k, v := range safeEnv {
		env = append(env, k+"="+v)
	}
	if _, hasPATH := safeEnv["PATH"]; !hasPATH {
		if path, ok := os.LookupEnv("PATH"); ok {
			env = append(env, "PATH="+path)
		} else {
			env = append(env, "PATH=/usr/bin:/bin")
		}
	}
	return env
}
