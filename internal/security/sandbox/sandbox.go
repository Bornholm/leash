package sandbox

import (
	"context"
	"os/exec"
)

// Sandbox isole l'exécution d'une commande système.
// Les implémentations transforment cmd pour appliquer les contraintes d'isolation.
type Sandbox interface {
	// Name identifie le backend (none, chroot, bwrap).
	Name() string

	// Wrap retourne la commande à exécuter effectivement.
	// Peut retourner cmd inchangée (backend none) ou une commande préfixée (bwrap, chroot).
	Wrap(ctx context.Context, cmd *exec.Cmd) (*exec.Cmd, error)

	// Close libère les ressources allouées (mounts, rootfs temp...).
	Close() error

	// Config retourne la configuration utilisée pour construire ce sandbox.
	Config() Config
}
