package sandbox

import "context"

type contextKey struct{}
type tmpDirKey struct{}

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

// ContextWithTmpDir retourne un contexte portant le chemin du répertoire tmp partagé.
func ContextWithTmpDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, tmpDirKey{}, dir)
}

// TmpDirFromContext extrait le chemin du répertoire tmp partagé du contexte.
// Retourne une chaîne vide si non présent.
func TmpDirFromContext(ctx context.Context) string {
	if dir, ok := ctx.Value(tmpDirKey{}).(string); ok {
		return dir
	}
	return ""
}
