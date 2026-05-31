package backends

import (
	"fmt"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

func ForName(name policy.BackendName) (Backend, error) {
	switch name {
	case policy.BackendLinuxLandlock:
		return newLandlockBackend(), nil
	case policy.BackendMacOSSeatbelt:
		return newSeatbeltBackend(), nil
	default:
		return nil, fmt.Errorf("unsupported backend: %s", name)
	}
}
