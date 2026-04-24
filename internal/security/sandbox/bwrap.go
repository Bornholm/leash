package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type bwrapSandbox struct {
	cfg      Config
	bwrapBin string
}

// NewBwrap construit un sandbox bubblewrap.
// Retourne une erreur si bwrap est absent du PATH ou si la config est invalide.
func NewBwrap(cfg Config) (Sandbox, error) {
	bin, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("bubblewrap not found in PATH: %w", err)
	}
	if err := validateBwrapConfig(cfg); err != nil {
		return nil, err
	}
	return &bwrapSandbox{cfg: cfg, bwrapBin: bin}, nil
}

func validateBwrapConfig(cfg Config) error {
	if len(cfg.ReadonlyBinds) == 0 && len(cfg.ReadwriteBinds) == 0 {
		return fmt.Errorf("bwrap sandbox: at least one bind mount is required")
	}
	for _, b := range cfg.ReadwriteBinds {
		if b.Source == "" || b.Target == "" {
			return fmt.Errorf("bwrap sandbox: bind mount requires source and target")
		}
	}
	return nil
}

func (s *bwrapSandbox) Name() string { return "bwrap" }

func (s *bwrapSandbox) buildArgs(origEnv []string) []string {
	args := []string{"--new-session"}

	if s.cfg.DieWithParent {
		args = append(args, "--die-with-parent")
	}

	for _, p := range s.cfg.ReadonlyBinds {
		args = append(args, "--ro-bind", p, p)
	}
	for _, b := range s.cfg.ReadwriteBinds {
		args = append(args, "--bind", b.Source, b.Target)
	}
	for _, t := range s.cfg.Tmpfs {
		args = append(args, "--tmpfs", t)
	}
	for _, sym := range s.cfg.Symlinks {
		args = append(args, "--symlink", sym.Source, sym.Target)
	}

	args = append(args, "--proc", "/proc")
	args = append(args, "--dev", "/dev")

	if s.cfg.Unshare.Network {
		args = append(args, "--unshare-net")
	}
	if s.cfg.Unshare.PID {
		args = append(args, "--unshare-pid")
	}
	if s.cfg.Unshare.IPC {
		args = append(args, "--unshare-ipc")
	}
	if s.cfg.Unshare.UTS {
		args = append(args, "--unshare-uts")
	}
	if s.cfg.Unshare.User {
		args = append(args, "--unshare-user")
	}

	if s.cfg.Workdir != "" {
		args = append(args, "--chdir", s.cfg.Workdir)
	}

	args = append(args, "--cap-drop", "ALL")

	for _, kv := range origEnv {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			continue
		}
		args = append(args, "--setenv", kv[:idx], kv[idx+1:])
	}

	return args
}

func (s *bwrapSandbox) Wrap(ctx context.Context, cmd *exec.Cmd) (*exec.Cmd, error) {
	bwrapArgs := s.buildArgs(cmd.Env)
	bwrapArgs = append(bwrapArgs, "--")
	bwrapArgs = append(bwrapArgs, cmd.Path)
	bwrapArgs = append(bwrapArgs, cmd.Args[1:]...)

	wrapped := exec.CommandContext(ctx, s.bwrapBin, bwrapArgs...)
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	wrapped.Env = []string{} // tout passé via --setenv
	wrapped.Dir = ""         // chdir géré par --chdir
	return wrapped, nil
}

func (s *bwrapSandbox) Close() error  { return nil }
func (s *bwrapSandbox) Config() Config { return s.cfg }
