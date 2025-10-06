package core

import (
	"os"
	"strconv"
)

// GetMaxConcurrentReconciles returns the maximum number of concurrent reconciliations
// from the MAX_CONCURRENT_RECONCILES environment variable, or a default value of 5
func GetMaxConcurrentReconciles() int {
	if v := os.Getenv("MAX_CONCURRENT_RECONCILES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	// Default: 5 concurrent reconciliations (good balance for most clusters)
	return 5
}
