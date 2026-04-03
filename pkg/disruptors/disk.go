package disruptors

import (
	"context"
	"time"
)

// DiskFillFaultInjector defines the interface for filling disk space in target pods
type DiskFillFaultInjector interface {
	InjectDiskFill(ctx context.Context, fault DiskFillFault, duration time.Duration) error
}

// DiskFillFault specifies a disk fill fault.
// The agent writes a large file to the target path and holds it for the full duration.
// The file is automatically deleted when the fault ends.
// If the written amount exceeds the pod's ephemeral-storage limit, the kubelet evicts the pod.
type DiskFillFault struct {
	// Bytes is the number of bytes to write (required).
	Bytes int64 `js:"bytes"`
	// Path is the directory to write the fill file into (default "/tmp").
	Path string `js:"path"`
	// BlockSize is the write block size in bytes (default 262144 = 256 KiB).
	// Smaller values produce more write syscalls; larger values are faster but less granular.
	BlockSize int64 `js:"blockSize"`
}

// IOStressFaultInjector defines the interface for injecting I/O stress into target pods
type IOStressFaultInjector interface {
	InjectIOStress(ctx context.Context, fault IOStressFault, duration time.Duration) error
}

// IOStressFault specifies a disk I/O stress fault.
// The agent runs N parallel workers that continuously write and read a fixed-size
// working-set file, creating sustained I/O pressure on the target path.
type IOStressFault struct {
	// Path is the directory to create working-set files in (default "/tmp").
	// Can be set to a volume mount path to target a specific PVC.
	Path string `js:"path"`
	// Workers is the number of parallel I/O workers (default 4).
	Workers int `js:"workers"`
	// BytesPerWorker is the working-set file size per worker in bytes (default 1 MiB).
	// Total I/O pressure ≈ Workers × BytesPerWorker per write/read cycle.
	BytesPerWorker int64 `js:"bytesPerWorker"`
}
