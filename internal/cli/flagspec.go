package cli

import (
	"reflect"
)

// FlagSpec describes one CLI flag. Specs for the shared run flags are derived
// by reflection from the Flags struct, so completion and the command-boundary
// inference in splitCommand read the parser's own definition and cannot drift
// from it.
type FlagSpec struct {
	Name        string // long name, without dashes
	Short       string // single-letter form, without dash
	TakesValue  bool   // consumes the following argument when not --flag=value
	Help        string
	Placeholder string // value placeholder shown in help, e.g. PATH
	// Complete names what the flag's value completes to: "profile" for
	// profile names, "file" to delegate to the shell's file completion, or
	// empty for values with no candidates.
	Complete string
}

// CommandSpec describes one subcommand, for dispatch and completion alike.
type CommandSpec struct {
	Name   string
	Verbs  []string   // fixed verb positionals, such as scratch list|diff|...
	Extra  []FlagSpec // subcommand-specific flags beyond the shared run flags
	Hidden bool       // internal commands, excluded from completion
}

// GlobalFlags returns the run-flag specs derived from the Flags struct.
func GlobalFlags() []FlagSpec {
	return flagSpecsOf(reflect.TypeOf(Flags{}))
}

func flagSpecsOf(t reflect.Type) []FlagSpec {
	var specs []FlagSpec
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			specs = append(specs, flagSpecsOf(field.Type)...)
			continue
		}
		name := field.Tag.Get("name")
		if name == "" || field.Tag.Get("arg") != "" {
			continue
		}
		specs = append(specs, FlagSpec{
			Name:        name,
			Short:       field.Tag.Get("short"),
			TakesValue:  field.Type.Kind() != reflect.Bool,
			Help:        field.Tag.Get("help"),
			Placeholder: field.Tag.Get("placeholder"),
			Complete:    field.Tag.Get("complete"),
		})
	}
	return specs
}
