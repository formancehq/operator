package stacks

import (
	"os"
	"strconv"
)

// GetStackConcurrency returns the maximum number of concurrent stack reconciliations
// from the STACK_MAX_CONCURRENT environment variable, or a default value of 5
func GetStackConcurrency() int {
	if v := os.Getenv("STACK_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	// Default: 5 concurrent reconciliations (good balance for most clusters)
	return 5
}
