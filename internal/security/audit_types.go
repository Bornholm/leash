package security

import "time"

// AuditTrail contient l'historique des commandes exécutées pendant un script.
type AuditTrail struct {
	Commands []CommandRecord
}

// CommandRecord décrit l'exécution d'une commande individuelle.
type CommandRecord struct {
	Command   string
	Args      []string
	StartTime time.Time
	Duration  time.Duration
	ExitCode  int
	// Blocked est vrai si la commande a été bloquée par la policy.
	Blocked bool
	// Reason explique pourquoi la commande a été bloquée.
	Reason string
	// IsSkill est vrai si la commande était un skill enregistré.
	IsSkill bool
}
