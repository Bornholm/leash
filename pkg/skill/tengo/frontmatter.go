package tengo

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

type frontmatterYAML struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Usage       string        `yaml:"usage"`
	Category    string        `yaml:"category"`
	RateLimit   int           `yaml:"rate_limit"`
	Args        []argYAML     `yaml:"args"`
	Flags       []flagYAML    `yaml:"flags"`
	Examples    []exampleYAML `yaml:"examples"`
}

type argYAML struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Pattern     string `yaml:"pattern"`
}

type flagYAML struct {
	Name        string `yaml:"name"`
	Short       string `yaml:"short"`
	Default     string `yaml:"default"`
	Description string `yaml:"description"`
	Pattern     string `yaml:"pattern"`
}

type exampleYAML struct {
	Title   string `yaml:"title"`
	Command string `yaml:"command"`
}

// parseFrontmatter extrait le bloc de métadonnées YAML d'un commentaire /* skill ... */
// et retourne les métadonnées ainsi que le corps du script sans le commentaire.
// Si aucun bloc de frontmatter n'est trouvé, les métadonnées sont vides et le corps
// est le script entier.
func parseFrontmatter(src []byte) (frontmatterYAML, []byte, error) {
	prefix := []byte("/* skill\n")
	suffix := []byte("*/")

	start := bytes.Index(src, prefix)
	if start == -1 {
		return frontmatterYAML{}, src, nil
	}

	endRel := bytes.Index(src[start+len(prefix):], suffix)
	if endRel == -1 {
		return frontmatterYAML{}, nil, fmt.Errorf("frontmatter: closing */ not found")
	}
	end := start + len(prefix) + endRel

	yamlContent := src[start+len(prefix) : end]
	body := bytes.TrimSpace(src[end+len(suffix):])

	var meta frontmatterYAML
	if err := yaml.Unmarshal(yamlContent, &meta); err != nil {
		return frontmatterYAML{}, nil, fmt.Errorf("frontmatter: invalid YAML: %w", err)
	}

	return meta, body, nil
}
