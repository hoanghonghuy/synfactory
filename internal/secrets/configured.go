package secrets

import (
	"fmt"
	"os"
	"strings"
)

const DefaultFileRoot = "/run/secrets"

// ConfiguredFromEnv builds the process-level logical secret provider without
// exposing backend-specific selection to workflow/runtime consumers.
func ConfiguredFromEnv() (Provider, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("SYNFACTORY_SECRET_PROVIDER")))
	switch backend {
	case "", "env":
		return EnvProvider{Prefix: "SYNFACTORY_"}, nil
	case "file":
		root := strings.TrimSpace(os.Getenv("SYNFACTORY_SECRET_FILE_ROOT"))
		if root == "" {
			root = DefaultFileRoot
		}
		return FileProvider{Root: root}, nil
	default:
		return nil, fmt.Errorf("unsupported SYNFACTORY_SECRET_PROVIDER %q (want env or file)", backend)
	}
}
