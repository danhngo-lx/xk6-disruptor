package network

import (
	"context"
	"fmt"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/runtime"
)

// ShapingConfig defines traffic shaping parameters applied via tc netem
type ShapingConfig struct {
	// Network interface to apply shaping on (default: eth0)
	Interface string
	// Average packet delay
	Delay time.Duration
	// Delay variation (jitter)
	Jitter time.Duration
	// Fraction of packets to drop (0.0-1.0)
	Loss float32
	// Fraction of packets to corrupt (0.0-1.0)
	Corrupt float32
	// Fraction of packets to duplicate (0.0-1.0)
	Duplicate float32
	// Bandwidth rate limit (e.g. "1mbit", "100kbit")
	Rate string
}

// Shaper applies traffic shaping disruptions using tc netem
type Shaper struct {
	executor runtime.Executor
	config   ShapingConfig
}

// NewShaper creates a new Shaper with the given configuration
func NewShaper(executor runtime.Executor, config ShapingConfig) (*Shaper, error) {
	if config.Interface == "" {
		config.Interface = "eth0"
	}

	if config.Loss < 0 || config.Loss > 1 {
		return nil, fmt.Errorf("loss must be in the range [0.0, 1.0]")
	}

	if config.Corrupt < 0 || config.Corrupt > 1 {
		return nil, fmt.Errorf("corrupt must be in the range [0.0, 1.0]")
	}

	if config.Duplicate < 0 || config.Duplicate > 1 {
		return nil, fmt.Errorf("duplicate must be in the range [0.0, 1.0]")
	}

	return &Shaper{executor: executor, config: config}, nil
}

// Apply applies traffic shaping for the given duration then cleans up
func (s *Shaper) Apply(ctx context.Context, duration time.Duration) error {
	args := s.buildNetemArgs()
	if len(args) == 0 {
		return fmt.Errorf("at least one shaping parameter must be specified")
	}

	// Use "replace" instead of "add" so the command succeeds even if a qdisc
	// already exists on the interface (e.g. from a previous interrupted run).
	addArgs := append([]string{"qdisc", "replace", "dev", s.config.Interface, "root", "netem"}, args...)
	if out, err := s.executor.Exec("tc", addArgs...); err != nil {
		return fmt.Errorf("applying tc netem: %w: %q", err, out)
	}

	defer func() {
		_, _ = s.executor.Exec("tc", "qdisc", "del", "dev", s.config.Interface, "root")
	}()

	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	<-ctx.Done()

	return nil
}

func (s *Shaper) buildNetemArgs() []string {
	var args []string

	if s.config.Delay > 0 {
		args = append(args, "delay", fmt.Sprintf("%dms", s.config.Delay.Milliseconds()))
		if s.config.Jitter > 0 {
			args = append(args, fmt.Sprintf("%dms", s.config.Jitter.Milliseconds()))
		}
	}

	if s.config.Loss > 0 {
		args = append(args, "loss", fmt.Sprintf("%.4f%%", s.config.Loss*100))
	}

	if s.config.Corrupt > 0 {
		args = append(args, "corrupt", fmt.Sprintf("%.4f%%", s.config.Corrupt*100))
	}

	if s.config.Duplicate > 0 {
		args = append(args, "duplicate", fmt.Sprintf("%.4f%%", s.config.Duplicate*100))
	}

	if s.config.Rate != "" {
		args = append(args, "rate", s.config.Rate)
	}

	return args
}
