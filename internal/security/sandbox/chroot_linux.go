//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

type chrootSandbox struct {
	cfg Config
}

// NewChroot construit un sandbox chroot (Linux uniquement).
// Nécessite les privilèges root. Préférer bwrap pour la sécurité.
func NewChroot(cfg Config) (Sandbox, error) {
	if cfg.Rootfs == "" {
		return nil, fmt.Errorf("chroot sandbox: rootfs is required")
	}
	return &chrootSandbox{cfg: cfg}, nil
}

func (s *chrootSandbox) Name() string { return "chroot" }

func (s *chrootSandbox) Wrap(_ context.Context, cmd *exec.Cmd) (*exec.Cmd, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Chroot = s.cfg.Rootfs
	if s.cfg.UID != nil && s.cfg.GID != nil {
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid: *s.cfg.UID,
			Gid: *s.cfg.GID,
		}
	}
	if cmd.Dir == "" {
		cmd.Dir = "/"
	}
	return cmd, nil
}

func (s *chrootSandbox) Close() error  { return nil }
func (s *chrootSandbox) Config() Config { return s.cfg }
