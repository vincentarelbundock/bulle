//go:build !linux && !darwin

package record

import (
	"github.com/vincentarelbundock/bulle/internal/policy"
)

type Probe struct{}

func StartProbe(policy.Policy) Probe { return Probe{} }

func (Probe) Hints() []string { return nil }

func (Probe) Grants() []ObservedGrant { return nil }
