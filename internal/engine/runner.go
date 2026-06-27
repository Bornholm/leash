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
	policy       security.PolicyEngine
	reg          *registry.Registry
	auditor      *security.AuditLogger
	rl           *security.RateLimiter
	sandbox      sandbox.Sandbox
	workDir      string // répertoire de travail du shell (OpenHandler, $PWD, chemins relatifs)
	sharedTmpDir string // répertoire /tmp partagé entre tous les appels Exec quand PersistentTmp est activé
}

// New crée un Runner avec tous ses composants.
// Si sb est nil, le backend none (no-op) est utilisé.
// Si PersistentTmp est activé, un répertoire tmp partagé est créé une fois pour
// toute la durée de vie du Runner, et réutilisé entre tous les appels Exec.
func New(
	pol security.PolicyEngine,
	reg *registry.Registry,
	auditor *security.AuditLogger,
	rl *security.RateLimiter,
	sb sandbox.Sandbox,
	workDir string,
) *Runner {
	if sb == nil {
		sb = sandbox.NewNone()
	}
	r := &Runner{
		policy:  pol,
		reg:     reg,
		auditor: auditor,
		rl:      rl,
		sandbox: sb,
		workDir: workDir,
	}
	if sb.Config().PersistentTmp {
		if tmpDir, err := os.MkdirTemp("", "leash-tmp-*"); err == nil {
			r.sharedTmpDir = tmpDir
		}
	}
	return r
}

// Close libère les ressources du Runner (répertoire tmp partagé si PersistentTmp est activé).
func (r *Runner) Close() {
	if r.sharedTmpDir != "" {
		_ = os.RemoveAll(r.sharedTmpDir)
		r.sharedTmpDir = ""
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
	// 1. Parse AST
	prog, err := syntax.NewParser().Parse(strings.NewReader(script), "script")
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// 2. Validation AST (compte commandes, subshells, background jobs)
	if err := r.policy.ValidateAST(prog); err != nil {
		return nil, fmt.Errorf("policy violation: %w", err)
	}

	// 3. Contexte avec timeout + injection sandbox
	ctx, cancel := context.WithTimeout(ctx, r.policy.MaxExecDuration())
	defer cancel()
	ctx = sandbox.ContextWithSandbox(ctx, r.sandbox)

	// Si PersistentTmp est activé, injecter le répertoire tmp partagé créé à
	// l'initialisation du Runner, afin que /tmp persiste entre les appels Exec.
	if r.sharedTmpDir != "" {
		ctx = sandbox.ContextWithTmpDir(ctx, r.sharedTmpDir)
	}

	// 4. Streams bornés
	limitedOut := newLimitedWriter(stdout, r.policy.MaxOutputBytes())
	limitedErr := newLimitedWriter(stderr, r.policy.MaxOutputBytes())

	// 5. Environnement sécurisé
	safeEnv := r.policy.SafeEnvironment()
	envList := make([]string, 0, len(safeEnv))
	for k, v := range safeEnv {
		envList = append(envList, k+"="+v)
	}

	// 6. AuditRecorder pour cette exécution
	recorder := security.NewAuditRecorder()

	// 7. Création du runner mvdan
	runnerOpts := []interp.RunnerOption{
		interp.StdIO(stdin, limitedOut, limitedErr),
		interp.Env(expand.ListEnviron(envList...)),
		interp.ExecHandlers(NewExecHandler(r.reg, r.policy, r.rl, recorder)),
		interp.OpenHandler(NewFSOpenHandler(r.sandbox.Config())),
		interp.ReadDirHandler2(NewFSReadDirHandler(r.sandbox.Config())),
	}
	if r.workDir != "" {
		runnerOpts = append(runnerOpts, interp.Dir(r.workDir))
	}
	mvRunner, err := interp.New(runnerOpts...)
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
