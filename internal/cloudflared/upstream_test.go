package cloudflared

import (
	"os"
	"strings"
	"testing"
)

func TestUpstreamPinRecordsSourceVersion(t *testing.T) {
	body, err := os.ReadFile("../../third_party/cloudflared/UPSTREAM.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "| Tag | `2026.9.1` |") {
		t.Fatal("UPSTREAM.md missing original source tag")
	}
	if !strings.Contains(text, "f11dea9cb7079e90a982c1a2d5548ab40847fdcf") {
		t.Fatal("UPSTREAM.md missing pinned commit")
	}
	if !strings.Contains(text, "github.com/chungthuang/quic-go v0.45.1-0.20260529212404-a9fddf436fc4") {
		t.Fatal("UPSTREAM.md missing pinned QUIC override")
	}
}
