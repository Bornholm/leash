package builtin

type Builder struct {
	b Builtin
}

func New(name string) *Builder {
	return &Builder{b: Builtin{Name: name}}
}

func (b *Builder) Description(d string) *Builder {
	b.b.Description = d
	return b
}

func (b *Builder) Usage(u string) *Builder {
	b.b.Usage = u
	return b
}

func (b *Builder) Category(c string) *Builder {
	b.b.Category = c
	return b
}

func (b *Builder) Arg(name, desc string, required bool) *Builder {
	b.b.Args = append(b.b.Args, ArgDef{Name: name, Description: desc, Required: required})
	return b
}

func (b *Builder) ArgPattern(pattern string) *Builder {
	if len(b.b.Args) > 0 {
		b.b.Args[len(b.b.Args)-1].Pattern = pattern
	}
	return b
}

func (b *Builder) Flag(name, short, defaultVal, desc string) *Builder {
	b.b.Flags = append(b.b.Flags, FlagDef{Name: name, Short: short, Default: defaultVal, Description: desc})
	return b
}

func (b *Builder) FlagPattern(pattern string) *Builder {
	if len(b.b.Flags) > 0 {
		b.b.Flags[len(b.b.Flags)-1].Pattern = pattern
	}
	return b
}

func (b *Builder) Example(title, command string) *Builder {
	b.b.Examples = append(b.b.Examples, Example{Title: title, Command: command})
	return b
}

func (b *Builder) RateLimit(rpm int) *Builder {
	b.b.RateLimit = rpm
	return b
}

func (b *Builder) Handle(fn HandlerFunc) *Builtin {
	b.b.Handler = fn
	return &b.b
}
