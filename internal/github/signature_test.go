package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestValidSignature(t *testing.T) {
	secret := "secret"
	body := []byte(`{"ok":true}`)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !ValidSignature(secret, body, signature) {
		t.Fatal("expected signature to be valid")
	}
	if ValidSignature(secret, []byte(`{"ok":false}`), signature) {
		t.Fatal("expected modified body to fail signature validation")
	}
}
