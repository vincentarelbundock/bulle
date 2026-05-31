package policy

import "runtime"

func RuntimeDefaultBackend() BackendName {
	switch runtime.GOOS {
	case "linux":
		return BackendLinuxLandlock
	case "darwin":
		return BackendMacOSSeatbelt
	default:
		return BackendName("unsupported")
	}
}
