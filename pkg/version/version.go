package version

import "fmt"

// These values are set via linker flags in scripts/build
var (
	Version   = "v0.0.0-dev"
	GitCommit = "HEAD"
)

func FriendlyVersion() string {
	return fmt.Sprintf("%s (%s)", Version, GitCommit)
}
