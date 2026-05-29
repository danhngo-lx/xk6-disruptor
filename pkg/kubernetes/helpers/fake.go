package helpers

import (
	"context"
	"sync"
)

// Command records the execution of a command in a Pod
type Command struct {
	Pod       string
	Namespace string
	Container string
	Command   []string
	Stdin     []byte
}

// ExecResult represents the result of a single command execution
type ExecResult struct {
	Stdout []byte
	Stderr []byte
	Err    error
}

// FakePodCommandExecutor mocks the execution of a command in a pod
// recording the command history and returning a predefined stdout, stderr, and error
type FakePodCommandExecutor struct {
	mutex   sync.Mutex
	history []Command
	// Default result (used when no sequence is set or sequence is exhausted)
	stdout []byte
	stderr []byte
	err    error
	// Sequence of results for call-by-call testing
	results     []ExecResult
	resultIndex int
}

// Exec records the execution of a command and returns the pre-defined
func (f *FakePodCommandExecutor) Exec(
	_ context.Context,
	pod string,
	namespace string,
	container string,
	cmd []string,
	stdin []byte,
) ([]byte, []byte, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.history = append(f.history, Command{
		Pod:       pod,
		Namespace: namespace,
		Container: container,
		Command:   cmd,
		Stdin:     stdin,
	})

	// Use sequence results if available
	if len(f.results) > 0 {
		idx := f.resultIndex
		if idx >= len(f.results) {
			// Repeat last result if sequence exhausted
			idx = len(f.results) - 1
		} else {
			f.resultIndex++
		}
		r := f.results[idx]
		return r.Stdout, r.Stderr, r.Err
	}

	return f.stdout, f.stderr, f.err
}

// SetResult sets the results to be returned for each invocation to the FakePodCommandExecutor
func (f *FakePodCommandExecutor) SetResult(stdout []byte, stderr []byte, err error) {
	f.stdout = stdout
	f.stderr = stderr
	f.err = err
}

// SetResultSequence sets a sequence of results to be returned for each call.
// Results are returned in order; once exhausted, the last result is repeated.
func (f *FakePodCommandExecutor) SetResultSequence(results []ExecResult) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.results = results
	f.resultIndex = 0
}

// GetHistory returns the history of commands executed by the FakePodCommandExecutor
func (f *FakePodCommandExecutor) GetHistory() []Command {
	return f.history
}

// Reset clears the history and resets result sequence index
func (f *FakePodCommandExecutor) Reset() {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.history = nil
	f.resultIndex = 0
}

// NewFakePodCommandExecutor creates a new instance of FakePodCommandExecutor
// with default attributes
func NewFakePodCommandExecutor() *FakePodCommandExecutor {
	return &FakePodCommandExecutor{}
}
