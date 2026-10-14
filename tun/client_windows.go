package tun

// RunClient is not used on Windows, where the core runs elevated with Kumo.
func RunClient(string, []string) int { return 1 }

// FilterEnv is unused on Windows.
const FilterEnv = "KUMO_CTL_FILTER"

// RunFilter is not used on Windows.
func RunFilter(string) int { return 1 }
