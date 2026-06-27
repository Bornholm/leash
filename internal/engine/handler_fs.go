package engine

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"mvdan.cc/sh/v3/interp"

	"github.com/bornholm/leash/internal/security/sandbox"
)

// NewFSOpenHandler retourne un OpenHandlerFunc qui applique les règles
// readonly/readwrite/tmpfs de la config sandbox aux builtins shell et redirections I/O.
// Backend none ou disabled → passe-travers (os.OpenFile).
func NewFSOpenHandler(cfg sandbox.Config) interp.OpenHandlerFunc {
	if !cfg.Enabled || cfg.Backend == "" || cfg.Backend == "none" {
		return func(_ context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
			return os.OpenFile(path, flag, perm)
		}
	}
	return func(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
		absPath := resolvePath(ctx, path)
		isWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0

		// ReadwriteBinds (chemin hôte source) → lecture et écriture
		for _, b := range cfg.ReadwriteBinds {
			if isUnder(absPath, b.Source) {
				return os.OpenFile(absPath, flag, perm)
			}
		}

		// Tmpfs → lecture et écriture (éphémère, cohérent avec bwrap)
		for _, t := range cfg.Tmpfs {
			if isUnder(absPath, t) {
				if cfg.PersistentTmp {
					if sharedTmpDir := sandbox.TmpDirFromContext(ctx); sharedTmpDir != "" {
						rel := strings.TrimPrefix(absPath, filepath.Clean(t))
						return os.OpenFile(filepath.Join(sharedTmpDir, rel), flag, perm)
					}
				}
				return os.OpenFile(absPath, flag, perm)
			}
		}

		// ReadonlyBinds → lecture uniquement
		for _, ro := range cfg.ReadonlyBinds {
			if isUnder(absPath, ro) {
				if isWrite {
					return nil, &os.PathError{Op: "open", Path: path, Err: syscall.EROFS}
				}
				return os.OpenFile(absPath, flag, perm)
			}
		}

		// Chemins auto-montés par bwrap
		if cfg.Backend == "bwrap" {
			if isUnder(absPath, "/proc") {
				if isWrite {
					return nil, &os.PathError{Op: "open", Path: path, Err: syscall.EROFS}
				}
				return os.OpenFile(absPath, flag, perm)
			}
			if isUnder(absPath, "/dev") {
				return os.OpenFile(absPath, flag, perm)
			}
		}

		return nil, &os.PathError{Op: "open", Path: path, Err: syscall.EACCES}
	}
}

// NewFSReadDirHandler retourne un ReadDirHandlerFunc2 appliquant les mêmes règles
// d'accès aux expansions glob et lectures de répertoire.
func NewFSReadDirHandler(cfg sandbox.Config) interp.ReadDirHandlerFunc2 {
	if !cfg.Enabled || cfg.Backend == "" || cfg.Backend == "none" {
		return func(_ context.Context, path string) ([]fs.DirEntry, error) {
			return os.ReadDir(path)
		}
	}
	return func(ctx context.Context, path string) ([]fs.DirEntry, error) {
		absPath := resolvePath(ctx, path)

		for _, b := range cfg.ReadwriteBinds {
			if isUnder(absPath, b.Source) {
				return os.ReadDir(absPath)
			}
		}
		for _, t := range cfg.Tmpfs {
			if isUnder(absPath, t) {
				if cfg.PersistentTmp {
					if sharedTmpDir := sandbox.TmpDirFromContext(ctx); sharedTmpDir != "" {
						rel := strings.TrimPrefix(absPath, filepath.Clean(t))
						return os.ReadDir(filepath.Join(sharedTmpDir, rel))
					}
				}
				return os.ReadDir(absPath)
			}
		}
		for _, ro := range cfg.ReadonlyBinds {
			if isUnder(absPath, ro) {
				return os.ReadDir(absPath)
			}
		}
		if cfg.Backend == "bwrap" {
			if isUnder(absPath, "/proc") || isUnder(absPath, "/dev") {
				return os.ReadDir(absPath)
			}
		}

		return nil, &os.PathError{Op: "readdir", Path: path, Err: syscall.EACCES}
	}
}

// resolvePath retourne le chemin absolu normalisé.
// Les chemins relatifs sont résolus par rapport au répertoire courant du shell
// (interp.HandlerCtx(ctx).Dir), qui reflète le Dir configuré sur le runner
// mvdan (WithWorkDir) ainsi que tout cd exécuté en cours de script.
func resolvePath(ctx context.Context, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if dir := handlerDir(ctx); dir != "" {
		return filepath.Clean(filepath.Join(dir, path))
	}
	cwd, _ := os.Getwd()
	return filepath.Clean(filepath.Join(cwd, path))
}

// handlerDir extrait le répertoire courant du HandlerContext mvdan.
// interp.HandlerCtx panique si le contexte ne contient pas de HandlerContext
// (cas des tests unitaires sans runner) — le recover absorbe ce cas.
func handlerDir(ctx context.Context) (dir string) {
	defer func() { _ = recover() }()
	return interp.HandlerCtx(ctx).Dir
}

// isUnder vérifie si path est sous dir (ou égal à dir).
// Évite les faux-positifs : "/usr2" n'est pas sous "/usr".
func isUnder(path, dir string) bool {
	dir = filepath.Clean(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
