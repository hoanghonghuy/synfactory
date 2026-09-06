package secrets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotFound = errors.New("secret not found")

const maxFileSecretBytes = 1 << 20

type Value struct {
	bytes    []byte
	Provider string `json:"provider"`
}

func newValue(value []byte, provider string) Value {
	return Value{bytes: append([]byte(nil), value...), Provider: provider}
}

func (v Value) String() string {
	return "[REDACTED]"
}

func (v Value) GoString() string {
	return "secrets.Value{[REDACTED]}"
}

func (v Value) CloneBytes() []byte {
	return append([]byte(nil), v.bytes...)
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
	return newValue([]byte(value), "env"), nil
}

type FileProvider struct {
	Root string
}

func (p FileProvider) Resolve(_ context.Context, logicalName string) (Value, error) {
	name, err := normalizeLogicalName(logicalName)
	if err != nil {
		return Value{}, err
	}
	rootPath := filepath.Clean(strings.TrimSpace(p.Root))
	if rootPath == "." || !filepath.IsAbs(rootPath) {
		return Value{}, errors.New("secret file root must be an absolute path")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return Value{}, fmt.Errorf("open secret root: %w", err)
	}
	defer root.Close()

	file, err := root.Open(filepath.FromSlash(name))
	if errors.Is(err, os.ErrNotExist) {
		return Value{}, fmt.Errorf("%w: %s", ErrNotFound, logicalName)
	}
	if err != nil {
		return Value{}, fmt.Errorf("open secret %q: %w", logicalName, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return Value{}, fmt.Errorf("stat secret %q: %w", logicalName, err)
	}
	if !info.Mode().IsRegular() {
		return Value{}, errors.New("secret file must be a regular file")
	}
	value, err := io.ReadAll(io.LimitReader(file, maxFileSecretBytes+1))
	if err != nil {
		return Value{}, fmt.Errorf("read secret %q: %w", logicalName, err)
	}
	if len(value) > maxFileSecretBytes {
		return Value{}, errors.New("secret file exceeds size limit")
	}
	return newValue(value, "file"), nil
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
