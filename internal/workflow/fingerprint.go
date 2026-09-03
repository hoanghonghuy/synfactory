package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

func TaskFingerprint(repository, capability, scope string) string {
	canonical := canonicalText(repository) + "\x00" + canonicalText(capability) + "\x00" + canonicalText(scope)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func canonicalText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	space := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
			continue
		}
		space = true
	}
	return strings.TrimSpace(b.String())
}
