package parallel

import "time"

// Default configuration values
const (
	DefaultMaxParallelism = 5
	DefaultToolTimeout    = 120 * time.Second
)

// DefaultSequentialTools returns the default list of tools that should execute sequentially
// These tools typically have dependencies on other tools' results
func DefaultSequentialTools() []string {
	return []string{
		"save_to_memory", // Should execute after search_memory to avoid saving redundant info
	}
}
