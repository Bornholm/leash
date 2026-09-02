package mcphttp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bornholm/leash/pkg/leash"
)

// fakeEngine est un leash.Engine de test qui compte les appels concurrents
// à ExecWithStreams, pour prouver la sérialisation par workspace (C5).
type fakeEngine struct {
	current int32
	maxSeen atomic.Int32
	calls   atomic.Int32
	sleep   time.Duration
}

func (f *fakeEngine) Exec(ctx context.Context, script string) (*leash.ExecResult, error) {
	return f.ExecWithStreams(ctx, script, nil, io.Discard, io.Discard)
}

func (f *fakeEngine) ExecWithStreams(ctx context.Context, script string, stdin io.Reader, stdout, stderr io.Writer) (*leash.ExecResult, error) {
	f.calls.Add(1)
	n := atomic.AddInt32(&f.current, 1)
	for {
		max := f.maxSeen.Load()
		if n <= max || f.maxSeen.CompareAndSwap(max, n) {
			break
		}
	}
	if f.sleep > 0 {
		time.Sleep(f.sleep)
	}
	atomic.AddInt32(&f.current, -1)
	return &leash.ExecResult{ExitCode: 0}, nil
}

func (f *fakeEngine) Instructions() string { return "" }

func newTestManager(t *testing.T, factory engineFactory) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	cfg := &ServerConfig{WorkspaceRoot: root, TTL: time.Hour}
	m := NewManager(cfg, factory)
	return m, root
}

func TestWorkspace_ExecIsSerializedAcrossGoroutines(t *testing.T) {
	fe := &fakeEngine{sleep: 2 * time.Millisecond}
	factory := func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		return fe, func() {}, nil
	}
	m, _ := newTestManager(t, factory)
	defer m.Shutdown()

	ws, err := m.Acquire(context.Background(), "tenant", &APIKeyConfig{})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			if _, err := ws.Exec(context.Background(), "", "echo hi", nil, io.Discard, io.Discard); err != nil {
				t.Errorf("Exec: %v", err)
			}
		})
	}
	wg.Wait()

	if got := fe.calls.Load(); got != n {
		t.Fatalf("expected %d calls, got %d", n, got)
	}
	if got := fe.maxSeen.Load(); got != 1 {
		t.Fatalf("expected maxSeen == 1 (serialized execution), got %d", got)
	}
}

func TestManager_AcquireDoesNotDoubleCreate(t *testing.T) {
	var factoryCalls atomic.Int32
	factory := func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		factoryCalls.Add(1)
		time.Sleep(time.Millisecond) // élargit la fenêtre de course
		return &fakeEngine{}, func() {}, nil
	}
	m, _ := newTestManager(t, factory)
	defer m.Shutdown()

	const n = 30
	var wg sync.WaitGroup
	spaces := make([]*Workspace, n)
	for i := range n {
		wg.Go(func() {
			ws, err := m.Acquire(context.Background(), "same-tenant", &APIKeyConfig{})
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			spaces[i] = ws
		})
	}
	wg.Wait()

	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("expected factory called exactly once, got %d", got)
	}
	first := spaces[0]
	for i, ws := range spaces {
		if ws != first {
			t.Fatalf("workspace at index %d differs from the first one: double creation", i)
		}
	}
}

func TestManager_AcquireFailureCleansUpDir(t *testing.T) {
	factory := func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		return nil, nil, errors.New("boom")
	}
	m, root := newTestManager(t, factory)
	defer m.Shutdown()

	if _, err := m.Acquire(context.Background(), "tenant", &APIKeyConfig{}); err == nil {
		t.Fatal("expected error from factory to propagate")
	}

	if _, err := os.Stat(filepath.Join(root, "tenant")); !os.IsNotExist(err) {
		t.Fatalf("expected workspace dir to be removed after factory failure, stat err = %v", err)
	}
}

func TestManager_ReapOnceClosesExpiredWorkspaceWithoutUseAfterFree(t *testing.T) {
	var cleanupCalled atomic.Bool
	factory := func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		return &fakeEngine{}, func() { cleanupCalled.Store(true) }, nil
	}
	root := t.TempDir()
	cfg := &ServerConfig{WorkspaceRoot: root, TTL: time.Minute}
	m := NewManager(cfg, factory)
	defer m.Shutdown()

	base := time.Now()
	m.now = func() time.Time { return base }

	ws, err := m.Acquire(context.Background(), "tenant", &APIKeyConfig{})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	wsDir := ws.dir

	// Avant l'expiration : reapOnce ne doit rien fermer.
	m.reapOnce()
	if cleanupCalled.Load() {
		t.Fatal("workspace closed before TTL expiry")
	}

	// Après l'expiration logique (horloge injectée).
	m.now = func() time.Time { return base.Add(2 * time.Minute) }
	m.reapOnce()

	if !cleanupCalled.Load() {
		t.Fatal("expected cleanup to be called after TTL expiry")
	}
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Fatalf("expected workspace dir removed, stat err = %v", err)
	}

	m.mu.Lock()
	_, stillPresent := m.spaces["tenant"]
	m.mu.Unlock()
	if stillPresent {
		t.Fatal("expired workspace should have been removed from the cache (use-after-free risk otherwise)")
	}

	// Acquérir à nouveau doit re-créer un workspace frais, pas réutiliser l'ancien.
	ws2, err := m.Acquire(context.Background(), "tenant", &APIKeyConfig{})
	if err != nil {
		t.Fatalf("Acquire after reap: %v", err)
	}
	if ws2 == ws {
		t.Fatal("re-acquired workspace should be a new instance after reaping")
	}
}

func TestManager_Acquire_RejectsWhenAtMaxWorkspaces(t *testing.T) {
	factory := func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		return &fakeEngine{}, func() {}, nil
	}
	root := t.TempDir()
	cfg := &ServerConfig{WorkspaceRoot: root, TTL: time.Hour, MaxWorkspaces: 2}
	m := NewManager(cfg, factory)
	defer m.Shutdown()

	if _, err := m.Acquire(context.Background(), "tenant-a", &APIKeyConfig{}); err != nil {
		t.Fatalf("Acquire tenant-a: %v", err)
	}
	if _, err := m.Acquire(context.Background(), "tenant-b", &APIKeyConfig{}); err != nil {
		t.Fatalf("Acquire tenant-b: %v", err)
	}
	// Troisième workspace distinct → doit être refusé.
	_, err := m.Acquire(context.Background(), "tenant-c", &APIKeyConfig{})
	if err == nil {
		t.Fatal("expected ErrMaxWorkspacesReached when at limit")
	}
}

func TestManager_Acquire_ReturnsExistingWhenAtMaxWorkspaces(t *testing.T) {
	factory := func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		return &fakeEngine{}, func() {}, nil
	}
	root := t.TempDir()
	cfg := &ServerConfig{WorkspaceRoot: root, TTL: time.Hour, MaxWorkspaces: 1}
	m := NewManager(cfg, factory)
	defer m.Shutdown()

	if _, err := m.Acquire(context.Background(), "tenant-a", &APIKeyConfig{}); err != nil {
		t.Fatalf("Acquire tenant-a: %v", err)
	}
	// Réacquisition du même workspace : doit réussir même si le max est atteint.
	if _, err := m.Acquire(context.Background(), "tenant-a", &APIKeyConfig{}); err != nil {
		t.Fatalf("re-Acquire tenant-a at max: %v", err)
	}
}

func TestManager_Evict_RemovesWorkspaceAndCleansUp(t *testing.T) {
	var cleanupCalled atomic.Bool
	factory := func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		return &fakeEngine{}, func() { cleanupCalled.Store(true) }, nil
	}
	m, root := newTestManager(t, factory)
	defer m.Shutdown()

	ws, err := m.Acquire(context.Background(), "eph-tenant", &APIKeyConfig{})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	wsDir := ws.dir

	m.Evict("eph-tenant")

	if !cleanupCalled.Load() {
		t.Fatal("expected cleanup to be called on Evict")
	}
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Fatalf("expected workspace dir removed after Evict, stat err = %v", err)
	}

	m.mu.Lock()
	_, stillPresent := m.spaces["eph-tenant"]
	m.mu.Unlock()
	if stillPresent {
		t.Fatal("evicted workspace should be removed from the cache")
	}

	// Double evict doit être sans effet (pas de panique, pas de double close).
	m.Evict("eph-tenant")

	// Après éviction, le répertoire root doit être intact (on n'a pas supprimé
	// le WorkspaceRoot lui-même, seulement le sous-répertoire du workspace).
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("WorkspaceRoot should still exist after Evict: %v", err)
	}
}

func TestManager_ShutdownStopsReaperWithoutGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	factory := func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		return &fakeEngine{}, func() {}, nil
	}
	m, _ := newTestManager(t, factory)
	m.StartReaper(time.Millisecond)

	if _, err := m.Acquire(context.Background(), "tenant", &APIKeyConfig{}); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // laisse le reaper tourner au moins une fois
	m.Shutdown()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("goroutine leak suspected: before=%d after=%d", before, runtime.NumGoroutine())
}

// Deux clés API visant le MÊME tenant doivent obtenir chacune leur moteur,
// donc leur policy. Sans cela, la seconde clé hérite de la policy de la
// première au hasard de l'ordre d'arrivée : une clé restreinte au
// téléchargement s'est ainsi vue appliquer la policy de l'atelier (aucun
// accès réseau), et la commande refusée — vu en production le 2026-08-29.
// L'inverse est un défaut de sécurité : une clé étroite pourrait hériter
// d'une policy large.
func TestManager_EngineIsPerAPIKey(t *testing.T) {
	engines := map[string]*fakeEngine{}
	var mu sync.Mutex

	factory := func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		mu.Lock()
		defer mu.Unlock()
		eng := &fakeEngine{}
		engines[key.Name] = eng
		return eng, func() {}, nil
	}

	m, _ := newTestManager(t, factory)
	defer m.Shutdown()

	ctx := context.Background()
	atelier := &APIKeyConfig{Name: "ATELIER"}
	fetch := &APIKeyConfig{Name: "FETCH"}

	// La première clé crée le workspace.
	ws, err := m.Acquire(ctx, "tenant", atelier)
	if err != nil {
		t.Fatalf("Acquire (atelier): %v", err)
	}

	// La seconde clé retrouve LE MÊME workspace — les fichiers sont
	// partagés, c'est tout l'intérêt — mais avec SON moteur.
	ws2, err := m.Acquire(ctx, "tenant", fetch)
	if err != nil {
		t.Fatalf("Acquire (fetch): %v", err)
	}
	if ws2 != ws {
		t.Fatal("le workspace doit être partagé entre les clés du même tenant")
	}
	if ws.dir != ws2.dir {
		t.Fatal("le répertoire doit être partagé")
	}

	if len(engines) != 2 {
		t.Fatalf("%d moteur(s) construit(s), attendu 2 (un par clé)", len(engines))
	}
	if ws.engineFor("ATELIER") == ws.engineFor("FETCH") {
		t.Fatal("les deux clés partagent un moteur : la policy de l'une s'applique à l'autre")
	}

	// Chaque exécution part sur le moteur de SA clé.
	if _, err := ws.Exec(ctx, "FETCH", "fetch-video", nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("Exec (fetch): %v", err)
	}
	if engines["FETCH"].calls.Load() != 1 || engines["ATELIER"].calls.Load() != 0 {
		t.Errorf("l'exécution n'a pas emprunté le moteur de sa clé (fetch=%d, atelier=%d)",
			engines["FETCH"].calls.Load(), engines["ATELIER"].calls.Load())
	}

	// Une clé sans moteur sur ce workspace ne s'exécute pas en silence
	// sous la policy d'une autre.
	if _, err := ws.Exec(ctx, "INCONNUE", "echo", nil, io.Discard, io.Discard); err == nil {
		t.Error("une clé sans moteur doit échouer, jamais emprunter celui d'une autre")
	}
}
