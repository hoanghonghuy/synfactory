package runtime

import (
	"strings"
	"testing"
)

func TestPrepareDockerCommandReadOnly(t *testing.T) {
	spec, err := prepareCommandSpec(CommandSpec{
		ExecutionID: "run/1", Name: "codex", Args: []string{"exec"}, Dir: t.TempDir(),
		Env:     map[string]string{"TOKEN": "secret"},
		Sandbox: SandboxSpec{Mode: SandboxDocker, Image: "agent:latest", ReadOnly: true, NetworkAllowed: false, Memory: "1g", CPUs: "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "docker" {
		t.Fatalf("name=%s", spec.Name)
	}
	joined := strings.Join(spec.Args, " ")
	for _, want := range []string{"run", "readonly", "--network none", "--memory 1g", "--cpus 1", "--env TOKEN", "agent:latest codex exec"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	if strings.Contains(joined, "secret") {
		t.Fatalf("secret leaked into docker args")
	}
}
