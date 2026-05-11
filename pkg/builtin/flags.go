package builtin

import (
	"fmt"
	"strings"
)

func ParseFlags(defs []FlagDef, rawArgs []string) (positional []string, flags map[string]string, err error) {
	flags = make(map[string]string)

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
