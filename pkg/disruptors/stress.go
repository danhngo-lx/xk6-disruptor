package disruptors

import (
	"context"
	"time"
)

// CPUStressFaultInjector defines the interface for injecting CPU stress faults
type CPUStressFaultInjector interface {
	InjectCPUStress(ctx context.Context, fault CPUStressFault, duration time.Duration) error
}

// CPUStressFault specifies a CPU stress fault to be injected
type CPUStressFault struct {
	// Load is the percentage of CPU to consume (0-100)
	Load int `js:"load"`
	// CPUs is the number of CPUs to stress
	CPUs int `js:"cpus"`
}

// MemoryStressFaultInjector defines the interface for injecting memory pressure faults
type MemoryStressFaultInjector interface {
	InjectMemoryStress(ctx context.Context, fault MemoryStressFault, duration time.Duration) error
}

// MemoryStressFault specifies a memory stress fault to be injected
type MemoryStressFault struct {
	// Bytes is the number of bytes to allocate and hold
	Bytes int64 `js:"bytes"`
}
