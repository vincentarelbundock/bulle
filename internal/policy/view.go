package policy

import "sort"

func NewView(p Policy) View {
	network := p.Network
	if network == "" {
		network = NetworkFull
	}
	envKeys := make([]string, 0, len(p.Env))
	for key := range p.Env {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	return View{
		Backend:       p.Backend,
		ProjectPath:   p.ProjectPath,
		Command:       append([]string{}, p.Command...),
		ReadOnly:      append([]string{}, p.ReadOnly...),
		ReadOnlyExec:  append([]string{}, p.ReadOnlyExec...),
		ReadWrite:     append([]string{}, p.ReadWrite...),
		ReadWriteExec: append([]string{}, p.ReadWriteExec...),
		EnvKeys:       envKeys,
		AddExec:       p.AddExec,
		AddLibs:       p.AddLibs,
		MachLookup:    append([]string{}, p.MachLookup...),
		Network:       network,
	}
}
