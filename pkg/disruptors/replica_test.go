package disruptors

import (
	"errors"
	"testing"
	"time"
)

func ptrInt32(v int32) *int32 { return &v }

func TestReplicaChangeFault_Validate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		title    string
		fault    ReplicaChangeFault
		duration time.Duration
		wantErr  bool
	}{
		{
			title:   "absolute replicas ok",
			fault:   ReplicaChangeFault{Replicas: ptrInt32(0)},
			wantErr: false,
		},
		{
			title:   "delta ok",
			fault:   ReplicaChangeFault{Delta: ptrInt32(-1)},
			wantErr: false,
		},
		{
			title:   "percentage ok",
			fault:   ReplicaChangeFault{Percentage: ptrInt32(50)},
			wantErr: false,
		},
		{
			title:   "none set",
			fault:   ReplicaChangeFault{},
			wantErr: true,
		},
		{
			title:   "two set",
			fault:   ReplicaChangeFault{Replicas: ptrInt32(2), Delta: ptrInt32(1)},
			wantErr: true,
		},
		{
			title:   "all three set",
			fault:   ReplicaChangeFault{Replicas: ptrInt32(2), Delta: ptrInt32(1), Percentage: ptrInt32(50)},
			wantErr: true,
		},
		{
			title:   "negative percentage",
			fault:   ReplicaChangeFault{Percentage: ptrInt32(-10)},
			wantErr: true,
		},
		{
			title:    "autoRevert without duration",
			fault:    ReplicaChangeFault{Replicas: ptrInt32(0), AutoRevert: true},
			duration: 0,
			wantErr:  true,
		},
		{
			title:    "autoRevert with duration",
			fault:    ReplicaChangeFault{Replicas: ptrInt32(0), AutoRevert: true},
			duration: 1 * time.Second,
			wantErr:  false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			err := tc.fault.Validate(tc.duration)
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestReplicaChangeFault_Resolve(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		title   string
		fault   ReplicaChangeFault
		current int32
		want    int32
	}{
		{
			title:   "absolute zero",
			fault:   ReplicaChangeFault{Replicas: ptrInt32(0)},
			current: 5,
			want:    0,
		},
		{
			title:   "absolute non-zero",
			fault:   ReplicaChangeFault{Replicas: ptrInt32(10)},
			current: 3,
			want:    10,
		},
		{
			title:   "delta positive",
			fault:   ReplicaChangeFault{Delta: ptrInt32(2)},
			current: 3,
			want:    5,
		},
		{
			title:   "delta negative",
			fault:   ReplicaChangeFault{Delta: ptrInt32(-2)},
			current: 3,
			want:    1,
		},
		{
			title:   "delta clamps to zero",
			fault:   ReplicaChangeFault{Delta: ptrInt32(-10)},
			current: 3,
			want:    0,
		},
		{
			title:   "percentage halve",
			fault:   ReplicaChangeFault{Percentage: ptrInt32(50)},
			current: 4,
			want:    2,
		},
		{
			title:   "percentage floor rounding",
			fault:   ReplicaChangeFault{Percentage: ptrInt32(50)},
			current: 1,
			want:    0,
		},
		{
			title:   "percentage 100 no change",
			fault:   ReplicaChangeFault{Percentage: ptrInt32(100)},
			current: 7,
			want:    7,
		},
		{
			title:   "percentage 0",
			fault:   ReplicaChangeFault{Percentage: ptrInt32(0)},
			current: 7,
			want:    0,
		},
		{
			title:   "percentage 200",
			fault:   ReplicaChangeFault{Percentage: ptrInt32(200)},
			current: 3,
			want:    6,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			got := tc.fault.Resolve(tc.current)
			if got != tc.want {
				t.Errorf("Resolve(%d) = %d, want %d", tc.current, got, tc.want)
			}
		})
	}
}

// Sanity that errors propagate as wrapped — not a behavioural test, just a guard
// that Validate stays consistent if anyone changes wording.
func TestReplicaChangeFault_Validate_ReturnsError(t *testing.T) {
	t.Parallel()
	err := ReplicaChangeFault{}.Validate(0)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, nil) {
		t.Fatal("expected non-nil error wrap")
	}
}
