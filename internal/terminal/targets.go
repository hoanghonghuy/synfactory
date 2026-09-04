package terminal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidTargetConfig = errors.New("invalid terminal target configuration")

type TargetFile struct {
	Targets []Target `json:"targets"`
}

func LoadTargets(path string) ([]Target, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: target file path is required", ErrInvalidTargetConfig)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read terminal target file: %w", err)
	}
	var file TargetFile
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("%w: decode targets: %v", ErrInvalidTargetConfig, err)
	}
	if len(file.Targets) == 0 {
		return nil, fmt.Errorf("%w: at least one terminal target is required", ErrInvalidTargetConfig)
	}
	seen := make(map[string]struct{}, len(file.Targets))
	for i := range file.Targets {
		if err := normalizeAndValidateTarget(&file.Targets[i]); err != nil {
			return nil, err
		}
		if _, ok := seen[file.Targets[i].ID]; ok {
			return nil, fmt.Errorf("%w: duplicate target id %q", ErrInvalidTargetConfig, file.Targets[i].ID)
		}
		seen[file.Targets[i].ID] = struct{}{}
	}
	return file.Targets, nil
}

func normalizeAndValidateTarget(target *Target) error {
	target.ID = strings.TrimSpace(target.ID)
	target.WorkDir = strings.TrimSpace(target.WorkDir)
	target.Shell = strings.TrimSpace(target.Shell)
	target.Host = strings.TrimSpace(target.Host)
	target.User = strings.TrimSpace(target.User)
	target.IdentityFile = strings.TrimSpace(target.IdentityFile)
	target.KnownHostsFile = strings.TrimSpace(target.KnownHostsFile)
	if target.ID == "" {
		return fmt.Errorf("%w: target id is required", ErrInvalidTargetConfig)
	}
	if target.WorkDir != "" && !filepath.IsAbs(target.WorkDir) {
		return fmt.Errorf("%w: target %q work_dir must be absolute", ErrInvalidTargetConfig, target.ID)
	}
	if target.Shell != "" && !filepath.IsAbs(target.Shell) {
		return fmt.Errorf("%w: target %q shell must be an absolute executable path", ErrInvalidTargetConfig, target.ID)
	}
	switch target.Kind {
	case TargetLocal:
		if target.Host != "" || target.User != "" || target.IdentityFile != "" || target.KnownHostsFile != "" || target.Port != 0 {
			return fmt.Errorf("%w: local target %q must not contain SSH fields", ErrInvalidTargetConfig, target.ID)
		}
	case TargetSSH:
		if target.Host == "" || target.User == "" {
			return fmt.Errorf("%w: ssh target %q requires host and user", ErrInvalidTargetConfig, target.ID)
		}
		if target.Port == 0 {
			target.Port = 22
		}
		if target.Port < 1 || target.Port > 65535 {
			return fmt.Errorf("%w: ssh target %q port is invalid", ErrInvalidTargetConfig, target.ID)
		}
		if !filepath.IsAbs(target.IdentityFile) || !filepath.IsAbs(target.KnownHostsFile) {
			return fmt.Errorf("%w: ssh target %q requires absolute identity_file and known_hosts_file paths", ErrInvalidTargetConfig, target.ID)
		}
	default:
		return fmt.Errorf("%w: target %q has unsupported kind %q", ErrInvalidTargetConfig, target.ID, target.Kind)
	}
	return nil
}
