package events

import "testing"

func TestDedupeKeyStableAndRevisionSensitive(t *testing.T) {
	a := DedupeKey("github", "owner/repo", "pull_request.synchronize", "55", "abc")
	b := DedupeKey("github", "owner/repo", "pull_request.synchronize", "55", "abc")
	c := DedupeKey("github", "owner/repo", "pull_request.synchronize", "55", "def")

	if a != b {
		t.Fatal("same event identity must produce same dedupe key")
	}
	if a == c {
		t.Fatal("different revisions must produce different dedupe keys")
	}
}
