package engine

import (
	"context"
	"io"
	"time"

	"github.com/bornholm/leash/internal/registry"
	"github.com/bornholm/leash/internal/security"
)

// Engine est l'interface principale du moteur d'exécution shell.
type Engine interface {
	// Exec exécute un script et capture stdout/stderr dans le résultat.
	Exec(ctx context.Context, script string) (*ExecResult, error)

	// ExecWithStreams exécute un script avec des streams I/O fournis par l'appelant.
	ExecWithStreams(ctx context.Context, script string, stdin io.Reader, stdout, stderr io.Writer) (*ExecResult, error)

	// Registry retourne le registre de skills.
	Registry() *registry.Registry

	// Policy retourne le moteur de politique actif.
	Policy() security.PolicyEngine

	// Instructions retourne un texte au format Markdown, destiné au champ
	// "instructions" d'un serveur MCP, qui liste les commandes shell
	// effectivement disponibles pour CET Engine (builtins enregistrés +
	// binaires autorisés par sa policy). Partagé par tous les transports MCP
	// (stdio et HTTP) pour qu'un agent découvre les commandes réelles plutôt
	// que de deviner un nom (ex. "leash_help" au lieu de "leash-help") et de
	// se faire bloquer en boucle sans retour exploitable.
	Instructions() string
}

// OutputChunk représente un fragment de sortie avec son origine (stdout ou stderr).
type OutputChunk struct {
	IsStderr bool
	Data     []byte
}

// ExecResult contient le résultat d'une exécution shell.
// AuditTrail et CommandRecord sont définis dans internal/security pour éviter les cycles d'import.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	Combined []OutputChunk // stdout + stderr entrelacés dans l'ordre d'écriture
	ExitCode int
	Duration time.Duration
	Audit    *security.AuditTrail
}
