// Exemple basic — utilisation de LeaSH comme librairie.
//
// Cet exemple montre comment :
//   - créer un Engine via l'API OptionFunc
//   - enregistrer des skills personnalisés
//   - exécuter des scripts shell sandboxés
//   - lire l'audit trail
//   - observer le comportement face aux commandes bloquées
//
// Lancer avec : go run ./examples/basic/
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bornholm/leash/pkg/leash"
	"github.com/bornholm/leash/pkg/skill"
)

func main() {
	ctx := context.Background()

	eng, cleanup, err := leash.New(ctx,
		// Limites d'exécution
		leash.WithMaxDuration(10*time.Second),
		leash.WithMaxCommandsPerScript(20),

		// Binaires système autorisés
		leash.WithAllowedBinaries("echo", "grep", "tr", "wc", "sort", "head"),

		// Environnement injecté dans chaque exécution
		leash.WithEnvVar("APP_ENV", "sandbox"),

		// Skills personnalisés
		leash.WithSkill(newWordCountSkill()),
		leash.WithSkill(newUppercaseSkill()),

		// Audit vers stderr
		leash.WithAuditWriter(os.Stderr),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur création engine: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	separator := strings.Repeat("─", 50)

	// ── 1. Script simple avec binaire autorisé ──────────────────────────────
	fmt.Println(separator)
	fmt.Println("1. Binaire autorisé : echo")
	fmt.Println(separator)

	result, err := eng.Exec(ctx, `echo "Environnement : $APP_ENV"`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur: %v\n", err)
	} else {
		fmt.Printf("stdout : %s", result.Stdout)
		fmt.Printf("exit   : %d\n", result.ExitCode)
	}

	// ── 2. Skill personnalisé : word-count ───────────────────────────────────
	fmt.Println(separator)
	fmt.Println("2. Skill personnalisé : word-count")
	fmt.Println(separator)

	script := `echo "the quick brown fox jumps over the lazy dog" | word-count`
	result, err = eng.Exec(ctx, script)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur: %v\n", err)
	} else {
		fmt.Printf("stdout : %s", result.Stdout)
		fmt.Printf("exit   : %d\n", result.ExitCode)
	}

	// ── 3. Skill avec flag : uppercase ──────────────────────────────────────
	fmt.Println(separator)
	fmt.Println("3. Skill avec flag : uppercase --prefix")
	fmt.Println(separator)

	result, err = eng.Exec(ctx, `echo "hello world" | uppercase --prefix=">>> "`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur: %v\n", err)
	} else {
		fmt.Printf("stdout : %s", result.Stdout)
		fmt.Printf("exit   : %d\n", result.ExitCode)
	}

	// ── 4. Pipeline multi-étapes ─────────────────────────────────────────────
	fmt.Println(separator)
	fmt.Println("4. Pipeline : echo | tr | word-count")
	fmt.Println(separator)

	script = `
		echo "one two three four five" \
		| tr ' ' '\n' \
		| sort \
		| word-count
	`
	result, err = eng.Exec(ctx, script)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur: %v\n", err)
	} else {
		fmt.Printf("stdout : %s", result.Stdout)
		fmt.Printf("exit   : %d\n", result.ExitCode)
		fmt.Printf("durée  : %s\n", result.Duration.Round(time.Millisecond))
	}

	// ── 5. Audit trail ───────────────────────────────────────────────────────
	fmt.Println(separator)
	fmt.Println("5. Lecture de l'audit trail")
	fmt.Println(separator)

	result, err = eng.Exec(ctx, `echo foo | word-count`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur: %v\n", err)
	} else if result.Audit != nil {
		for _, cmd := range result.Audit.Commands {
			status := "OK"
			if cmd.Blocked {
				status = "BLOQUÉ (" + cmd.Reason + ")"
			}
			fmt.Printf("  %-15s args=%-20v exit=%-3d skill=%-5v %s\n",
				cmd.Command,
				cmd.Args,
				cmd.ExitCode,
				cmd.IsSkill,
				status,
			)
		}
	}

	// ── 6. Commande bloquée ──────────────────────────────────────────────────
	fmt.Println(separator)
	fmt.Println("6. Commande bloquée (cat non autorisé)")
	fmt.Println(separator)

	result, err = eng.Exec(ctx, `cat /etc/hostname`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erreur: %v\n", err)
	} else {
		fmt.Printf("exit : %d (127 = commande non trouvée / bloquée)\n", result.ExitCode)
		if result.Audit != nil {
			for _, cmd := range result.Audit.Commands {
				if cmd.Blocked {
					fmt.Printf("audit: %q bloqué — %s\n", cmd.Command, cmd.Reason)
				}
			}
		}
	}

	// ── 7. Pattern bloqué ────────────────────────────────────────────────────
	fmt.Println(separator)
	fmt.Println("7. Pattern bloqué (rm -rf détecté avant parsing)")
	fmt.Println(separator)

	_, err = eng.Exec(ctx, `echo "rm -rf /"`)
	if err != nil {
		fmt.Printf("erreur attendue : %v\n", err)
	}

	fmt.Println(separator)
}

// newWordCountSkill crée un skill qui compte les mots lus sur stdin.
func newWordCountSkill() *skill.Skill {
	return skill.New("word-count").
		Description("Compte le nombre de mots lus sur stdin").
		Category("text").
		Example("Compter les mots d'une phrase", "echo 'foo bar baz' | word-count").
		Handle(func(ctx context.Context, c *skill.Call) error {
			data, err := io.ReadAll(c.Stdin)
			if err != nil {
				return err
			}
			words := strings.Fields(string(data))
			fmt.Fprintf(c.Stdout, "%d\n", len(words))
			return nil
		})
}

// newUppercaseSkill crée un skill qui convertit stdin en majuscules,
// avec un flag optionnel --prefix.
func newUppercaseSkill() *skill.Skill {
	return skill.New("uppercase").
		Description("Convertit stdin en majuscules").
		Category("text").
		Flag("prefix", "p", "", "Préfixe à ajouter devant la sortie").
		Example("Majuscules simples", "echo 'hello' | uppercase").
		Example("Avec préfixe", "echo 'hello' | uppercase --prefix='>> '").
		Handle(func(ctx context.Context, c *skill.Call) error {
			data, err := io.ReadAll(c.Stdin)
			if err != nil {
				return err
			}
			prefix := c.Flags["prefix"]
			fmt.Fprintf(c.Stdout, "%s%s\n", prefix, strings.ToUpper(strings.TrimRight(string(data), "\n")))
			return nil
		})
}
