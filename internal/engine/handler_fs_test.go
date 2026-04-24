package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/bornholm/leash/internal/security/sandbox"
)

// ctx de base : hc.Dir sera "" (zéro) — suffisant pour tous les tests sur chemins absolus.
var bgCtx = context.Background()

func TestNewFSOpenHandler_NoneBackend(t *testing.T) {
	h := NewFSOpenHandler(sandbox.DefaultConfig())
	tmp := t.TempDir()

	// Création d'un fichier : aucune restriction avec backend none
	f, err := h(bgCtx, filepath.Join(tmp, "x"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("none backend doit tout autoriser : %v", err)
	}
	_ = f.Close()
}

func TestNewFSOpenHandler_ReadonlyBind(t *testing.T) {
	ro := t.TempDir()
	target := filepath.Join(ro, "file.txt")
	_ = os.WriteFile(target, []byte("data"), 0o644)

	cfg := sandbox.Config{
		Enabled:       true,
		Backend:       "bwrap",
		ReadonlyBinds: []string{ro},
	}
	h := NewFSOpenHandler(cfg)

	t.Run("lecture autorisée", func(t *testing.T) {
		f, err := h(bgCtx, target, os.O_RDONLY, 0)
		if err != nil {
			t.Fatalf("lecture dans readonly_bind doit être autorisée : %v", err)
		}
		_ = f.Close()
	})

	t.Run("écriture refusée EROFS", func(t *testing.T) {
		_, err := h(bgCtx, target, os.O_WRONLY|os.O_CREATE, 0o600)
		if err == nil {
			t.Fatal("écriture dans readonly_bind doit être refusée")
		}
		var pathErr *os.PathError
		if !errors.As(err, &pathErr) || !errors.Is(pathErr.Err, syscall.EROFS) {
			t.Errorf("attendu EROFS, obtenu : %v", err)
		}
	})
}

func TestNewFSOpenHandler_ReadwriteBind(t *testing.T) {
	rw := t.TempDir()
	cfg := sandbox.Config{
		Enabled: true,
		Backend: "bwrap",
		ReadwriteBinds: []sandbox.BindMount{
			{Source: rw, Target: "/work"},
		},
	}
	h := NewFSOpenHandler(cfg)

	f, err := h(bgCtx, filepath.Join(rw, "new.txt"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("écriture dans readwrite_bind doit être autorisée : %v", err)
	}
	_ = f.Close()
}

func TestNewFSOpenHandler_Tmpfs(t *testing.T) {
	tmp := t.TempDir()
	cfg := sandbox.Config{
		Enabled: true,
		Backend: "bwrap",
		Tmpfs:   []string{tmp},
	}
	h := NewFSOpenHandler(cfg)

	f, err := h(bgCtx, filepath.Join(tmp, "ephemere.txt"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("écriture dans tmpfs doit être autorisée : %v", err)
	}
	_ = f.Close()
}

func TestNewFSOpenHandler_UndeclaredPath(t *testing.T) {
	cfg := sandbox.Config{
		Enabled:       true,
		Backend:       "bwrap",
		ReadonlyBinds: []string{"/usr"},
	}
	h := NewFSOpenHandler(cfg)

	_, err := h(bgCtx, "/home/secret/key.txt", os.O_RDONLY, 0)
	if err == nil {
		t.Fatal("accès à un chemin non déclaré doit être refusé")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !errors.Is(pathErr.Err, syscall.EACCES) {
		t.Errorf("attendu EACCES, obtenu : %v", err)
	}
}

// TestNewFSOpenHandler_RelativePath vérifie que les chemins relatifs non résolubles
// vers un chemin autorisé sont refusés.
// Quand hc.Dir est "" (contexte sans runner), filepath.Clean("secret.txt") = "secret.txt"
// (non absolu) → ne peut être sous aucun bind absolu → EACCES.
func TestNewFSOpenHandler_RelativePath(t *testing.T) {
	cfg := sandbox.Config{
		Enabled:       true,
		Backend:       "bwrap",
		ReadonlyBinds: []string{"/usr"},
	}
	h := NewFSOpenHandler(cfg)

	_, err := h(bgCtx, "secret.txt", os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		t.Fatal("écriture d'un chemin relatif non autorisé doit être refusée")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !errors.Is(pathErr.Err, syscall.EACCES) {
		t.Errorf("attendu EACCES, obtenu : %v", err)
	}
}

func TestNewFSOpenHandler_BwrapAutoMounts(t *testing.T) {
	cfg := sandbox.Config{
		Enabled:       true,
		Backend:       "bwrap",
		ReadonlyBinds: []string{"/usr"},
	}
	h := NewFSOpenHandler(cfg)

	t.Run("/proc lecture autorisée", func(t *testing.T) {
		f, err := h(bgCtx, "/proc/version", os.O_RDONLY, 0)
		if err != nil {
			t.Fatalf("/proc/version doit être lisible avec bwrap : %v", err)
		}
		_ = f.Close()
	})

	t.Run("/dev/null accessible en écriture", func(t *testing.T) {
		f, err := h(bgCtx, "/dev/null", os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("/dev/null doit être accessible avec bwrap : %v", err)
		}
		_ = f.Close()
	})

	t.Run("/proc écriture refusée", func(t *testing.T) {
		_, err := h(bgCtx, "/proc/sysrq-trigger", os.O_WRONLY, 0o200)
		if err == nil {
			t.Fatal("écriture dans /proc doit être refusée")
		}
		var pathErr *os.PathError
		if !errors.As(err, &pathErr) || !errors.Is(pathErr.Err, syscall.EROFS) {
			t.Errorf("attendu EROFS, obtenu : %v", err)
		}
	})
}

func TestIsUnder(t *testing.T) {
	cases := []struct {
		path, dir string
		want      bool
	}{
		{"/usr/bin/ls", "/usr", true},
		{"/usr", "/usr", true},
		{"/usr2/bin", "/usr", false},
		{"/usr/", "/usr", true},
		{"/home/user", "/usr", false},
		{"/tmp/foo/bar", "/tmp", true},
	}
	for _, tc := range cases {
		got := isUnder(tc.path, tc.dir)
		if got != tc.want {
			t.Errorf("isUnder(%q, %q) = %v, attendu %v", tc.path, tc.dir, got, tc.want)
		}
	}
}
