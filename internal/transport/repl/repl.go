package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bornholm/leash/internal/engine"
)

// ErrQuit signale que l'utilisateur souhaite quitter.
var ErrQuit = errors.New("quit")

// HistoryEntry enregistre une entrée de l'historique de session.
type HistoryEntry struct {
	Input  string
	Result *engine.ExecResult
	Time   time.Time
}

// REPL est le shell interactif de LeaSH.
type REPL struct {
	eng        engine.Engine
	policyName string
	history    []HistoryEntry
	scanner    *bufio.Scanner
	out        io.Writer
	lastAudit  *engine.ExecResult
}

// New crée un REPL.
func New(eng engine.Engine, policyName string) *REPL {
	return &REPL{
		eng:        eng,
		policyName: policyName,
		scanner:    bufio.NewScanner(os.Stdin),
		out:        os.Stdout,
	}
}

// Run démarre la boucle interactive.
func (r *REPL) Run() error {
	fmt.Fprintf(r.out, "LeaSH — policy: %s\nType :help for commands, :quit to exit.\n\n", r.policyName)

	for {
		fmt.Fprintf(r.out, "[%s] leash> ", r.policyName)

		if !r.scanner.Scan() {
			break
		}
		line := strings.TrimSpace(r.scanner.Text())
		if line == "" {
			continue
		}

		if err := r.dispatch(line); err != nil {
			if errors.Is(err, ErrQuit) {
				fmt.Fprintln(r.out, "Bye.")
				return nil
			}
			fmt.Fprintf(r.out, "error: %v\n", err)
		}
	}
	return r.scanner.Err()
}

func (r *REPL) dispatch(line string) error {
	if !strings.HasPrefix(line, ":") {
		return r.execScript(line)
	}

	parts := strings.Fields(line[1:])
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "quit", "q", "exit":
		return ErrQuit
	case "help":
		r.printHelp()
	case "commands":
		fmt.Fprintln(r.out, r.eng.Registry().GenerateManifest())
	case "history":
		r.printHistory()
	case "audit":
		r.printLastAudit()
	case "policy":
		r.switchPolicy(parts[1:])
	default:
		fmt.Fprintf(r.out, "unknown meta-command: :%s\n", parts[0])
	}
	return nil
}

func (r *REPL) execScript(script string) error {
	result, err := r.eng.Exec(context.Background(), script)
	if err != nil {
		return err
	}

	r.lastAudit = result

	if len(result.Stdout) > 0 {
		fmt.Fprint(r.out, string(result.Stdout))
		if !strings.HasSuffix(string(result.Stdout), "\n") {
			fmt.Fprintln(r.out)
		}
	}
	if len(result.Stderr) > 0 {
		fmt.Fprint(r.out, string(result.Stderr))
	}
	if result.ExitCode != 0 {
		fmt.Fprintf(r.out, "[exit %d]\n", result.ExitCode)
	}

	r.history = append(r.history, HistoryEntry{
		Input:  script,
		Result: result,
		Time:   time.Now(),
	})
	return nil
}

func (r *REPL) printHelp() {
	fmt.Fprintln(r.out, `Meta-commands:
  :help               Show this help
  :commands           List available skills
  :history            Show session history
  :audit              Show last execution audit trail
  :policy <name>      Switch policy (reload required)
  :quit               Exit LeaSH`)
}

func (r *REPL) printHistory() {
	if len(r.history) == 0 {
		fmt.Fprintln(r.out, "No history yet.")
		return
	}
	for i, entry := range r.history {
		exitCode := 0
		if entry.Result != nil {
			exitCode = entry.Result.ExitCode
		}
		fmt.Fprintf(r.out, "[%d] %s  [exit:%d] (%s)\n",
			i+1,
			entry.Input,
			exitCode,
			entry.Time.Format("15:04:05"),
		)
	}
}

func (r *REPL) printLastAudit() {
	if r.lastAudit == nil || r.lastAudit.Audit == nil {
		fmt.Fprintln(r.out, "No audit data available.")
		return
	}
	trail := r.lastAudit.Audit
	fmt.Fprintf(r.out, "Audit trail (%d commands):\n", len(trail.Commands))
	for _, cmd := range trail.Commands {
		status := "ok"
		if cmd.Blocked {
			status = "BLOCKED: " + cmd.Reason
		}
		fmt.Fprintf(r.out, "  %-20s %v [%s]\n", cmd.Command, cmd.Duration.Round(time.Millisecond), status)
	}
}

func (r *REPL) switchPolicy(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(r.out, "current policy: %s\n", r.policyName)
		return
	}
	r.policyName = args[0]
	fmt.Fprintf(r.out, "policy switched to: %s (note: requires restart to reload policy file)\n", r.policyName)
}
