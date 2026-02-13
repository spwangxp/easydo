package version

import "fmt"

// Version info - set by ldflags at build time
var (
	Commit      = "unknown"
	CommitShort = "unknown"
	Date        = "unknown"
	Version     = "1.0.0"
)

// Info returns version information string
func Info() string {
	return fmt.Sprintf("EasyDo Agent v%s (commit: %s, built: %s)", Version, CommitShort, Date)
}

// FullInfo returns detailed version information
func FullInfo() string {
	return fmt.Sprintf(`EasyDo Agent Version Information:
  Version:   %s
  Commit:    %s
  CommitShort: %s
  Built:     %s`, Version, Commit, CommitShort, Date)
}
