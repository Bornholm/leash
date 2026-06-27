package leash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/bornholm/leash/pkg/builtin"
	"github.com/bornholm/leash/pkg/leash"
)

func newPingBuiltin() *builtin.Builtin {
	return builtin.New("ping").
		Description("test builtin").
		Handle(func(ctx context.Context, c *builtin.Call) error {
			_, _ = c.Stdout.Write([]byte("pong"))
			return nil
		})
}

func TestWithBuiltinsDisabled_BlocksEvenWithWhitelist(t *testing.T) {
	ctx := context.Background()

	eng, cleanup, err := leash.New(ctx,
		leash.WithBuiltins(newPingBuiltin()),
		leash.WithEnabledBuiltins("ping"), // whitelist explicite incluant "ping"
		leash.WithBuiltinsDisabled(),      // doit court-circuiter la whitelist
	)
	if err != nil {
		t.Fatalf("leash.New: %v", err)
	}
	defer cleanup()

	result, err := eng.Exec(ctx, "ping")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code with builtins disabled, got 0 (stdout=%q)", result.Stdout)
	}
	if strings.Contains(string(result.Stdout), "pong") {
		t.Fatalf("builtin executed despite WithBuiltinsDisabled(): stdout=%q", result.Stdout)
	}
}

func TestWithBuiltinsDisabled_AllowsWithoutDisable(t *testing.T) {
	ctx := context.Background()

	eng, cleanup, err := leash.New(ctx,
		leash.WithBuiltins(newPingBuiltin()),
		leash.WithEnabledBuiltins("ping"),
	)
	if err != nil {
		t.Fatalf("leash.New: %v", err)
	}
	defer cleanup()

	result, err := eng.Exec(ctx, "ping")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(string(result.Stdout), "pong") {
		t.Fatalf("expected builtin output, got stdout=%q", result.Stdout)
	}
}
