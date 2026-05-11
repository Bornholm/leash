package security

import "time"

type AuditTrail struct {
	Commands []CommandRecord
}

type CommandRecord struct {
	Command   string
	Args      []string
	StartTime time.Time
	Duration  time.Duration
	ExitCode  int
	Blocked   bool
	Reason    string
	IsBuiltin bool
	SandboxBackend string
}
