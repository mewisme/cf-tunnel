package buildinfo

import (
	"runtime/debug"
	"strings"
)

const modulePath = "go.mewis.me/cf-tunnel"

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func Version() string {
	if value := strings.TrimSpace(version); value != "" && value != "dev" {
		return value
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if value := versionFromBuildInfo(info); value != "" {
			return value
		}
	}
	return "dev"
}

// EdgeVersion identifies this fork independently from the cloudflared version
// on which its code was originally based.
func EdgeVersion() string {
	return "cf-tunnel.v" + strings.TrimPrefix(Version(), "v")
}

func versionFromBuildInfo(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	if info.Main.Path == modulePath || strings.HasPrefix(info.Main.Path, modulePath+"/") {
		if value := moduleVersion(info.Main.Version); value != "" {
			return value
		}
	}
	for _, dep := range info.Deps {
		if dep.Path == modulePath && dep.Replace == nil {
			return moduleVersion(dep.Version)
		}
	}
	return ""
}

func moduleVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "(devel)" || value == "devel" {
		return ""
	}
	return value
}

func Commit() string { return strings.TrimSpace(commit) }

func Date() string { return strings.TrimSpace(date) }
