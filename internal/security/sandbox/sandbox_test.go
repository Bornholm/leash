package sandbox_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/bornholm/leash/internal/security/sandbox"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name        string
		cfg         sandbox.Config
		wantBackend string
		wantErr     bool
	}{
		{
			name:        "config vide → none",
			cfg:         sandbox.Config{},
			wantBackend: "none",
		},
		{
			name:        "enabled=false backend=bwrap → none (disabled prime)",
			cfg:         sandbox.Config{Enabled: false, Backend: "bwrap"},
			wantBackend: "none",
		},
		{
			name:    "enabled=true backend inconnu → erreur",
			cfg:     sandbox.Config{Enabled: true, Backend: "unknown"},
			wantErr: true,
		},
		{
			name:        "enabled=false backend=none → none",
			cfg:         sandbox.Config{Enabled: false, Backend: "none"},
			wantBackend: "none",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb, err := sandbox.New(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("attendu une erreur, aucune reçue")
				}
				return
			}
			if err != nil {
				t.Fatalf("erreur inattendue : %v", err)
			}
			if sb.Name() != tc.wantBackend {
				t.Errorf("backend = %q, attendu %q", sb.Name(), tc.wantBackend)
			}
		})
	}
}

func TestNoneSandbox(t *testing.T) {
	sb := sandbox.NewNone()

	if sb.Name() != "none" {
		t.Errorf("Name() = %q, attendu %q", sb.Name(), "none")
	}

	t.Run("Wrap retourne cmd inchangé", func(t *testing.T) {
		cmd := exec.Command("echo", "hello")
		wrapped, err := sb.Wrap(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Wrap() erreur inattendue : %v", err)
		}
		if wrapped != cmd {
			t.Error("Wrap() doit retourner le même pointeur cmd")
		}
	})

	t.Run("Close retourne nil", func(t *testing.T) {
		if err := sb.Close(); err != nil {
			t.Errorf("Close() = %v, attendu nil", err)
		}
	})
}

func TestSandboxFromContext(t *testing.T) {
	t.Run("contexte sans sandbox → none", func(t *testing.T) {
		sb := sandbox.SandboxFromContext(context.Background())
		if sb == nil {
			t.Fatal("SandboxFromContext ne doit pas retourner nil")
		}
		if sb.Name() != "none" {
			t.Errorf("Name() = %q, attendu %q", sb.Name(), "none")
		}
	})

	t.Run("contexte avec sandbox → sandbox injecté", func(t *testing.T) {
		none := sandbox.NewNone()
		ctx := sandbox.ContextWithSandbox(context.Background(), none)
		sb := sandbox.SandboxFromContext(ctx)
		if sb != none {
			t.Error("SandboxFromContext doit retourner le sandbox injecté")
		}
	})
}
