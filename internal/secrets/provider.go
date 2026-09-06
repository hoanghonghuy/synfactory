package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotFound = errors.New("secret not found")

type Value struct {
	Bytes    []byte
	Provider string
}

func (v Value) String() string {
	return "[REDACTED]"
}

func (v Value) CloneBytes() []byte {
	return append([]byte(nil), v.Bytes...)
}

type Provider interface {
	Resolve(ctx context.Context, logicalName string) (Value, error)
}

type EnvProvider struct {
	Prefix string
}

func (p EnvProvider) Resolve(_ context.Context, logicalName string) (Value, error) {
	key, err := envKey(p.Prefix, logicalName)
	if err != nil {
		return Value{}, err
	}
	value, ok := os.LookupEnv(key)
	if !ok {
		return Value{}, fmt.Errorf("%w: %s", ErrNotFound, logicalName)
	}
	return Value{Bytes: []byte(value), Provider: "env"}, nil
}

type FileProvider struct {
	Root string
}

func (p FileProvider) Resolve(_ context.Context, logicalName string) (Value, error) {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return Value{}, err
	}
	root := filepath.Clean(strings.TrimSpace(p.Root))
	if root == "." || !filepath.IsAbs(root) {
		return Value{}, errors.New("secret file root must be an absolute path")
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Value{}, errors.New("secret path escapes configured root")
	}
	value, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Value{}, fmt.Errorf("%w: %s", ErrNotFound, logicalName)
	}
	if err != nil {
		return Value{}, fmt.Errorf("read secret %q: %w", logicalName, err)
	}
	return Value{Bytes: value, Provider: "file"}, nil
}

func normalizeLogicalName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("logical secret name is required")
	}
	value = strings.ReplaceAll(value, "\\", "/")
	clean := filepath.ToSlash(filepath.Clean(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", errors.New("logical secret name must be relative")
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("logical secret name contains an invalid segment")
		}
	}
	return clean, nil
}

func envKey(prefix, logicalName string) (string, error) {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(prefix))
	for _, r := range strings.ToUpper(name) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String(), nil
}
