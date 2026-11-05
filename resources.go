package main

import (
	"strconv"
	"strings"
)

// resourceSet holds the three things the simulator reasons about. CPU is in
// millicores, memory in bytes, GPUs as whole devices.
type resourceSet struct {
	CPU    int64 `json:"cpu_millis"`
	Memory int64 `json:"memory_bytes"`
	GPUs   int   `json:"gpus"`
}

func (r resourceSet) add(other resourceSet) resourceSet {
	return resourceSet{CPU: r.CPU + other.CPU, Memory: r.Memory + other.Memory, GPUs: r.GPUs + other.GPUs}
}

func (r resourceSet) fitsIn(capacity resourceSet) bool {
	return r.CPU <= capacity.CPU && r.Memory <= capacity.Memory && r.GPUs <= capacity.GPUs
}

func (r resourceSet) isZero() bool {
	return r.CPU == 0 && r.Memory == 0 && r.GPUs == 0
}

func quantityMillis(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if strings.HasSuffix(value, "m") {
		n, err := strconv.ParseInt(strings.TrimSuffix(value, "m"), 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int64(f * 1000)
}

var memoryUnits = []struct {
	suffix string
	scale  float64
}{
	{"Ki", 1024},
	{"Mi", 1024 * 1024},
	{"Gi", 1024 * 1024 * 1024},
	{"Ti", 1024 * 1024 * 1024 * 1024},
	{"K", 1000},
	{"M", 1000 * 1000},
	{"G", 1000 * 1000 * 1000},
	{"T", 1000 * 1000 * 1000 * 1000},
}

func quantityBytes(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	for _, unit := range memoryUnits {
		if strings.HasSuffix(value, unit.suffix) {
			f, err := strconv.ParseFloat(strings.TrimSuffix(value, unit.suffix), 64)
			if err != nil {
				return 0
			}
			return int64(f * unit.scale)
		}
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int64(f)
}

func formatCPU(millis int64) string {
	if millis == 0 {
		return "0"
	}
	if millis%1000 == 0 {
		return strconv.FormatInt(millis/1000, 10)
	}
	return strconv.FormatFloat(float64(millis)/1000, 'f', -1, 64)
}

func formatMemory(bytes int64) string {
	if bytes == 0 {
		return "0"
	}
	const gi = 1024 * 1024 * 1024
	const mi = 1024 * 1024
	if bytes >= gi {
		return strconv.FormatFloat(float64(bytes)/gi, 'f', 1, 64) + "Gi"
	}
	return strconv.FormatFloat(float64(bytes)/mi, 'f', 0, 64) + "Mi"
}

func formatResources(r resourceSet) string {
	parts := []string{"cpu " + formatCPU(r.CPU), "mem " + formatMemory(r.Memory)}
	if r.GPUs > 0 {
		parts = append(parts, "gpu "+strconv.Itoa(r.GPUs))
	}
	return strings.Join(parts, ", ")
}
