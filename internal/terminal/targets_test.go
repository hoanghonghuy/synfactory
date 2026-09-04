package terminal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTargetFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targets.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadTargetsAcceptsLocalAndSSHPolicy(t *testing.T) {
	path := writeTargetFile(t, `{
  "targets": [
    {"id":"control","kind":"local","work_dir":"/opt/synfactory","shell":"/bin/bash"},
    {"id":"worker-1","kind":"ssh","host":"10.0.0.8","user":"ubuntu","identity_file":"/run/secrets/worker.key","known_hosts_file":"/etc/synfactory/known_hosts"}
  ]
}`)
	targets, err := LoadTargets(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].ID != "control" || targets[1].Port != 22 {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	if targets[1].IdentityFile != "/run/secrets/worker.key" || targets[1].KnownHostsFile != "/etc/synfactory/known_hosts" {
		t.Fatalf("ssh credential policy paths were not retained: %+v", targets[1])
	}
}

func TestLoadTargetsRejectsDuplicateOrUnknownTargets(t *testing.T) {
	duplicate := writeTargetFile(t, `{"targets":[{"id":"control","kind":"local"},{"id":"control","kind":"local"}]}`)
	if _, err := LoadTargets(duplicate); !errors.Is(err, ErrInvalidTargetConfig) {
		t.Fatalf("duplicate target error = %v", err)
	}

	unknown := writeTargetFile(t, `{"targets":[{"id":"control","kind":"docker"}]}`)
	if _, err := LoadTargets(unknown); !errors.Is(err, ErrInvalidTargetConfig) {
		t.Fatalf("unknown target kind error = %v", err)
	}
}

func TestLoadTargetsRejectsUnsafeLocalAndIncompleteSSHPolicy(t *testing.T) {
	localWithSSHFields := writeTargetFile(t, `{"targets":[{"id":"control","kind":"local","host":"example.com"}]}`)
	if _, err := LoadTargets(localWithSSHFields); !errors.Is(err, ErrInvalidTargetConfig) {
		t.Fatalf("local target with ssh fields error = %v", err)
	}

	relativeWorkDir := writeTargetFile(t, `{"targets":[{"id":"control","kind":"local","work_dir":"repos/project"}]}`)
	if _, err := LoadTargets(relativeWorkDir); !errors.Is(err, ErrInvalidTargetConfig) {
		t.Fatalf("relative local workdir error = %v", err)
	}

	missingHostKeyPolicy := writeTargetFile(t, `{"targets":[{"id":"worker","kind":"ssh","host":"10.0.0.8","user":"ubuntu","identity_file":"/run/secrets/worker.key"}]}`)
	if _, err := LoadTargets(missingHostKeyPolicy); !errors.Is(err, ErrInvalidTargetConfig) {
		t.Fatalf("ssh target without known_hosts policy error = %v", err)
	}
}

func TestLoadTargetsRejectsUnknownJSONFields(t *testing.T) {
	path := writeTargetFile(t, `{"targets":[{"id":"control","kind":"local","password":"secret"}]}`)
	if _, err := LoadTargets(path); !errors.Is(err, ErrInvalidTargetConfig) {
		t.Fatalf("unknown secret field error = %v", err)
	}
}
