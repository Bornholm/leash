package sandbox

import (
	"context"
	"os/exec"
)

type noneSandbox struct{}

// NewNone retourne un sandbox no-op (pas d'isolation).
func NewNone() Sandbox { return &noneSandbox{} }

func (noneSandbox) Name() string                                              { return "none" }
func (noneSandbox) Wrap(_ context.Context, cmd *exec.Cmd) (*exec.Cmd, error) { return cmd, nil }
func (noneSandbox) Close() error                                              { return nil }
func (noneSandbox) Config() Config                                            { return DefaultConfig() }
