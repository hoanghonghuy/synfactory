package events

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func DedupeKey(provider, repository, kind, subject, revision string) string {
	canonical := strings.Join([]string{provider, repository, kind, subject, revision}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
