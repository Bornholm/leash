// Exemple shell — skills dynamiques définis via des scripts shell.
//
// Cet exemple montre comment :
//   - charger des skills depuis un répertoire de scripts .sh
//   - utiliser des flags avec validation regexp
//   - observer le comportement en cas d'erreur de validation
//
// Lancer avec : go run ./examples/shell/
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

	skillsDir := "examples/shell/skills"

	eng, cleanup, err := leash.New(ctx,
		leash.WithMaxDuration(10*time.Second),
		leash.WithAllowedBinaries("printf", "tr"),
		leash.WithShellSkillDir(skillsDir),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur création engine: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	sep := strings.Repeat("─", 50)

	// ── 1. uppercase : conversion en majuscules ───────────────────────────────
	fmt.Println(sep)
	fmt.Println("1. uppercase — conversion en majuscules")
	fmt.Println(sep)

	result, err := eng.Exec(ctx, `echo "hello world" | uppercase`)
	printResult(result, err)

	// ── 2. uppercase avec préfixe ─────────────────────────────────────────────
	fmt.Println(sep)
	fmt.Println(`2. uppercase --prefix=">> "`)
	fmt.Println(sep)

	result, err = eng.Exec(ctx, `echo "hello world" | uppercase --prefix=">> "`)
	printResult(result, err)

	// ── 3. wrap : encadrement de lignes ───────────────────────────────────────
	fmt.Println(sep)
	fmt.Println("3. wrap — encadrement avec délimiteurs par défaut")
	fmt.Println(sep)

	result, err = eng.Exec(ctx, `printf 'a\nb\nc\n' | wrap`)
	printResult(result, err)

	// ── 4. wrap avec délimiteurs personnalisés ────────────────────────────────
	fmt.Println(sep)
	fmt.Println(`4. wrap --prefix="<" --suffix=">"`)
	fmt.Println(sep)

	result, err = eng.Exec(ctx, `printf 'x\ny\n' | wrap --prefix="<" --suffix=">"`)
	printResult(result, err)

	// ── 5. Validation du flag prefix (valeur trop longue) ─────────────────────
	fmt.Println(sep)
	fmt.Println("5. Validation regexp : prefix trop long (exit 1 attendu)")
	fmt.Println(sep)

	result, err = eng.Exec(ctx, `echo "test" | wrap --prefix="toolongprefix"`)
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
