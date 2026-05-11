package shell

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

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

var heredocOpenRe = regexp.MustCompile(`(?m)^:?\s*<<\s*'?BUILTIN'?\s*$`)

func parseFrontmatter(src []byte) (frontmatterYAML, error) {
	loc := heredocOpenRe.FindIndex(src)
	if loc == nil {
		return frontmatterYAML{}, nil
	}

	afterOpen := src[loc[1]:]
	if len(afterOpen) > 0 && afterOpen[0] == '\n' {
		afterOpen = afterOpen[1:]
	}

	endIdx := -1
	pos := 0
	for pos < len(afterOpen) {
		nl := bytes.IndexByte(afterOpen[pos:], '\n')
		var line []byte
		if nl == -1 {
			line = afterOpen[pos:]
		} else {
			line = afterOpen[pos : pos+nl]
		}
		if bytes.Equal(bytes.TrimRight(line, "\r"), []byte("BUILTIN")) {
			endIdx = pos
			break
		}
		if nl == -1 {
			break
		}
		pos += nl + 1
	}
	if endIdx == -1 {
		return frontmatterYAML{}, fmt.Errorf("frontmatter: closing BUILTIN marker not found")
	}

	yamlContent := afterOpen[:endIdx]

	var meta frontmatterYAML
	if err := yaml.Unmarshal(yamlContent, &meta); err != nil {
		return frontmatterYAML{}, fmt.Errorf("frontmatter: invalid YAML: %w", err)
	}

	return meta, nil
}

func parseShebang(src []byte) string {
	firstLine, _, _ := bytes.Cut(src, []byte("\n"))
	line := strings.TrimSpace(string(firstLine))
	if !strings.HasPrefix(line, "#!") {
		return "sh"
	}
	interp := strings.TrimPrefix(line, "#!")
	interp = strings.TrimSpace(interp)

	parts := strings.Fields(interp)
	if len(parts) == 0 {
		return "sh"
	}
	if strings.HasSuffix(parts[0], "/env") && len(parts) > 1 {
		return parts[1]
	}
	p := parts[0]
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		p = p[idx+1:]
	}
	return p
}
