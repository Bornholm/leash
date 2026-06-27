package mcphttp

import (
	"bytes"
	"fmt"
	"text/template"
)

// policyTemplateData expose les valeurs interpolables dans un fichier de
// policy par clé (LEASH_APIKEY_<NAME>_POLICY). Cela permet à des serveurs
// MCP externes déclarés statiquement dans le fichier (command/env/url) de
// recevoir, à l'exécution, le vrai chemin hôte du workspace de la session
// qui les invoque — par exemple pour leur donner accès en lecture/écriture
// au même répertoire que celui vu par le script shell sandboxé.
type policyTemplateData struct {
	// WorkspaceDir est le chemin hôte réel du répertoire de ce workspace :
	// le même répertoire que celui bind-monté en lecture-écriture dans le
	// sandbox bubblewrap de la session (cf. workspaceSandbox).
	WorkspaceDir string
	// WorkspaceID est le hash hex du discriminant identifiant ce workspace.
	WorkspaceID string
}

// renderPolicyTemplate interpole raw (le contenu d'un fichier de policy
// YAML) avec data, via la syntaxe Go template ({{.WorkspaceDir}}, etc.).
// "missingkey=error" fait échouer le rendu si un champ référencé n'existe
// pas dans policyTemplateData, plutôt que de produire silencieusement
// "<no value>" dans le YAML résultant.
func renderPolicyTemplate(raw []byte, name string, data policyTemplateData) ([]byte, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing policy file as template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering policy file template: %w", err)
	}

	return buf.Bytes(), nil
}
