package skill

// Builder construit un Skill via une API fluente.
type Builder struct {
	s Skill
}

// New crée un Builder pour un skill avec le nom donné.
func New(name string) *Builder {
	return &Builder{s: Skill{Name: name}}
}

func (b *Builder) Description(d string) *Builder {
	b.s.Description = d
	return b
}

func (b *Builder) Usage(u string) *Builder {
	b.s.Usage = u
	return b
}

func (b *Builder) Category(c string) *Builder {
	b.s.Category = c
	return b
}

// Arg ajoute un argument positionnel.
func (b *Builder) Arg(name, desc string, required bool) *Builder {
	b.s.Args = append(b.s.Args, ArgDef{Name: name, Description: desc, Required: required})
	return b
}

// Flag ajoute un flag optionnel avec sa valeur par défaut.
func (b *Builder) Flag(name, short, defaultVal, desc string) *Builder {
	b.s.Flags = append(b.s.Flags, FlagDef{Name: name, Short: short, Default: defaultVal, Description: desc})
	return b
}

// Example ajoute un exemple d'usage.
func (b *Builder) Example(title, command string) *Builder {
	b.s.Examples = append(b.s.Examples, Example{Title: title, Command: command})
	return b
}

// RateLimit fixe la limite de débit en requêtes/minute.
func (b *Builder) RateLimit(rpm int) *Builder {
	b.s.RateLimit = rpm
	return b
}

// Handle attache le handler et retourne le Skill construit.
func (b *Builder) Handle(fn HandlerFunc) *Skill {
	b.s.Handler = fn
	return &b.s
}
