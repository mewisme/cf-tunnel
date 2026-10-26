package ingress

import (
	"regexp"
	"testing"
)

func TestProtectedIngressCanonicalizesPathBeforeMatching(t *testing.T) {
	ing := Ingress{Rules: []Rule{
		{Path: &Regexp{Regexp: regexp.MustCompile("^/public/")}},
		{Path: &Regexp{Regexp: regexp.MustCompile("^/admin")}},
		{},
	}}
	ing.Rules[1].Config.Access.Required = true
	for _, raw := range []string{
		"/public/../admin/settings",
		"/public/..\\admin/settings",
		"/public//../admin/settings",
	} {
		_, index := ing.FindMatchingRule("example.com", raw)
		if index != 1 {
			t.Errorf("raw path %q selected rule %d, want protected rule 1", raw, index)
		}
	}
}

func TestCanonicalPathPreservesDirectorySuffix(t *testing.T) {
	for raw, want := range map[string]string{
		"":                   "/",
		"/public/../admin/":  "/admin/",
		"/public/../admin/.": "/admin/",
		"/public/..":         "/",
		"/a//b/":             "/a/b/",
	} {
		if got := canonicalPathForMatching(raw); got != want {
			t.Errorf("canonicalPathForMatching(%q) = %q, want %q", raw, got, want)
		}
	}
}
