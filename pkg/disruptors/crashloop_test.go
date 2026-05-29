package disruptors

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"
	"github.com/danhngo-lx/xk6-disruptor/pkg/testutils/kubernetes/builders"

	corev1 "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func buildRunningPod(name, namespace, containerName string) corev1.Pod {
	container := builders.NewContainerBuilder(containerName).Build()

	pod := builders.NewPodBuilder(name).
		WithNamespace(namespace).
		WithContainer(container).
		WithIP("192.0.2.6").
		Build()

	// Set pod phase to Running
	pod.Status.Phase = corev1.PodRunning
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: containerName,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{},
			},
			Ready: true,
		},
	}

	return pod
}

func Test_InjectCrashLoopFault(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		namespace     string
		pods          []corev1.Pod
		selectorSpec  PodSelectorSpec
		fault         CrashLoopFault
		duration      time.Duration
		execResults   []helpers.ExecResult
		expectError   bool
		errorContains string
		// minKillCalls is the minimum expected number of kill command calls
		minKillCalls int
	}{
		{
			name:      "successful crash loop with count limit",
			namespace: "test-ns",
			pods: []corev1.Pod{
				buildRunningPod("pod-1", "test-ns", "app"),
			},
			selectorSpec: PodSelectorSpec{
				Namespace: "test-ns",
				Select:    PodAttributes{Labels: map[string]string{"app": "test"}},
			},
			fault: CrashLoopFault{
				Container: "app",
				Count:     3,
			},
			duration: 30 * time.Second,
			execResults: []helpers.ExecResult{
				{Stdout: nil, Stderr: nil, Err: nil}, // successful kill
			},
			expectError:  false,
			minKillCalls: 3,
		},
		{
			name:      "no matching pods returns error",
			namespace: "test-ns",
			pods:      []corev1.Pod{}, // no pods
			selectorSpec: PodSelectorSpec{
				Namespace: "test-ns",
				Select:    PodAttributes{Labels: map[string]string{"app": "test"}},
			},
			fault: CrashLoopFault{
				Container: "app",
				Count:     1,
			},
			duration:      10 * time.Second,
			execResults:   nil,
			expectError:   true,
			errorContains: "no pods found",
			minKillCalls:  0,
		},
		{
			name:      "consecutive kill failures returns error",
			namespace: "test-ns",
			pods: []corev1.Pod{
				buildRunningPod("pod-1", "test-ns", "app"),
			},
			selectorSpec: PodSelectorSpec{
				Namespace: "test-ns",
				Select:    PodAttributes{Labels: map[string]string{"app": "test"}},
			},
			fault: CrashLoopFault{
				Container: "app",
				Count:     10,
			},
			duration: 30 * time.Second,
			execResults: []helpers.ExecResult{
				// 5 consecutive failures should trigger error
				{Stderr: []byte("kill: permission denied"), Err: errors.New("exec failed")},
				{Stderr: []byte("kill: permission denied"), Err: errors.New("exec failed")},
				{Stderr: []byte("kill: permission denied"), Err: errors.New("exec failed")},
				{Stderr: []byte("kill: permission denied"), Err: errors.New("exec failed")},
				{Stderr: []byte("kill: permission denied"), Err: errors.New("exec failed")},
			},
			expectError:   true,
			errorContains: "failed to kill process",
			minKillCalls:  5,
		},
		{
			name:      "intermittent failures are tolerated",
			namespace: "test-ns",
			pods: []corev1.Pod{
				buildRunningPod("pod-1", "test-ns", "app"),
			},
			selectorSpec: PodSelectorSpec{
				Namespace: "test-ns",
				Select:    PodAttributes{Labels: map[string]string{"app": "test"}},
			},
			fault: CrashLoopFault{
				Container: "app",
				Count:     3,
			},
			duration: 30 * time.Second,
			execResults: []helpers.ExecResult{
				{Stdout: nil, Stderr: nil, Err: nil},                             // success
				{Stderr: []byte("container restarting"), Err: errors.New("err")}, // fail (container restarting)
				{Stdout: nil, Stderr: nil, Err: nil},                             // success after retry
				{Stdout: nil, Stderr: nil, Err: nil},                             // success
			},
			expectError:  false,
			minKillCalls: 3,
		},
		{
			name:      "no successful kills returns error",
			namespace: "test-ns",
			pods: []corev1.Pod{
				buildRunningPod("pod-1", "test-ns", "app"),
			},
			selectorSpec: PodSelectorSpec{
				Namespace: "test-ns",
				Select:    PodAttributes{Labels: map[string]string{"app": "test"}},
			},
			fault: CrashLoopFault{
				Container: "app",
				Count:     10,
			},
			duration: 30 * time.Second, // longer duration to allow 5 retries (2s each)
			execResults: []helpers.ExecResult{
				{Stderr: []byte("kill not found"), Err: errors.New("command not found")},
				{Stderr: []byte("kill not found"), Err: errors.New("command not found")},
				{Stderr: []byte("kill not found"), Err: errors.New("command not found")},
				{Stderr: []byte("kill not found"), Err: errors.New("command not found")},
				{Stderr: []byte("kill not found"), Err: errors.New("command not found")},
			},
			expectError:   true,
			errorContains: "failed to kill",
			minKillCalls:  5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Add labels to pods to match selector
			for i := range tc.pods {
				if tc.pods[i].Labels == nil {
					tc.pods[i].Labels = make(map[string]string)
				}
				for k, v := range tc.selectorSpec.Select.Labels {
					tc.pods[i].Labels[k] = v
				}
			}

			// Create fake kubernetes client with pods
			var objects []runtime.Object
			for i := range tc.pods {
				objects = append(objects, &tc.pods[i])
			}
			client := fake.NewSimpleClientset(objects...)

			k8s, err := kubernetes.NewFakeKubernetes(client)
			if err != nil {
				t.Fatalf("failed to create fake kubernetes: %v", err)
			}

			// Configure exec results
			executor := k8s.GetFakeProcessExecutor()
			if len(tc.execResults) > 0 {
				executor.SetResultSequence(tc.execResults)
			}

			// Create pod disruptor
			disruptor, err := NewPodDisruptor(
				context.Background(),
				k8s,
				tc.selectorSpec,
				PodDisruptorOptions{InjectTimeout: -1 * time.Second},
			)
			if err != nil {
				t.Fatalf("failed to create pod disruptor: %v", err)
			}

			// Inject crash loop fault
			ctx, cancel := context.WithTimeout(context.Background(), tc.duration+5*time.Second)
			defer cancel()

			err = disruptor.InjectCrashLoopFault(ctx, tc.fault, tc.duration)

			// Check error expectations
			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if tc.errorContains != "" && !strings.Contains(err.Error(), tc.errorContains) {
					t.Errorf("expected error containing %q, got: %v", tc.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			// Verify kill commands were called
			history := executor.GetHistory()
			killCalls := 0
			for _, cmd := range history {
				if len(cmd.Command) >= 3 && cmd.Command[0] == "kill" {
					killCalls++
				}
			}

			if killCalls < tc.minKillCalls {
				t.Errorf("expected at least %d kill calls, got %d", tc.minKillCalls, killCalls)
			}
		})
	}
}

func Test_CrashLoopFault_ContextCancellation(t *testing.T) {
	t.Parallel()

	pod := buildRunningPod("pod-1", "test-ns", "app")
	pod.Labels = map[string]string{"app": "test"}

	client := fake.NewSimpleClientset(&pod)
	k8s, _ := kubernetes.NewFakeKubernetes(client)

	// Set up successful exec result
	executor := k8s.GetFakeProcessExecutor()
	executor.SetResult(nil, nil, nil)

	disruptor, err := NewPodDisruptor(
		context.Background(),
		k8s,
		PodSelectorSpec{
			Namespace: "test-ns",
			Select:    PodAttributes{Labels: map[string]string{"app": "test"}},
		},
		PodDisruptorOptions{InjectTimeout: -1 * time.Second},
	)
	if err != nil {
		t.Fatalf("failed to create pod disruptor: %v", err)
	}

	// Create a context that gets cancelled quickly
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// This should return without error when context is cancelled
	err = disruptor.InjectCrashLoopFault(ctx, CrashLoopFault{Container: "app", Count: 100}, 1*time.Minute)
	if err != nil {
		t.Errorf("expected no error on context cancellation, got: %v", err)
	}
}

func Test_CrashLoopFault_MultiplePods(t *testing.T) {
	t.Parallel()

	pods := []corev1.Pod{
		buildRunningPod("pod-1", "test-ns", "app"),
		buildRunningPod("pod-2", "test-ns", "app"),
		buildRunningPod("pod-3", "test-ns", "app"),
	}

	// Add labels to match selector
	for i := range pods {
		pods[i].Labels = map[string]string{"app": "test"}
	}

	var objects []runtime.Object
	for i := range pods {
		objects = append(objects, &pods[i])
	}
	client := fake.NewSimpleClientset(objects...)

	k8s, _ := kubernetes.NewFakeKubernetes(client)

	// Set up successful exec result
	executor := k8s.GetFakeProcessExecutor()
	executor.SetResult(nil, nil, nil)

	disruptor, err := NewPodDisruptor(
		context.Background(),
		k8s,
		PodSelectorSpec{
			Namespace: "test-ns",
			Select:    PodAttributes{Labels: map[string]string{"app": "test"}},
		},
		PodDisruptorOptions{InjectTimeout: -1 * time.Second},
	)
	if err != nil {
		t.Fatalf("failed to create pod disruptor: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Kill each pod's container once
	err = disruptor.InjectCrashLoopFault(ctx, CrashLoopFault{Container: "app", Count: 1}, 5*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify kill was called for each pod
	history := executor.GetHistory()
	podKills := make(map[string]int)
	for _, cmd := range history {
		if len(cmd.Command) >= 3 && cmd.Command[0] == "kill" {
			podKills[cmd.Pod]++
		}
	}

	// Each of the 3 pods should have at least 1 kill call
	if len(podKills) != 3 {
		t.Errorf("expected kills on 3 pods, got %d pods: %v", len(podKills), podKills)
	}
}
