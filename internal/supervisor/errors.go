package supervisor

import (
	"fmt"
	"time"
)

type TimeoutError struct {
	Duration time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("command timed out after %s", e.Duration)
}

type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("runner exited with status %d", e.Code)
}
