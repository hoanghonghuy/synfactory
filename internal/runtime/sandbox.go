package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var dockerNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

func prepareCommandSpec(spec CommandSpec) (CommandSpec, error) {
	if spec.Sandbox.Mode == "" || spec.Sandbox.Mode == SandboxHost { return spec, nil }
	if spec.Sandbox.Mode != SandboxDocker { return CommandSpec{}, fmt.Errorf("unsupported sandbox mode %q", spec.Sandbox.Mode) }
	if strings.TrimSpace(spec.Sandbox.Image) == "" { return CommandSpec{}, errors.New("docker sandbox image is required") }
	if strings.TrimSpace(spec.Dir) == "" { return CommandSpec{}, errors.New("docker sandbox workspace directory is required") }
	workspace, err := filepath.Abs(spec.Dir); if err != nil { return CommandSpec{}, fmt.Errorf("resolve workspace path: %w", err) }
	containerPath := spec.Sandbox.ContainerPath; if containerPath == "" { containerPath = "/workspace" }
	mount := fmt.Sprintf("type=bind,src=%s,dst=%s", workspace, containerPath); if spec.Sandbox.ReadOnly { mount += ",readonly" }
	name := dockerNameSanitizer.ReplaceAllString("synfactory-"+spec.ExecutionID, "-"); if len(name) > 63 { name = name[:63] }
	args := []string{"run", "--rm", "--name", name, "--workdir", containerPath, "--mount", mount}
	if !spec.Sandbox.NetworkAllowed { args = append(args, "--network", "none") }
	if spec.Sandbox.Memory != "" { args = append(args, "--memory", spec.Sandbox.Memory) }
	if spec.Sandbox.CPUs != "" { args = append(args, "--cpus", spec.Sandbox.CPUs) }

	// Docker's `--env NAME` copies a value from the docker client's own
	// environment without putting that value on the command line. Include
	// explicit runtime env plus configured secret values discovered in the
	// inherited host environment.
	keys := map[string]bool{}
	for key := range spec.Env { keys[key] = true }
	secretSet := map[string]bool{}
	for _, secret := range spec.Secrets { if secret != "" { secretSet[secret] = true } }
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok && secretSet[value] { keys[key] = true }
	}
	ordered := make([]string, 0, len(keys)); for key := range keys { ordered = append(ordered, key) }; sort.Strings(ordered)
	for _, key := range ordered { args = append(args, "--env", key) }
	args = append(args, spec.Sandbox.Image, spec.Name); args = append(args, spec.Args...)
	spec.Name = "docker"; spec.Args = args; spec.Dir = ""
	return spec, nil
}
