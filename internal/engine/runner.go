package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"

	"github.com/bornholm/leash/internal/registry"
	"github.com/bornholm/leash/internal/security"
	"github.com/bornholm/leash/internal/security/sandbox"
)

// Runner est l'implémentation concrète de Engine.
type Runner struct {
	policy  security.PolicyEngine
	reg     *registry.Registry
	auditor *security.AuditLogger
	rl      *security.RateLimiter
	sandbox sandbox.Sandbox
}

// New crée un Runner avec tous ses composants.
// Si sb est nil, le backend none (no-op) est utilisé.
func New(
	pol security.PolicyEngine,
	reg *registry.Registry,
	auditor *security.AuditLogger,
	rl *security.RateLimiter,
	sb sandbox.Sandbox,
) *Runner {
	if sb == nil {
		sb = sandbox.NewNone()
	}
	return &Runner{
		policy:  pol,
		reg:     reg,
		auditor: auditor,
		rl:      rl,
		sandbox: sb,
	}
}

// muxWriter écrit vers dst et enregistre chaque fragment dans combined (thread-safe).
type muxWriter struct {
	mu       *sync.Mutex
	combined *[]OutputChunk
	isStderr bool
	dst      io.Writer
}

func (w *muxWriter) Write(p []byte) (int, error) {
	chunk := make([]byte, len(p))
	copy(chunk, p)
	w.mu.Lock()
	*w.combined = append(*w.combined, OutputChunk{IsStderr: w.isStderr, Data: chunk})
	w.mu.Unlock()
	return w.dst.Write(p)
}

// Exec implémente Engine.Exec.
func (r *Runner) Exec(ctx context.Context, script string) (*ExecResult, error) {
	var stdout, stderr bytes.Buffer
	var mu sync.Mutex
	var combined []OutputChunk

	muxOut := &muxWriter{mu: &mu, combined: &combined, isStderr: false, dst: &stdout}
	muxErr := &muxWriter{mu: &mu, combined: &combined, isStderr: true, dst: &stderr}

	result, err := r.ExecWithStreams(ctx, script, strings.NewReader(""), muxOut, muxErr)
	if result != nil {
		result.Stdout = stdout.Bytes()
		result.Stderr = stderr.Bytes()
		result.Combined = combined
	}
	return result, err
}

// ExecWithStreams implémente Engine.ExecWithStreams.
func (r *Runner) ExecWithStreams(ctx context.Context, script string, stdin io.Reader, stdout, stderr io.Writer) (*ExecResult, error) {
	// 1. Vérification textuelle des patterns bloqués (rapide)
	if blocked, pattern := r.policy.IsBlockedPattern(script); blocked {
		return &ExecResult{ExitCode: 1}, fmt.Errorf("blocked pattern detected: %q", pattern)
	}

	// 2. Parse AST
	prog, err := syntax.NewParser().Parse(strings.NewReader(script), "script")
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// 3. Validation AST (compte commandes, subshells, background jobs)
	if err := r.policy.ValidateAST(prog); err != nil {
		return nil, fmt.Errorf("policy violation: %w", err)
	}

	// 4. Contexte avec timeout + injection sandbox
	ctx, cancel := context.WithTimeout(ctx, r.policy.MaxExecDuration())
	defer cancel()
	ctx = sandbox.ContextWithSandbox(ctx, r.sandbox)

	// Si PersistentTmp est activé, créer un répertoire tmp partagé entre toutes les commandes du script.
	if r.sandbox.Config().PersistentTmp {
		tmpDir, err := os.MkdirTemp("", "leash-tmp-*")
		if err != nil {
			return nil, fmt.Errorf("create shared tmp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		ctx = sandbox.ContextWithTmpDir(ctx, tmpDir)
	}

	// 5. Streams bornés
	limitedOut := newLimitedWriter(stdout, r.policy.MaxOutputBytes())
	limitedErr := newLimitedWriter(stderr, r.policy.MaxOutputBytes())

	// 6. Environnement sécurisé
	safeEnv := r.policy.SafeEnvironment()
	envList := make([]string, 0, len(safeEnv))
	for k, v := range safeEnv {
		envList = append(envList, k+"="+v)
	}

	// 7. AuditRecorder pour cette exécution
	recorder := security.NewAuditRecorder()

	// 8. Création du runner mvdan
	mvRunner, err := interp.New(
		interp.StdIO(stdin, limitedOut, limitedErr),
		interp.Env(expand.ListEnviron(envList...)),
		interp.ExecHandlers(NewExecHandler(r.reg, r.policy, r.rl, recorder)),
		interp.OpenHandler(NewFSOpenHandler(r.sandbox.Config())),
		interp.ReadDirHandler2(NewFSReadDirHandler(r.sandbox.Config())),
	)
	if err != nil {
		return nil, fmt.Errorf("creating runner: %w", err)
	}

	start := time.Now()
	runErr := mvRunner.Run(ctx, prog)
	duration := time.Since(start)

	exitCode := 0

	// Tester le timeout AVANT IsExitStatus (contexte annulé)
	if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		exitCode = 124 // convention timeout shell
		runErr = nil
	} else if status, ok := interp.IsExitStatus(runErr); ok {
		exitCode = int(status)
		runErr = nil
	}

	trail := recorder.Build()
	if r.auditor != nil {
		r.auditor.Log(ctx, trail)
	}

	return &ExecResult{
		ExitCode: exitCode,
		Duration: duration,
		Audit:    trail,
	}, runErr
}

// Registry implémente Engine.Registry.
func (r *Runner) Registry() *registry.Registry { return r.reg }

// Policy implémente Engine.Policy.
func (r *Runner) Policy() security.PolicyEngine { return r.policy }

// limitedWriter borne la taille de sortie.
type limitedWriter struct {
	w       io.Writer
	written int64
	max     int64
}

func newLimitedWriter(w io.Writer, max int64) *limitedWriter {
	return &limitedWriter{w: w, max: max}
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	remaining := l.max - l.written
	if remaining <= 0 {
		return 0, fmt.Errorf("output limit exceeded (%d bytes)", l.max)
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := l.w.Write(p)
	l.written += int64(n)
	return n, err
}
