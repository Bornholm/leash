package tengo

import (
	"context"
	"fmt"

	"github.com/bornholm/leash/pkg/skill"
	"github.com/bornholm/leash/pkg/skill/tengo/modules"
	tengosdk "github.com/d5/tengo/v2"
)

// MakeHandler construit un skill.HandlerFunc à partir d'un script Tengo pré-compilé.
// Chaque invocation clone le Compiled, injecte les données du Call, exécute et lit exit_code.
func MakeHandler(skillName string, compiled *tengosdk.Compiled) skill.HandlerFunc {
	return func(ctx context.Context, c *skill.Call) error {
		clone := compiled.Clone()

		tengoArgs := make([]tengosdk.Object, len(c.Args))
		for i, a := range c.Args {
			tengoArgs[i] = &tengosdk.String{Value: a}
		}

		tengoFlags := make(map[string]tengosdk.Object, len(c.Flags))
		for k, v := range c.Flags {
			tengoFlags[k] = &tengosdk.String{Value: v}
		}

		injections := map[string]tengosdk.Object{
			"args":      &tengosdk.Array{Value: tengoArgs},
			"flags":     &tengosdk.Map{Value: tengoFlags},
			"stdin":     makeStdinFn(c.Stdin),
			"write":     makeWriteFn(c.Stdout),
			"ewrite":    makeEwriteFn(c.Stderr),
			"env":       makeEnvFn(c.Env),
			"exit_code": &tengosdk.Int{Value: 0},
			"sandbox":   modules.MakeSandboxModule(ctx, c.Stdin, c.Stdout, c.Stderr, c.SafeEnv),
		}

		for name, val := range injections {
			if err := clone.Set(name, val); err != nil {
				return fmt.Errorf("tengo skill %q: inject %s: %w", skillName, name, err)
			}
		}

		if err := clone.RunContext(ctx); err != nil {
			return fmt.Errorf("tengo skill %q: %w", skillName, err)
		}

		exitCodeVar := clone.Get("exit_code")
		if !exitCodeVar.IsUndefined() {
			if code := exitCodeVar.Int(); code != 0 {
				return skill.ExitError{Code: code}
			}
		}

		return nil
	}
}
