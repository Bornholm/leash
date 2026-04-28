// Exemple tengo — skills dynamiques définis via des scripts Tengo.
//
// Cet exemple montre comment :
//   - charger des skills depuis un répertoire de scripts .tengo
//   - utiliser des args et flags avec validation regexp
//   - observer le comportement en cas d'erreur de validation
//
// Lancer avec : go run ./examples/tengo/
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bornholm/leash/pkg/leash"
)

func main() {
	ctx := context.Background()

	skillsDir := "examples/tengo/skills"

	eng, cleanup, err := leash.New(ctx,
		leash.WithMaxDuration(10*time.Second),
		leash.WithAllowedBinaries("echo", "ls", "cat"),
		leash.WithTengoSkillDir(skillsDir),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur création engine: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	sep := strings.Repeat("─", 50)

	// ── 1. slugify : conversion en slug ─────────────────────────────────────
	fmt.Println(sep)
	fmt.Println("1. slugify — conversion en URL slug")
	fmt.Println(sep)

	result, err := eng.Exec(ctx, `slugify "Hello, World! This is Leash."`)
	printResult(result, err)

	// ── 2. slugify avec séparateur personnalisé ──────────────────────────────
	fmt.Println(sep)
	fmt.Println("2. slugify --separator=_")
	fmt.Println(sep)

	result, err = eng.Exec(ctx, `slugify --separator=_ "Hello World"`)
	printResult(result, err)

	// ── 3. Validation du flag separator (valeur invalide) ────────────────────
	fmt.Println(sep)
	fmt.Println("3. Validation regexp : séparateur invalide (exit 1 attendu)")
	fmt.Println(sep)

	result, err = eng.Exec(ctx, `slugify --separator=abc "Hello World"`)
	printResult(result, err)

	// ── 4. repeat : répéter stdin ────────────────────────────────────────────
	fmt.Println(sep)
	fmt.Println("4. repeat — répétition de stdin")
	fmt.Println(sep)

	result, err = eng.Exec(ctx, `echo "leash" | repeat 3`)
	printResult(result, err)

// ── 5. Validation de l'arg count (valeur invalide) ───────────────────────
	fmt.Println(sep)
	fmt.Println("5. Validation regexp : count=0 invalide (pattern ^[1-9][0-9]*$)")
	fmt.Println(sep)

	result, err = eng.Exec(ctx, `echo "leash" | repeat 0`)

	printResult(result, err)

	fmt.Println(sep)
}

func printResult(result *leash.ExecResult, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur: %v\n", err)
		return
	}
	if len(result.Stdout) > 0 {
		fmt.Printf("stdout : %s", result.Stdout)
	}
	if len(result.Stderr) > 0 {
		fmt.Printf("stderr : %s", result.Stderr)
	}
	fmt.Printf("exit   : %d\n", result.ExitCode)
}
