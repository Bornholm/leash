package skill

import (
	"fmt"
	"strings"
)

// ParseFlags sépare les arguments positionnels des flags --key=value, --key value, ou -k value.
// Le shell (mvdan.cc/sh/v3) a déjà splité les arguments et résolu les guillemets.
func ParseFlags(defs []FlagDef, rawArgs []string) (positional []string, flags map[string]string, err error) {
	flags = make(map[string]string)

	// Initialiser avec les valeurs par défaut (y compris les défauts vides)
	// afin que tous les flags soient présents dans la map avec leur valeur par défaut.
	// Cela garantit que dans les skills Tengo, `flags["key"]` retourne toujours
	// une chaîne (éventuellement vide) et jamais `undefined`.
	for _, def := range defs {
		flags[def.Name] = def.Default
	}

	i := 0
	for i < len(rawArgs) {
		arg := rawArgs[i]

		if strings.HasPrefix(arg, "--") {
			key, val, hasVal := strings.Cut(arg[2:], "=")
			if hasVal {
				flags[key] = val
			} else if i+1 < len(rawArgs) && !strings.HasPrefix(rawArgs[i+1], "-") {
				flags[key] = rawArgs[i+1]
				i++
			} else {
				flags[key] = "true"
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) == 2 {
			short := string(arg[1])
			name := shortToName(defs, short)
			if name == "" {
				return nil, nil, fmt.Errorf("unknown flag: -%s", short)
			}
			if i+1 < len(rawArgs) && !strings.HasPrefix(rawArgs[i+1], "-") {
				flags[name] = rawArgs[i+1]
				i++
			} else {
				flags[name] = "true"
			}
		} else {
			positional = append(positional, arg)
		}
		i++
	}

	return positional, flags, nil
}

func shortToName(defs []FlagDef, short string) string {
	for _, def := range defs {
		if def.Short == short {
			return def.Name
		}
	}
	return ""
}
