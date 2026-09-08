package domain

import "testing"

func TestJobRequirementsCompatibleWithWorkerCapabilities(t *testing.T) {
	capabilities := WorkerCapabilities{
		OS: "Linux", Architecture: "AMD64", CPUClass: "8-cpu", MemoryClass: "16-gib", Docker: true,
		Runtimes: []string{"codex", "opencode"}, Providers: []string{"openai_compatible"}, Labels: []string{"trusted", "gpu-pool"}, Region: "ap-southeast-1",
	}
	requirements := JobRequirements{
		OS: "linux", Architecture: "amd64", Docker: true, Runtimes: []string{"CODEX"}, Labels: []string{"trusted"}, Region: "AP-SOUTHEAST-1",
	}
	if !requirements.CompatibleWith(capabilities) {
		t.Fatal("expected compatible capability inventory")
	}
	requirements.Runtimes = []string{"claude"}
	if requirements.CompatibleWith(capabilities) {
		t.Fatal("missing runtime must be incompatible")
	}
}

func TestCapabilitiesNormalizeVersionAndLists(t *testing.T) {
	capabilities := (WorkerCapabilities{Runtimes: []string{" Codex ", "codex", "OpenCode"}}).Normalized()
	if capabilities.Version != WorkerCapabilityVersion {
		t.Fatalf("version = %d, want %d", capabilities.Version, WorkerCapabilityVersion)
	}
	if len(capabilities.Runtimes) != 2 || capabilities.Runtimes[0] != "codex" || capabilities.Runtimes[1] != "opencode" {
		t.Fatalf("unexpected normalized runtimes: %#v", capabilities.Runtimes)
	}
	if !(JobRequirements{}).Empty() {
		t.Fatal("zero requirements must be empty")
	}
}
