package stressors

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

const (
	defaultDiskFillBlockSize int64 = 256 * 1024       // 256 KiB
	defaultIOWorkerBytes     int64 = 1 * 1024 * 1024  // 1 MiB working set per worker (conservative; adjust per use case)
	ioWorkerBlockSize        int64 = 1 * 1024 * 1024  // write 1 MiB blocks to create sustained I/O
)

// DiskFillDisruption specifies a disk fill disruption
type DiskFillDisruption struct {
	// Bytes is the number of bytes to write
	Bytes int64
	// Path is the directory to write the fill file into (default /tmp)
	Path string
	// BlockSize is the size of each write operation in bytes (default 256 KiB)
	BlockSize int64
}

// DiskFillStressor fills disk space by writing a large file and holding it for the duration.
// The file is deleted on cleanup, releasing the space.
type DiskFillStressor struct {
	Disruption DiskFillDisruption
}

// NewDiskFillStressor creates a new DiskFillStressor
func NewDiskFillStressor(disruption DiskFillDisruption) (*DiskFillStressor, error) {
	if disruption.Bytes <= 0 {
		return nil, fmt.Errorf("bytes must be greater than zero")
	}

	if disruption.Path == "" {
		disruption.Path = "/tmp"
	}

	if disruption.BlockSize <= 0 {
		disruption.BlockSize = defaultDiskFillBlockSize
	}

	return &DiskFillStressor{Disruption: disruption}, nil
}

// Apply writes a large file to the target path, holds it for the duration, then deletes it.
func (s *DiskFillStressor) Apply(ctx context.Context, duration time.Duration) error {
	//nolint:gosec
	fillPath := filepath.Join(s.Disruption.Path, fmt.Sprintf("disk-fill-%d", rand.Int63()))

	f, err := os.Create(fillPath)
	if err != nil {
		return fmt.Errorf("creating fill file at %q: %w", fillPath, err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(fillPath)
	}()

	block := make([]byte, s.Disruption.BlockSize)
	// Non-zero bytes prevent sparse file optimizations on some filesystems
	for i := range block {
		block[i] = 0xff
	}

	written := int64(0)
	for written < s.Disruption.Bytes {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		remaining := s.Disruption.Bytes - written
		wb := block
		if remaining < s.Disruption.BlockSize {
			wb = block[:remaining]
		}

		n, err := f.Write(wb)
		if err != nil {
			return fmt.Errorf("writing fill data after %d bytes: %w", written, err)
		}

		written += int64(n)
	}

	// Flush to disk so the usage is visible to the kubelet
	if err := f.Sync(); err != nil {
		return fmt.Errorf("syncing fill file: %w", err)
	}

	// Hold the filled disk for the remainder of the duration
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	<-ctx.Done()

	return nil
}

// IODisruption specifies an IO stress disruption
type IODisruption struct {
	// Path is the directory to stress (default /tmp)
	Path string
	// Workers is the number of parallel I/O workers (default 4)
	Workers int
	// BytesPerWorker is the working-set file size written by each worker (default 1 MiB)
	BytesPerWorker int64
}

// IOStressor runs parallel workers that continuously write and read a working-set
// file to create sustained I/O pressure on the target path.
type IOStressor struct {
	Disruption IODisruption
}

// NewIOStressor creates a new IOStressor
func NewIOStressor(disruption IODisruption) (*IOStressor, error) {
	if disruption.Path == "" {
		disruption.Path = "/tmp"
	}

	if disruption.Workers <= 0 {
		disruption.Workers = 4
	}

	if disruption.BytesPerWorker <= 0 {
		disruption.BytesPerWorker = defaultIOWorkerBytes
	}

	return &IOStressor{Disruption: disruption}, nil
}

// Apply starts N parallel workers that continuously write/read a working-set file.
func (s *IOStressor) Apply(ctx context.Context, duration time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	errCh := make(chan error, s.Disruption.Workers)

	for i := range s.Disruption.Workers {
		go func(id int) {
			errCh <- s.worker(ctx, id)
		}(i)
	}

	pending := s.Disruption.Workers
	for pending > 0 {
		select {
		case <-ctx.Done():
			// drain remaining workers then return
			for range pending - 1 {
				<-errCh
			}
			return nil
		case err := <-errCh:
			pending--
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// worker continuously writes and reads a fixed-size file to create I/O pressure.
func (s *IOStressor) worker(ctx context.Context, id int) error {
	//nolint:gosec
	filePath := filepath.Join(s.Disruption.Path, fmt.Sprintf("io-stress-%d-%d", id, rand.Int63()))
	defer os.Remove(filePath) //nolint:errcheck

	blockSize := ioWorkerBlockSize
	if blockSize > s.Disruption.BytesPerWorker {
		blockSize = s.Disruption.BytesPerWorker
	}

	block := make([]byte, blockSize)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Write phase: write the full working-set file in blocks
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("opening io-stress file: %w", err)
		}

		written := int64(0)
		for written < s.Disruption.BytesPerWorker {
			select {
			case <-ctx.Done():
				_ = f.Close()
				return nil
			default:
			}

			remaining := s.Disruption.BytesPerWorker - written
			wb := block
			if remaining < blockSize {
				wb = block[:remaining]
			}

			n, werr := f.Write(wb)
			if werr != nil {
				_ = f.Close()
				return fmt.Errorf("writing io-stress data: %w", werr)
			}
			written += int64(n)
		}

		if serr := f.Sync(); serr != nil {
			_ = f.Close()
			return fmt.Errorf("syncing io-stress file: %w", serr)
		}
		_ = f.Close()

		// Read phase: read the file back to stress read I/O
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		rf, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("opening io-stress file for read: %w", err)
		}

		readBuf := make([]byte, blockSize)
		for {
			select {
			case <-ctx.Done():
				_ = rf.Close()
				return nil
			default:
			}

			n, rerr := rf.Read(readBuf)
			if n == 0 || rerr != nil {
				break
			}
		}
		_ = rf.Close()
	}
}
