package limits

import (
	"runtime"
	"sync"
)

// Current reports what this machine can enforce, probing at most once per
// process. The probe creates and removes a cgroup, so caching keeps repeated
// calls cheap; more importantly it guarantees that the warning printed before
// a run, the mechanism shown by bulle policy, and the enforcement actually
// attempted all describe the same answer.
var Current = sync.OnceValue(func() Support {
	return Detect(runtime.GOOS)
})
