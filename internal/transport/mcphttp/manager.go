package mcphttp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bornholm/leash/pkg/leash"
)

// engineFactory construit l'Engine et son cleanup pour un workspace donné.
// injectable pour les tests.
type engineFactory func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error)

// Manager gère le cycle de vie des Workspace : création paresseuse, cache,
// et reaping TTL.
//
// Discipline de lock : Manager.mu est toujours acquis avant Workspace.execMu,
// jamais l'inverse. Acquire tient mu pendant la création (appel à factory,
// donc à leash.New) pour éviter la double création concurrente — choix
// assumé car la création est rare et bornée dans le temps.
type Manager struct {
	mu      sync.Mutex
	cfg     *ServerConfig
	spaces  map[string]*Workspace
	factory engineFactory
	now     func() time.Time

	wg     sync.WaitGroup
	stopCh chan struct{}
}

// NewManager crée un Manager et démarre son reaper TTL.
func NewManager(cfg *ServerConfig, factory engineFactory) *Manager {
	m := &Manager{
		cfg:     cfg,
		spaces:  make(map[string]*Workspace),
		factory: factory,
		now:     time.Now,
		stopCh:  make(chan struct{}),
	}
	return m
}

// ErrMaxWorkspacesReached est retournée quand le nombre maximal de workspaces
// actifs est atteint.
var ErrMaxWorkspacesReached = fmt.Errorf("mcphttp: maximum number of workspaces reached")

// Acquire renvoie le workspace identifié par id, le créant si nécessaire.
func (m *Manager) Acquire(ctx context.Context, id string, key *APIKeyConfig) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ws, ok := m.spaces[id]; ok {
		ws.lastAccess.Store(m.now().UnixNano())
		slog.DebugContext(ctx, "mcphttp: reusing existing workspace", "workspace_id", id, "api_key", key.Name)
		return ws, nil
	}

	if m.cfg.MaxWorkspaces > 0 && len(m.spaces) >= m.cfg.MaxWorkspaces {
		slog.WarnContext(ctx, "mcphttp: max workspaces reached",
			"current", len(m.spaces), "max", m.cfg.MaxWorkspaces)
		return nil, ErrMaxWorkspacesReached
	}

	dir := filepath.Join(m.cfg.WorkspaceRoot, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mcphttp: creating workspace dir: %w", err)
	}

	// On construit l'Engine (et ses connexions MCP externes, potentiellement
	// long-lived) avec un contexte propre, indépendant de ctx (le contexte de
	// la requête HTTP qui a déclenché cette création) : ctx sera annulé dès
	// la fin de cette requête, alors que le workspace et ses connexions MCP
	// doivent survivre bien au-delà, jusqu'au TTL ou à Manager.Shutdown.
	eng, cleanup, err := m.factory(context.Background(), dir, key)
	if err != nil {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slog.ErrorContext(ctx, "mcphttp: failed to remove workspace dir after engine build failure", "dir", dir, "error", rmErr)
		}
		return nil, fmt.Errorf("mcphttp: building engine: %w", err)
	}

	ws := &Workspace{
		id:       id,
		dir:      dir,
		apiKey:   key.Name,
		engine:   eng,
		cleanup:  cleanup,
		execSlot: make(chan struct{}, 1),
	}
	ws.lastAccess.Store(m.now().UnixNano())
	m.spaces[id] = ws
	slog.InfoContext(ctx, "mcphttp: created new workspace", "workspace_id", id, "api_key", key.Name, "dir", dir)
	return ws, nil
}

// StartReaper lance une goroutine qui ferme périodiquement les workspaces
// inactifs depuis plus de cfg.TTL. Elle s'arrête sur Shutdown.
func (m *Manager) StartReaper(interval time.Duration) {
	m.wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.reapOnce()
			}
		}
	})
}

// reapOnce collecte les workspaces expirés sous mu, puis les ferme hors mu
// (discipline de lock : ne jamais tenir Manager.mu pendant Workspace.close()).
func (m *Manager) reapOnce() {
	if m.cfg.TTL <= 0 {
		return
	}

	deadline := m.now().Add(-m.cfg.TTL).UnixNano()

	var expired []*Workspace
	m.mu.Lock()
	for id, ws := range m.spaces {
		if ws.lastAccess.Load() < deadline {
			expired = append(expired, ws)
			delete(m.spaces, id)
		}
	}
	m.mu.Unlock()

	for _, ws := range expired {
		slog.InfoContext(context.Background(), "mcphttp: reaping inactive workspace", "workspace_id", ws.id, "api_key", ws.apiKey)
		if err := ws.close(); err != nil {
			slog.ErrorContext(context.Background(), "mcphttp: error closing reaped workspace", "workspace_id", ws.id, "error", err)
		}
	}
}

// Evict ferme et supprime immédiatement le workspace identifié par id.
// Sans effet si le workspace n'existe plus (reaper concurrent ou double appel).
func (m *Manager) Evict(id string) {
	m.mu.Lock()
	ws, ok := m.spaces[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.spaces, id)
	m.mu.Unlock()

	slog.Info("mcphttp: evicting ephemeral workspace", "workspace_id", id, "api_key", ws.apiKey)
	if err := ws.close(); err != nil {
		slog.Error("mcphttp: error closing evicted workspace", "workspace_id", id, "error", err)
	}
}

// Shutdown arrête le reaper et ferme tous les workspaces actifs.
func (m *Manager) Shutdown() {
	close(m.stopCh)
	m.wg.Wait()

	m.mu.Lock()
	spaces := m.spaces
	m.spaces = make(map[string]*Workspace)
	m.mu.Unlock()

	for _, ws := range spaces {
		if err := ws.close(); err != nil {
			slog.Error("mcphttp: error closing workspace during shutdown", "workspace_id", ws.id, "error", err)
		}
	}
}
