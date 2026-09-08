package domain

import (
	"sort"
	"strings"
)

const WorkerCapabilityVersion = 1

type WorkerCapabilities struct {
	Version      int      `json:"version"`
	OS           string   `json:"os"`
	Architecture string   `json:"architecture"`
	CPUClass     string   `json:"cpu_class"`
	MemoryClass  string   `json:"memory_class"`
	Docker       bool     `json:"docker"`
	Runtimes     []string `json:"runtimes,omitempty"`
	Providers    []string `json:"providers,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	GPU          string   `json:"gpu,omitempty"`
	Region       string   `json:"region,omitempty"`
	NetworkClass string   `json:"network_class,omitempty"`
}

type JobRequirements struct {
	OS           string   `json:"os,omitempty"`
	Architecture string   `json:"architecture,omitempty"`
	CPUClass     string   `json:"cpu_class,omitempty"`
	MemoryClass  string   `json:"memory_class,omitempty"`
	Docker       bool     `json:"docker,omitempty"`
	Runtimes     []string `json:"runtimes,omitempty"`
	Providers    []string `json:"providers,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	GPU          string   `json:"gpu,omitempty"`
	Region       string   `json:"region,omitempty"`
	NetworkClass string   `json:"network_class,omitempty"`
}

func (c WorkerCapabilities) Normalized() WorkerCapabilities {
	if c.Version <= 0 {
		c.Version = WorkerCapabilityVersion
	}
	c.OS = strings.TrimSpace(strings.ToLower(c.OS))
	c.Architecture = strings.TrimSpace(strings.ToLower(c.Architecture))
	c.CPUClass = strings.TrimSpace(strings.ToLower(c.CPUClass))
	c.MemoryClass = strings.TrimSpace(strings.ToLower(c.MemoryClass))
	c.GPU = strings.TrimSpace(strings.ToLower(c.GPU))
	c.Region = strings.TrimSpace(strings.ToLower(c.Region))
	c.NetworkClass = strings.TrimSpace(strings.ToLower(c.NetworkClass))
	c.Runtimes = normalizeCapabilityList(c.Runtimes)
	c.Providers = normalizeCapabilityList(c.Providers)
	c.Labels = normalizeCapabilityList(c.Labels)
	return c
}

func (r JobRequirements) Normalized() JobRequirements {
	r.OS = strings.TrimSpace(strings.ToLower(r.OS))
	r.Architecture = strings.TrimSpace(strings.ToLower(r.Architecture))
	r.CPUClass = strings.TrimSpace(strings.ToLower(r.CPUClass))
	r.MemoryClass = strings.TrimSpace(strings.ToLower(r.MemoryClass))
	r.GPU = strings.TrimSpace(strings.ToLower(r.GPU))
	r.Region = strings.TrimSpace(strings.ToLower(r.Region))
	r.NetworkClass = strings.TrimSpace(strings.ToLower(r.NetworkClass))
	r.Runtimes = normalizeCapabilityList(r.Runtimes)
	r.Providers = normalizeCapabilityList(r.Providers)
	r.Labels = normalizeCapabilityList(r.Labels)
	return r
}

func (r JobRequirements) Empty() bool {
	r = r.Normalized()
	return r.OS == "" && r.Architecture == "" && r.CPUClass == "" && r.MemoryClass == "" && !r.Docker &&
		len(r.Runtimes) == 0 && len(r.Providers) == 0 && len(r.Labels) == 0 && r.GPU == "" && r.Region == "" && r.NetworkClass == ""
}

func (r JobRequirements) CompatibleWith(c WorkerCapabilities) bool {
	r = r.Normalized()
	c = c.Normalized()
	if r.OS != "" && r.OS != c.OS || r.Architecture != "" && r.Architecture != c.Architecture ||
		r.CPUClass != "" && r.CPUClass != c.CPUClass || r.MemoryClass != "" && r.MemoryClass != c.MemoryClass ||
		r.Docker && !c.Docker || r.GPU != "" && r.GPU != c.GPU || r.Region != "" && r.Region != c.Region ||
		r.NetworkClass != "" && r.NetworkClass != c.NetworkClass {
		return false
	}
	return containsAll(c.Runtimes, r.Runtimes) && containsAll(c.Providers, r.Providers) && containsAll(c.Labels, r.Labels)
}

func normalizeCapabilityList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsAll(have, required []string) bool {
	available := make(map[string]struct{}, len(have))
	for _, value := range normalizeCapabilityList(have) {
		available[value] = struct{}{}
	}
	for _, value := range normalizeCapabilityList(required) {
		if _, ok := available[value]; !ok {
			return false
		}
	}
	return true
}
