package builtin_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/bornholm/leash/pkg/builtin"
)

func makeBuiltinWithPatterns() *builtin.Builtin {
	return builtin.New("test").
		Arg("count", "A number", true).ArgPattern(`^\d+$`).
		Arg("name", "A name", false).ArgPattern(`^[a-z]+$`).
		Flag("format", "f", "text", "Output format").FlagPattern(`^(text|json|csv)$`).
		Flag("label", "l", "", "Label (no restriction)").
		Handle(func(_ context.Context, _ *builtin.Call) error { return nil })
}

func makeCall(args []string, flags map[string]string) *builtin.Call {
	return &builtin.Call{
		Args:   args,
		Flags:  flags,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Env:    func(string) string { return "" },
	}
}

func TestValidate_OK(t *testing.T) {
	sk := makeBuiltinWithPatterns()
	call := makeCall([]string{"42", "alice"}, map[string]string{"format": "json", "label": "anything"})
	if err := builtin.Validate(sk, call); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidate_ArgFails(t *testing.T) {
	sk := makeBuiltinWithPatterns()
	call := makeCall([]string{"not-a-number"}, map[string]string{"format": "text"})
	err := builtin.Validate(sk, call)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), `"count"`) || !strings.Contains(err.Error(), `"not-a-number"`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidate_FlagFails(t *testing.T) {
	sk := makeBuiltinWithPatterns()
	call := makeCall([]string{"42"}, map[string]string{"format": "xml"})
	err := builtin.Validate(sk, call)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), `"format"`) || !strings.Contains(err.Error(), `"xml"`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidate_NoPattern_AlwaysOK(t *testing.T) {
	sk := builtin.New("bare").
		Arg("x", "anything", true).
		Flag("y", "y", "", "anything").
		Handle(func(_ context.Context, _ *builtin.Call) error { return nil })

	call := makeCall([]string{"whatever 123 !@#"}, map[string]string{"y": "whatever 123 !@#"})
	if err := builtin.Validate(sk, call); err != nil {
		t.Errorf("expected no error without patterns, got: %v", err)
	}
}

func TestValidate_FlagAbsent_Skipped(t *testing.T) {
	sk := makeBuiltinWithPatterns()
	call := makeCall([]string{"42"}, map[string]string{})
	if err := builtin.Validate(sk, call); err != nil {
		t.Errorf("expected no error for absent flag, got: %v", err)
	}
}

func TestValidate_SecondArgFails(t *testing.T) {
	sk := makeBuiltinWithPatterns()
	call := makeCall([]string{"42", "alice123"}, map[string]string{"format": "text"})
	err := builtin.Validate(sk, call)
	if err == nil {
		t.Fatal("expected validation error for second arg, got nil")
	}
	if !strings.Contains(err.Error(), `"name"`) {
		t.Errorf("error should mention arg name: %v", err)
	}
}
