//go:build !linux && !darwin

package supervisor

import (
	"fmt"
	"os"
	"time"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

type Options struct {
	Executable  string
	Timeout     time.Duration
	GracePeriod time.Duration
	Stdin       *os.File
	Stdout      *os.File
	Stderr      *os.File
}

func Run(policy.Policy, Options) error {
	return fmt.Errorf("timeout supervision is only supported on linux and darwin")
}
