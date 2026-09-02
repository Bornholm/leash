package mcphttp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bornholm/leash/internal/security"
	"github.com/bornholm/leash/internal/security/sandbox"
	"github.com/bornholm/leash/pkg/leash"
)

// ProductionFactory construit l'engineFactory de production.
//
// Par défaut (clé sans PolicyFile) : sandbox bubblewrap durci (C2),
// remplacement total de la policy sandbox (C3), builtins désactivés (C4).
//
// Si la clé référence un PolicyFile, ce fichier pilote binaires autorisés,
// builtins, serveurs MCP et, s'il en définit un, son propre sandbox
// (Unshare, binds additionnels, etc.) — une clé peut ainsi assouplir les
// défauts ci-dessus. Le backend bubblewrap reste néanmoins toujours forcé
// (C2 est une contrainte absolue, pas seulement un défaut), et le
// répertoire réel du workspace est toujours injecté comme bind
// lecture-écriture, quoi que dise le fichier, pour que l'isolation par
// tenant ne puisse jamais être omise par erreur.
//
// Le fichier est interpolé comme un Go template (cf. policytemplate.go)
// avant d'être chargé : {{.WorkspaceDir}} et {{.WorkspaceID}} permettent à
// un serveur MCP externe déclaré dans mcp_servers (command/env/url) de
// recevoir le vrai chemin hôte du workspace de la session qui l'invoque —
// ces serveurs stdio tournent sur l'hôte (pas dans le sandbox), donc un
// chemin interne au sandbox comme "/work" ne leur serait pas accessible.
//
// backend est le backend sandbox imposé par l'opérateur ("bwrap" ou
// "chroot") ; une valeur vide vaut "bwrap".
func ProductionFactory(backend string) engineFactory {
	return func(ctx context.Context, dir string, key *APIKeyConfig) (leash.Engine, func(), error) {
		auditOpts := []leash.Option{
			leash.WithAuditWriter(os.Stderr),
			leash.WithAuditAttrs("workspace_id", filepath.Base(dir), "api_key", key.Name),
		}

		if key.PolicyFile == "" {
			opts := append([]leash.Option{
				leash.WithSandbox(hardenedSandbox(backend, dir)),
				leash.WithBuiltinsDisabled(),
				leash.WithWorkDir(dir),
			}, auditOpts...)
			if len(key.Env) > 0 {
				opts = append(opts, leash.WithStaticEnv(key.Env))
			}
			return leash.New(ctx, opts...)
		}

		resolvedPath, err := writeResolvedPolicyFile(key.PolicyFile, dir)
		if err != nil {
			return nil, nil, fmt.Errorf("mcphttp: resolving policy file %q: %w", key.PolicyFile, err)
		}

		polCfg, err := security.LoadPolicyConfig(resolvedPath)
		if err != nil {
			_ = os.Remove(resolvedPath)
			return nil, nil, fmt.Errorf("mcphttp: loading resolved policy file: %w", err)
		}

		opts := append([]leash.Option{
			leash.WithPolicyFile(resolvedPath),
			leash.WithSandbox(workspaceSandbox(backend, polCfg.Sandbox, dir)),
			leash.WithWorkDir(dir),
		}, auditOpts...)
		if len(key.Env) > 0 {
			opts = append(opts, leash.WithStaticEnv(key.Env))
		}

		eng, cleanup, err := leash.New(ctx, opts...)
		if err != nil {
			_ = os.Remove(resolvedPath)
			return nil, nil, err
		}

		return eng, func() {
			cleanup()
			_ = os.Remove(resolvedPath)
		}, nil
	}
}

// writeResolvedPolicyFile interpole policyFile avec le répertoire réel du
// workspace et écrit le résultat dans un fichier temporaire HORS de dir :
// dir est bind-monté dans le sandbox du tenant, donc y écrire le fichier
// résolu (qui peut contenir des secrets de serveurs MCP, des env vars,
// etc.) le rendrait visible depuis le script shell de l'utilisateur. Le
// fichier temporaire est nettoyé par l'appelant (cleanup de l'Engine ou
// chemin d'erreur).
func writeResolvedPolicyFile(policyFile, dir string) (string, error) {
	raw, err := os.ReadFile(policyFile)
	if err != nil {
		return "", fmt.Errorf("reading: %w", err)
	}

	rendered, err := renderPolicyTemplate(raw, filepath.Base(policyFile), policyTemplateData{
		WorkspaceDir: dir,
		WorkspaceID:  filepath.Base(dir),
	})
	if err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp("", "leash-policy-*.yaml")
	if err != nil {
		return "", fmt.Errorf("creating resolved policy file: %w", err)
	}

	if _, err := tmp.Write(rendered); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("writing resolved policy file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("closing resolved policy file: %w", err)
	}

	return tmp.Name(), nil
}

// hardenedSandbox produit la configuration sandbox la plus sécurisée
// possible pour un workspace donné (C2) :
//   - backend bwrap toujours actif
//   - réseau, PID, IPC, UTS, user namespaces isolés
//   - un seul montage en lecture-écriture : dir, monté sur /work
//   - le process meurt si le parent meurt (pas de processus orphelin)
func hardenedSandbox(backend, dir string) sandbox.Config {
	return forceBackend(sandbox.Config{
		Enabled: true,
		Workdir: "/work",
		ReadonlyBinds: []string{
			"/usr", "/bin", "/lib", "/lib64", "/etc",
		},
		ReadwriteBinds: []sandbox.BindMount{
			{Source: dir, Target: "/work"},
		},
		Unshare: sandbox.Unshare{
			Network: true,
			PID:     true,
			IPC:     true,
			UTS:     true,
			User:    true,
		},
		DieWithParent: true,
	}, backend, dir)
}

// workspaceSandbox part de la configuration sandbox d'un fichier de policy
// par clé et y injecte le répertoire réel du workspace comme bind
// lecture-écriture, en forçant le backend bwrap (C2). Le reste (Unshare,
// binds additionnels, tmpfs, etc.) provient intégralement du fichier,
// permettant par exemple à une clé d'autoriser le réseau pour joindre des
// serveurs MCP distants.
func workspaceSandbox(backend string, cfg sandbox.Config, dir string) sandbox.Config {
	out := cfg
	out.Enabled = true
	if out.Workdir == "" {
		out.Workdir = "/work"
	}

	binds := make([]sandbox.BindMount, len(out.ReadwriteBinds), len(out.ReadwriteBinds)+1)
	copy(binds, out.ReadwriteBinds)
	out.ReadwriteBinds = append(binds, sandbox.BindMount{Source: dir, Target: out.Workdir})

	return forceBackend(out, backend, dir)
}

// forceBackend impose le backend choisi par l'opérateur du serveur, quelle
// que soit la valeur présente dans un fichier de policy par clé.
//
// Le backend chroot est un REPLI d'exploitation : il ne sait ni monter de
// bind, ni couper le réseau. Quand il est retenu, la frontière de sécurité
// effective est le conteneur qui héberge le serveur, et le répertoire réel
// du workspace devient directement le répertoire de travail du processus.
// Voir docs/mcp-http.md, section « Sandbox backend ».
func forceBackend(cfg sandbox.Config, backend, dir string) sandbox.Config {
	out := cfg
	out.Enabled = true
	switch backend {
	case SandboxBackendChroot:
		out.Backend = SandboxBackendChroot
		if out.Rootfs == "" {
			out.Rootfs = "/"
		}
		// Sans bind mount, /work n'existe pas : le processus travaille
		// directement dans le répertoire hôte du workspace.
		out.Workdir = dir
		out.ReadwriteBinds = nil
		out.ReadonlyBinds = nil
	default:
		out.Backend = SandboxBackendBwrap
		if out.Workdir == "" {
			out.Workdir = "/work"
		}
	}
	return out
}
