package sandbox

import "context"

type contextKey struct{}

// ContextWithSandbox retourne un contexte portant le sandbox donné.
func ContextWithSandbox(ctx context.Context, sb Sandbox) context.Context {
	return context.WithValue(ctx, contextKey{}, sb)
}

// SandboxFromContext extrait le sandbox du contexte.
// Retourne NewNone() si aucun sandbox n'est présent, jamais nil.
func SandboxFromContext(ctx context.Context) Sandbox {
	if sb, ok := ctx.Value(contextKey{}).(Sandbox); ok && sb != nil {
		return sb
	}
	return NewNone()
}
