package skill

import (
	"context"
	"fmt"
	"io"
)

// HandlerFunc est la signature de toute implémentation de skill.
// Retourner ExitError pour signaler un exit code non-zéro sans erreur système.
type HandlerFunc func(ctx context.Context, call *Call) error

// ExitError permet à un skill de signaler un exit code non-zéro.
type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// Skill décrit une commande virtuelle disponible dans le shell.
type Skill struct {
	Name        string
	Description string
	Usage       string
	Category    string
	Args        []ArgDef
	Flags       []FlagDef
	Examples    []Example
	Handler     HandlerFunc
	RateLimit   int // requêtes/minute, 0 = illimité
}

// Call représente un appel à un skill avec ses entrées/sorties.
type Call struct {
	Args  []string
	Flags map[string]string
	Stdin io.Reader
	// Stdout et Stderr sont câblés sur les pipes shell actifs par mvdan.cc/sh/v3.
	// Ne pas les remplacer par les streams de l'Engine.
	Stdout io.Writer
	Stderr io.Writer
	// Env retourne la valeur d'une variable d'environnement du script en cours.
	// Utiliser une fonction évite de copier tout l'environnement à chaque appel.
	Env func(string) string
}

// ArgDef décrit un argument positionnel d'un skill.
type ArgDef struct {
	Name        string
	Description string
	Required    bool
}

// FlagDef décrit un flag optionnel d'un skill.
type FlagDef struct {
	Name        string
	Short       string
	Default     string
	Description string
}

// Example illustre un cas d'usage d'un skill.
type Example struct {
	Title   string
	Command string
}
