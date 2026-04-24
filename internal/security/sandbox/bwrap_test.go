package sandbox_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/bornholm/leash/internal/security/sandbox"
)

func requireBwrap(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not available, skipping")
	}
}

func TestNewBwrap_NoBinds(t *testing.T) {
	requireBwrap(t)
	_, err := sandbox.NewBwrap(sandbox.Config{
		Enabled: true,
		Backend: "bwrap",
	})
	if err == nil {
		t.Fatal("attendu une erreur pour config sans bind mounts")
	}
}

func TestNewBwrap_InvalidReadwriteBind(t *testing.T) {
	requireBwrap(t)
	_, err := sandbox.NewBwrap(sandbox.Config{
		Enabled: true,
		Backend: "bwrap",
		ReadwriteBinds: []sandbox.BindMount{
			{Source: "", Target: "/work"},
		},
	})
	if err == nil {
		t.Fatal("attendu une erreur pour bind avec source vide")
	}
}

func TestBwrap_BuildArgs(t *testing.T) {
	requireBwrap(t)
	sb, err := sandbox.NewBwrap(sandbox.Config{
		Enabled:       true,
		Backend:       "bwrap",
		ReadonlyBinds: []string{"/usr"},
		ReadwriteBinds: []sandbox.BindMount{
			{Source: "/tmp/work", Target: "/work"},
		},
		Unshare: sandbox.Unshare{
			Network: true,
		},
		DieWithParent: true,
	})
	if err != nil {
		t.Fatalf("NewBwrap: %v", err)
	}

	cmd := exec.Command("ls", "/work")
	cmd.Env = []string{"HOME=/tmp", "PATH=/usr/bin:/bin"}

	wrapped, err := sb.Wrap(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}

	argsStr := strings.Join(wrapped.Args, " ")
	checks := []string{
		"--ro-bind", "/usr",
		"--bind", "/tmp/work", "/work",
		"--unshare-net",
		"--die-with-parent",
		"--cap-drop", "ALL",
		"--setenv", "HOME",
		"--setenv", "PATH",
	}
	for _, check := range checks {
		if !strings.Contains(argsStr, check) {
			t.Errorf("args bwrap manquant %q dans : %s", check, argsStr)
		}
	}
}

func TestBwrap_Integration_ListFiles(t *testing.T) {
	requireBwrap(t)

	sb, err := sandbox.NewBwrap(sandbox.Config{
		Enabled:       true,
		Backend:       "bwrap",
		ReadonlyBinds: []string{"/usr", "/bin", "/lib", "/lib64"},
		ReadwriteBinds: []sandbox.BindMount{
			{Source: "/tmp", Target: "/tmp-work"},
		},
		Workdir: "/tmp-work",
	})
	if err != nil {
		t.Fatalf("NewBwrap: %v", err)
	}
	defer sb.Close()

	cmd := exec.Command("/bin/ls", "/tmp-work")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}

	wrapped, err := sb.Wrap(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if err := wrapped.Run(); err != nil {
		t.Fatalf("exécution ls dans sandbox: %v", err)
	}
}

func TestBwrap_Integration_IsolationEtc(t *testing.T) {
	requireBwrap(t)

	// /etc n'est pas bind-monté — accès doit échouer
	sb, err := sandbox.NewBwrap(sandbox.Config{
		Enabled:       true,
		Backend:       "bwrap",
		ReadonlyBinds: []string{"/usr", "/bin"},
		ReadwriteBinds: []sandbox.BindMount{
			{Source: "/tmp", Target: "/work"},
		},
		Workdir: "/work",
	})
	if err != nil {
		t.Fatalf("NewBwrap: %v", err)
	}
	defer sb.Close()

	cmd := exec.Command("/bin/cat", "/etc/shadow")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}

	wrapped, err := sb.Wrap(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if err := wrapped.Run(); err == nil {
		t.Fatal("attendu une erreur : /etc/shadow ne doit pas être accessible")
	}
}
