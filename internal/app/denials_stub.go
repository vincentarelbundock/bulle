//go:build !linux && !darwin

package app

import (
	"github.com/vincentarelbundock/bulle/internal/policy"
)

type denialProbe struct{}

func startDenialProbe(policy.Policy) denialProbe { return denialProbe{} }

func (denialProbe) hints() []string { return nil }

func (denialProbe) grants() []grant { return nil }

func recordingSupported() (string, bool) {
	return "recording is not supported on this platform", false
}
