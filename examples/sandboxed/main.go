// Exemple : LeaSH avec isolation filesystem via bubblewrap.
//
// Prérequis : bubblewrap installé (apt install bubblewrap / pacman -S bubblewrap)
//
// Avant d'exécuter :
//
//	mkdir -p /tmp/leash-demo
//	go run ./examples/sandboxed/
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/bornholm/leash/internal/security/sandbox"
	"github.com/bornholm/leash/pkg/leash"
)

func main() {
	ctx := context.Background()

	if err := os.MkdirAll("/tmp/leash-demo", 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	eng, cleanup, err := leash.New(ctx,
		leash.WithAllowedBinaries("ls", "cat", "grep", "wc", "echo"),
		leash.WithEnvVar("HOME", "/work"),
		leash.WithEnvVar("PATH", "/usr/bin:/bin"),
		leash.WithAuditWriter(os.Stderr),
		leash.WithSandbox(sandbox.Config{
			Enabled: true,
			Backend: "bwrap",
			ReadonlyBinds: []string{"/usr"},
			ReadwriteBinds: []sandbox.BindMount{
				{Source: "/tmp/leash-demo", Target: "/work"},
			},
			// Sur les systèmes merged-usr (Arch, Debian 12+, Ubuntu 22+)
			Symlinks: []sandbox.SymlinkSpec{
				{Source: "usr/bin", Target: "/bin"},
				{Source: "usr/lib", Target: "/lib"},
				{Source: "usr/lib", Target: "/lib64"},
			},
			Tmpfs:   []string{"/tmp"},
			Workdir: "/work",
			Unshare: sandbox.Unshare{
				Network: true,
				PID:     true,
				IPC:     true,
			},
			DieWithParent: true,
		}),
	)
	if err != nil {
		log.Fatalf("leash.New: %v", err)
	}
	defer cleanup()

	// Seul /work (→ /tmp/leash-demo) est accessible en écriture.
	result, err := eng.Exec(ctx, "ls /work")
	if err != nil {
		log.Fatalf("exec: %v", err)
	}
	fmt.Printf("ls /work (exit %d):\n%s\n", result.ExitCode, result.Stdout)

	// /etc n'est pas bind-monté — accès impossible.
	result, _ = eng.Exec(ctx, "cat /etc/shadow")
	fmt.Printf("cat /etc/shadow (exit %d) — isolation OK\n", result.ExitCode)
}
