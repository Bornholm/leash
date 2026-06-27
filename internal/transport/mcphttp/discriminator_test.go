package mcphttp

import (
	"regexp"
	"strings"
	"testing"
)

var hexHash64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestHashDiscriminator_EmptyReturnsError(t *testing.T) {
	if _, err := hashDiscriminator([]byte("secret"), ""); err != ErrEmptyDiscriminator {
		t.Fatalf("expected ErrEmptyDiscriminator, got %v", err)
	}
}

func TestHashDiscriminator_OutputIsSafeHex(t *testing.T) {
	cases := []string{
		"normal-tenant",
		"../../etc/passwd",
		"..\\..\\windows\\system32",
		"../../../../../../etc/shadow",
		"a\x00b",
		"foo\x00../../bar",
		"日本語テナント",
		"🏴‍☠️tenant",
		"....//....//etc",
		"/etc/passwd",
		"~root",
		"tenant\nwith\nnewlines",
		strings.Repeat("../", 200),
		"",
	}

	secret := []byte("test-server-secret")

	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			out, err := hashDiscriminator(secret, raw)
			if raw == "" {
				if err != ErrEmptyDiscriminator {
					t.Fatalf("expected ErrEmptyDiscriminator for empty input, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for raw=%q: %v", raw, err)
			}
			if !hexHash64.MatchString(out) {
				t.Fatalf("output %q for raw=%q does not match ^[0-9a-f]{64}$", out, raw)
			}
			if strings.Contains(out, "..") || strings.Contains(out, "/") || strings.Contains(out, "\\") {
				t.Fatalf("output %q for raw=%q contains path-traversal-relevant characters", out, raw)
			}
		})
	}
}

func TestHashDiscriminator_DifferentInputsDifferentOutputs(t *testing.T) {
	secret := []byte("secret")
	a, err := hashDiscriminator(secret, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashDiscriminator(secret, "tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("expected different hashes for different discriminators, got %q for both", a)
	}
}

func TestHashDiscriminator_SameInputSameOutput(t *testing.T) {
	secret := []byte("secret")
	a, err := hashDiscriminator(secret, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashDiscriminator(secret, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("expected deterministic output, got %q vs %q", a, b)
	}
}

func TestHashDiscriminator_DifferentSecretsDifferentOutputs(t *testing.T) {
	a, err := hashDiscriminator([]byte("secret1"), "tenant")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashDiscriminator([]byte("secret2"), "tenant")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("expected different hashes for different secrets")
	}
}
