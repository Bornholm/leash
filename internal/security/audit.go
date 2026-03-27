package security

import (
	"context"
	"log/slog"
	"time"
)

// AuditRecorder collecte les CommandRecord pendant une exécution.
// Non thread-safe : une instance par appel à Runner.ExecWithStreams.
type AuditRecorder struct {
	commands []CommandRecord
	active   map[string]*CommandRecord
}

// NewAuditRecorder crée un AuditRecorder vide.
func NewAuditRecorder() *AuditRecorder {
	return &AuditRecorder{
		active: make(map[string]*CommandRecord),
	}
}

// Start enregistre le début d'une commande et retourne une clé pour Finish.
func (r *AuditRecorder) Start(command string, args []string, isSkill bool) string {
	key := command + "@" + time.Now().String()
	r.active[key] = &CommandRecord{
		Command:   command,
		Args:      args,
		StartTime: time.Now(),
		IsSkill:   isSkill,
	}
	return key
}

// Finish enregistre la fin d'une commande.
func (r *AuditRecorder) Finish(key string, exitCode int) {
	rec, ok := r.active[key]
	if !ok {
		return
	}
	rec.Duration = time.Since(rec.StartTime)
	rec.ExitCode = exitCode
	r.commands = append(r.commands, *rec)
	delete(r.active, key)
}

// RecordBlocked enregistre une commande bloquée.
func (r *AuditRecorder) RecordBlocked(command string, args []string, reason string) {
	r.commands = append(r.commands, CommandRecord{
		Command:   command,
		Args:      args,
		StartTime: time.Now(),
		Blocked:   true,
		Reason:    reason,
	})
}

// Build construit l'AuditTrail final.
func (r *AuditRecorder) Build() *AuditTrail {
	return &AuditTrail{Commands: r.commands}
}

// AuditLogger persiste les AuditTrail via slog. Thread-safe.
type AuditLogger struct {
	log *slog.Logger
}

// NewAuditLogger crée un AuditLogger.
func NewAuditLogger(log *slog.Logger) *AuditLogger {
	return &AuditLogger{log: log}
}

// Log persiste un AuditTrail.
func (a *AuditLogger) Log(ctx context.Context, trail *AuditTrail) {
	if trail == nil {
		return
	}
	for _, cmd := range trail.Commands {
		attrs := []any{
			slog.String("command", cmd.Command),
			slog.Any("args", cmd.Args),
			slog.Duration("duration", cmd.Duration),
			slog.Int("exit_code", cmd.ExitCode),
			slog.Bool("blocked", cmd.Blocked),
			slog.Bool("is_skill", cmd.IsSkill),
		}
		if cmd.Blocked {
			attrs = append(attrs, slog.String("reason", cmd.Reason))
			a.log.WarnContext(ctx, "command blocked", attrs...)
		} else {
			a.log.InfoContext(ctx, "command executed", attrs...)
		}
	}
}
