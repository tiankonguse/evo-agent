package skills

// Provider satisfies prompt.SkillsProvider via structural typing.
// It delegates to the package-level functions.
type Provider struct{}

func (Provider) Catalog() string      { return Catalog() }
func (Provider) SlashNames() []string { return SlashNames() }
