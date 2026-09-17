package kubernetes

import (
	"context"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes/helpers"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// FakeKubernetes is a fake implementation of the Kubernetes interface
type FakeKubernetes struct {
	client   *fake.Clientset
	dynamic  dynamic.Interface
	ctx      context.Context
	executor *helpers.FakePodCommandExecutor
}

// NewFakeKubernetes returns a new fake implementation of Kubernetes from fake Clientset
func NewFakeKubernetes(clientset *fake.Clientset) (*FakeKubernetes, error) {
	return &FakeKubernetes{
		client:   clientset,
		dynamic:  dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
		ctx:      context.TODO(),
		executor: helpers.NewFakePodCommandExecutor(),
	}, nil
}

// NewFakeKubernetesWithDynamicObjects returns a fake Kubernetes whose dynamic client is
// pre-seeded with the given unstructured objects (e.g. Istio CRDs), for tests that need
// DynamicClient() to resolve pre-existing resources.
func NewFakeKubernetesWithDynamicObjects(clientset *fake.Clientset, objects ...runtime.Object) (*FakeKubernetes, error) {
	return &FakeKubernetes{
		client:   clientset,
		dynamic:  dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...),
		ctx:      context.TODO(),
		executor: helpers.NewFakePodCommandExecutor(),
	}, nil
}

// PodHelper returns a PodHelper for the given namespace
func (f *FakeKubernetes) PodHelper(namespace string) helpers.PodHelper {
	return helpers.NewPodHelper(
		f.client,
		f.executor,
		namespace,
	)
}

// ServiceHelper returns a ServiceHelper for the given namespace
func (f *FakeKubernetes) ServiceHelper(namespace string) helpers.ServiceHelper {
	return helpers.NewServiceHelper(
		f.client,
		namespace,
	)
}

// Client return a kubernetes client
func (f *FakeKubernetes) Client() kubernetes.Interface {
	return f.client
}

// DynamicClient returns the fake dynamic client
func (f *FakeKubernetes) DynamicClient() dynamic.Interface {
	return f.dynamic
}

// NodeHelper returns a NodeHelper backed by the fake client
func (f *FakeKubernetes) NodeHelper() helpers.NodeHelper {
	return helpers.NewNodeHelper(f.client)
}

// WorkloadHelper returns a WorkloadHelper backed by the fake client
func (f *FakeKubernetes) WorkloadHelper() helpers.WorkloadHelper {
	return helpers.NewWorkloadHelper(f.client)
}

// GetFakeProcessExecutor returns the FakeProcessExecutor used by the helpers to mock
// the execution of commands in a Pod
func (f *FakeKubernetes) GetFakeProcessExecutor() *helpers.FakePodCommandExecutor {
	return f.executor
}
