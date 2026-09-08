package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/hoanghonghuy/synfactory/internal/domain"
	runtimefactory "github.com/hoanghonghuy/synfactory/internal/runtime"
)

var (
	capabilityMetadataOnce sync.Once
	capabilityMetadata     json.RawMessage
)

func workerCapabilityMetadata() json.RawMessage {
	capabilityMetadataOnce.Do(func() {
		capabilities := domain.WorkerCapabilities{
			Version:      domain.WorkerCapabilityVersion,
			OS:           goruntime.GOOS,
			Architecture: goruntime.GOARCH,
			CPUClass:     fmt.Sprintf("%d-cpu", goruntime.NumCPU()),
			MemoryClass:  workerMemoryClass(),
			Docker:       binaryAvailable("docker"),
			Labels:       splitCapabilityList(os.Getenv("SYNFACTORY_WORKER_LABELS")),
			GPU:          strings.TrimSpace(os.Getenv("SYNFACTORY_WORKER_GPU")),
			Region:       strings.TrimSpace(os.Getenv("SYNFACTORY_WORKER_REGION")),
			NetworkClass: strings.TrimSpace(os.Getenv("SYNFACTORY_WORKER_NETWORK_CLASS")),
		}
		path := strings.TrimSpace(os.Getenv("SYNFACTORY_RUNTIME_CONFIG"))
		if path == "" {
			path = "/etc/synfactory/runtimes.json"
		}
		if cfg, err := runtimefactory.LoadConfigFile(path); err == nil {
			providerSet := map[string]struct{}{}
			for name, runtimeCfg := range cfg.Runtimes {
				capabilities.Runtimes = append(capabilities.Runtimes, name)
				providerSet[string(runtimeCfg.Kind)] = struct{}{}
			}
			for provider := range providerSet {
				capabilities.Providers = append(capabilities.Providers, provider)
			}
			sort.Strings(capabilities.Runtimes)
			sort.Strings(capabilities.Providers)
		}
		encoded, err := json.Marshal(map[string]any{"capabilities": capabilities.Normalized()})
		if err != nil {
			capabilityMetadata = json.RawMessage(`{}`)
			return
		}
		capabilityMetadata = encoded
	})
	return append(json.RawMessage(nil), capabilityMetadata...)
}

func binaryAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func splitCapabilityList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' })
}

func workerMemoryClass() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kb <= 0 {
			break
		}
		gib := (kb + 1024*1024 - 1) / (1024 * 1024)
		for _, bucket := range []int64{2, 4, 8, 16, 32, 64, 128} {
			if gib <= bucket {
				return fmt.Sprintf("%d-gib", bucket)
			}
		}
		return "128+-gib"
	}
	return "unknown"
}
