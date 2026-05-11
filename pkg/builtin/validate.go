package builtin

import (
	"fmt"
	"regexp"
)

func Validate(sk *Builtin, call *Call) error {
	for i, val := range call.Args {
		if i >= len(sk.Args) {
			break
		}
		def := sk.Args[i]
		if def.Pattern == "" {
			continue
		}
		re, err := regexp.Compile(def.Pattern)
		if err != nil {
			continue
		}
		if !re.MatchString(val) {
			return fmt.Errorf("argument %q (pos %d): value %q does not match pattern %q",
				def.Name, i+1, val, def.Pattern)
		}
	}

	for _, def := range sk.Flags {
		if def.Pattern == "" {
			continue
		}
		val, ok := call.Flags[def.Name]
		if !ok {
			continue
		}
		re, err := regexp.Compile(def.Pattern)
		if err != nil {
			continue
		}
		if !re.MatchString(val) {
			return fmt.Errorf("flag %q: value %q does not match pattern %q",
				def.Name, val, def.Pattern)
		}
	}

	return nil
}
