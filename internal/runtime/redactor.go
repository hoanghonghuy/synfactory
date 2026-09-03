package runtime

import "strings"

type Redactor struct {
	secrets []string
}

func NewRedactor(secrets ...string) Redactor {
	unique := make([]string, 0, len(secrets))
	seen := map[string]bool{}
	for _, secret := range secrets {
		if len(secret) < 3 || seen[secret] {
			continue
		}
		seen[secret] = true
		unique = append(unique, secret)
	}
	return Redactor{secrets: unique}
}

func (r Redactor) String(value string) string {
	for _, secret := range r.secrets {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}
