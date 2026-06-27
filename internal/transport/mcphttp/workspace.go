package mcphttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/bornholm/leash/pkg/leash"
)

// ErrWorkspaceClosed est retournée quand Exec est appelé sur un workspace déjà fermé.
var ErrWorkspaceClosed = errors.New("workspace is closed")

// Workspace représente un espace de travail isolé pour un tenant donné.
//
// Sérialisation : execSlot (canal buffered à 1) garantit qu'un seul Exec
// s'exécute à la fois. Contrairement à un sync.Mutex, le select sur execSlot
// est context-aware : si le contexte est annulé pendant l'attente du slot,
// l'appel échoue immédiatement au lieu de bloquer indéfiniment.
//
// Fermeture : close() acquiert execSlot pour attendre la fin d'un éventuel
// Exec en cours, positionne closed=true, puis libère le slot. Les Exec
// ultérieurs voient closed=true après acquisition du slot et retournent
// ErrWorkspaceClosed sans toucher au moteur déjà nettoyé.
type Workspace struct {
	id       string
	dir      string
	apiKey   string
	execSlot chan struct{} // buffered(1), sérialise Exec de façon context-aware
	engine   leash.Engine
	cleanup  func()

	lastAccess atomic.Int64
	closed     atomic.Bool
}

const maxScriptLogLen = 200

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Exec sérialise tous les appels d'exécution sur ce workspace. Si le
// workspace est occupé et que le contexte est annulé avant que le slot soit
// disponible, l'appel retourne immédiatement avec l'erreur du contexte.
func (w *Workspace) Exec(ctx context.Context, script string, stdin io.Reader, stdout, stderr io.Writer) (*leash.ExecResult, error) {
	select {
	case w.execSlot <- struct{}{}:
		// slot acquis
	case <-ctx.Done():
		return nil, fmt.Errorf("workspace busy: %w", ctx.Err())
	}
	defer func() { <-w.execSlot }()

	if w.closed.Load() {
		return nil, ErrWorkspaceClosed
	}

	w.lastAccess.Store(time.Now().UnixNano())

	slog.DebugContext(ctx, "mcphttp: executing script",
		"workspace_id", w.id,
		"api_key", w.apiKey,
		"script", truncateForLog(script, maxScriptLogLen),
	)

	result, err := w.engine.ExecWithStreams(ctx, script, stdin, stdout, stderr)
	if err != nil {
		slog.DebugContext(ctx, "mcphttp: script execution failed",
			"workspace_id", w.id,
			"api_key", w.apiKey,
			"error", err,
		)
		return result, err
	}

	slog.DebugContext(ctx, "mcphttp: script executed",
		"workspace_id", w.id,
		"api_key", w.apiKey,
		"exit_code", result.ExitCode,
		"duration", result.Duration,
	)

	return result, nil
}

// close attend la fin d'un éventuel Exec en cours, marque le workspace comme
// fermé pour que les Exec en attente échouent proprement, puis libère les
// ressources. Thread-safe et idempotent (les appels suivants retournent nil
// immédiatement).
func (w *Workspace) close() error {
	if !w.closed.CompareAndSwap(false, true) {
		return nil // déjà fermé
	}
	// Acquérir le slot pour attendre la fin d'un Exec en cours.
	w.execSlot <- struct{}{}
	// Libérer le slot : les Exec en attente peuvent maintenant acquérir le
	// slot, lire closed=true et retourner ErrWorkspaceClosed.
	<-w.execSlot

	if w.cleanup != nil {
		w.cleanup()
	}
	return os.RemoveAll(w.dir)
}
