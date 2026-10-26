package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionFromBuildInfo(t *testing.T) {
	cases := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{
			name: "go install binary",
			info: &debug.BuildInfo{Main: debug.Module{Path: modulePath + "/cmd/cf-tunnel", Version: "v0.0.1"}},
			want: "v0.0.1",
		},
		{
			name: "development binary with module tag",
			info: &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "v0.0.1+dirty"}},
			want: "v0.0.1+dirty",
		},
		{
			name: "importable library dependency",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/app", Version: "(devel)"},
				Deps: []*debug.Module{{Path: modulePath, Version: "v0.2.3"}},
			},
			want: "v0.2.3",
		},
		{
			name: "local replaced dependency",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/app", Version: "(devel)"},
				Deps: []*debug.Module{{Path: modulePath, Version: "v0.2.3", Replace: &debug.Module{Path: "../cf-tunnel"}}},
			},
		},
		{
			name: "unrelated app",
			info: &debug.BuildInfo{Main: debug.Module{Path: "example.com/app", Version: "v5.6.7"}},
		},
		{
			name: "unversioned module",
			info: &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}},
		},
		{name: "missing", info: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionFromBuildInfo(tc.info); got != tc.want {
				t.Fatalf("versionFromBuildInfo() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEdgeVersionUsesRealModuleVersion(t *testing.T) {
	got := EdgeVersion()
	want := "cf-tunnel.v" + strings.TrimPrefix(Version(), "v")
	if got != want {
		t.Fatalf("EdgeVersion() = %q, want %q", got, want)
	}
	if strings.Contains(got, "2026.9.1") {
		t.Fatalf("edge version must not pretend to be the original cloudflared: %q", got)
	}
}
