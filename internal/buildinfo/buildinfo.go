package buildinfo

import "strings"

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func Version() string {
	if value := strings.TrimSpace(version); value != "" {
		return value
	}
	return "dev"
}

func Commit() string { return strings.TrimSpace(commit) }

func Date() string { return strings.TrimSpace(date) }
