package backends

import "github.com/vincentarelbundock/bulle/internal/policy"

type Backend interface {
	Run(p policy.Policy) error
}
