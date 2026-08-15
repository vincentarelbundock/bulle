package policy

import (
	"sort"

	"github.com/vincentarelbundock/bulle/internal/limits"
)

func NewView(p Policy) View {
	return newView(p, limits.Current())
}

// NewViewWithSupport builds the view against an explicit support report, so a
// caller that has already probed the machine reuses that result instead of
// probing again and risking a different answer.
func NewViewWithSupport(p Policy, support limits.Support) View {
	return newView(p, support)
}

func newView(p Policy, support limits.Support) View {
	network := p.Network
	if network == "" {
		network = NetworkFull
	}
	timeout := ""
	if p.Timeout > 0 {
		timeout = p.Timeout.String()
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
		Timeout:       timeout,
		ReadOnly:      append([]string{}, p.ReadOnly...),
		ReadOnlyExec:  append([]string{}, p.ReadOnlyExec...),
		ReadWrite:     append([]string{}, p.ReadWrite...),
		ReadWriteExec: append([]string{}, p.ReadWriteExec...),
		EnvKeys:       envKeys,
		AddExec:       p.AddExec,
		AddLibs:       p.AddLibs,
		MachLookup:    append([]string{}, p.MachLookup...),
		Network:       network,
		Limits:        limits.Plan(p.Limits, support),
	}
}
