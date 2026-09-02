package mcphttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
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
// UN MOTEUR PAR CLÉ API, un seul répertoire. Deux clés peuvent viser le
// même tenant avec des policies différentes — c'est ainsi qu'un appelant
// télécharge sous une policy réseau étroite puis édite les mêmes fichiers
// sous une policy étanche. Le moteur PORTE la policy : le partager entre
// clés donnerait à la seconde les permissions de la première, au hasard de
// l'ordre d'arrivée. Le répertoire, sa suppression et la sérialisation des
// exécutions restent, eux, communs au tenant.
type Workspace struct {
	id  string
	dir string
	// apiKey est la clé qui a créé le workspace : pour les journaux, jamais
	// pour décider d'une policy.
	apiKey   string
	execSlot chan struct{} // buffered(1), sérialise Exec de façon context-aware

	enginesMu sync.Mutex
	engines   map[string]*workspaceEngine

	lastAccess atomic.Int64
	closed     atomic.Bool
}

// workspaceEngine est le moteur d'UNE clé API sur ce workspace, avec son
// nettoyage.
type workspaceEngine struct {
	engine  leash.Engine
	cleanup func()
}

// engineFor retourne le moteur de la clé, ou nil si elle n'en a pas encore.
func (w *Workspace) engineFor(keyName string) leash.Engine {
	w.enginesMu.Lock()
	defer w.enginesMu.Unlock()
	if entry, ok := w.engines[keyName]; ok {
		return entry.engine
	}
	return nil
}

// putEngine enregistre le moteur d'une clé. Retourne le moteur retenu :
// en cas de création concurrente, le premier arrivé gagne et le second est
// nettoyé par l'appelant.
func (w *Workspace) putEngine(keyName string, entry *workspaceEngine) (kept leash.Engine, replaced bool) {
	w.enginesMu.Lock()
	defer w.enginesMu.Unlock()
	if existing, ok := w.engines[keyName]; ok {
		return existing.engine, true
	}
	w.engines[keyName] = entry
	return entry.engine, false
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
// keyName désigne la clé API de l'appel : c'est ELLE qui choisit le
// moteur, donc la policy appliquée.
func (w *Workspace) Exec(ctx context.Context, keyName, script string, stdin io.Reader, stdout, stderr io.Writer) (*leash.ExecResult, error) {
	engine := w.engineFor(keyName)
	if engine == nil {
		return nil, fmt.Errorf("mcphttp: no engine for API key %q on this workspace", keyName)
	}

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
		"api_key", keyName,
		"script", truncateForLog(script, maxScriptLogLen),
	)

	result, err := engine.ExecWithStreams(ctx, script, stdin, stdout, stderr)
	if err != nil {
		slog.DebugContext(ctx, "mcphttp: script execution failed",
			"workspace_id", w.id,
			"api_key", keyName,
			"error", err,
		)
		return result, err
	}

	slog.DebugContext(ctx, "mcphttp: script executed",
		"workspace_id", w.id,
		"api_key", keyName,
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

	// Tous les moteurs du workspace, pas seulement celui de la clé qui l'a
	// créé : chacun tient ses propres ressources.
	w.enginesMu.Lock()
	entries := make([]*workspaceEngine, 0, len(w.engines))
	for _, entry := range w.engines {
		entries = append(entries, entry)
	}
	w.engines = nil
	w.enginesMu.Unlock()

	for _, entry := range entries {
		if entry.cleanup != nil {
			entry.cleanup()
		}
	}

	return os.RemoveAll(w.dir)
}

// instructions retourne les consignes du moteur de la clé : elles
// décrivent les commandes autorisées, qui dépendent de la policy — donc de
// la clé, pas du workspace.
func (w *Workspace) instructions(keyName string) string {
	if engine := w.engineFor(keyName); engine != nil {
		return engine.Instructions()
	}
	return ""
}
