package disruptors

import (
	"context"
	"fmt"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	xk6kubernetes "github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
)

// Supported webhook configuration kinds.
const (
	WebhookKindMutating   = "Mutating"
	WebhookKindValidating = "Validating"
)

// WebhookConfigFault specifies a fault to apply to every webhook entry of a
// MutatingWebhookConfiguration or ValidatingWebhookConfiguration. Zero-value
// fields are left unchanged. The original configuration is restored once the
// fault duration elapses.
type WebhookConfigFault struct {
	// FailurePolicy overrides every entry's failurePolicy ("Ignore" or "Fail").
	// Empty leaves it unchanged.
	FailurePolicy string `js:"failurePolicy"`
	// TimeoutSeconds overrides every entry's timeoutSeconds (1-30). 0 leaves it unchanged.
	TimeoutSeconds int32 `js:"timeoutSeconds"`
	// InvalidateCABundle replaces every entry's caBundle with garbage bytes, causing the
	// API server to fail TLS verification when calling the webhook.
	InvalidateCABundle bool `js:"invalidateCABundle"`
}

// Validate checks that the fault specifies at least one change to apply.
func (f WebhookConfigFault) Validate() error {
	if f.FailurePolicy == "" && f.TimeoutSeconds == 0 && !f.InvalidateCABundle {
		return fmt.Errorf("at least one of failurePolicy, timeoutSeconds, or invalidateCABundle must be set")
	}
	if f.FailurePolicy != "" &&
		f.FailurePolicy != string(admissionregistrationv1.Ignore) &&
		f.FailurePolicy != string(admissionregistrationv1.Fail) {
		return fmt.Errorf("failurePolicy must be %q or %q", admissionregistrationv1.Ignore, admissionregistrationv1.Fail)
	}
	return nil
}

// WebhookConfigFaultInjector defines the interface for injecting faults into an
// admission webhook configuration.
type WebhookConfigFaultInjector interface {
	InjectWebhookConfigFault(ctx context.Context, fault WebhookConfigFault, duration time.Duration) error
}

// WebhookDisruptor defines the methods for injecting chaos into a single
// MutatingWebhookConfiguration or ValidatingWebhookConfiguration. It is a
// cluster-scoped, pure-API disruptor: it never execs into a target pod.
type WebhookDisruptor interface {
	Disruptor
	WebhookConfigFaultInjector
}

// webhookDisruptor is the concrete implementation of WebhookDisruptor.
type webhookDisruptor struct {
	client kubernetes.Interface
	kind   string
	name   string
}

// NewWebhookDisruptor creates a WebhookDisruptor targeting the named
// MutatingWebhookConfiguration or ValidatingWebhookConfiguration.
func NewWebhookDisruptor(
	ctx context.Context,
	k8s xk6kubernetes.Kubernetes,
	kind string,
	name string,
) (WebhookDisruptor, error) {
	if kind != WebhookKindMutating && kind != WebhookKindValidating {
		return nil, fmt.Errorf("unsupported webhook kind %q (supported: %q, %q)", kind, WebhookKindMutating, WebhookKindValidating)
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	d := &webhookDisruptor{
		client: k8s.Client(),
		kind:   kind,
		name:   name,
	}

	// Eagerly resolve to fail fast if the target does not exist.
	if _, err := d.get(ctx); err != nil {
		return nil, fmt.Errorf("getting %s webhook configuration %q: %w", kind, name, err)
	}

	return d, nil
}

// Targets returns the single target as "<Kind>/<name>".
func (d *webhookDisruptor) Targets(_ context.Context) ([]string, error) {
	return []string{d.kind + "/" + d.name}, nil
}

// TargetIPs is not meaningful for a webhook configuration. Returns an empty slice.
func (d *webhookDisruptor) TargetIPs(_ context.Context) ([]string, error) {
	return []string{}, nil
}

// Cleanup is a no-op: InjectWebhookConfigFault always restores the original
// configuration before returning, mirroring NodeDisruptor's Drain/TaintNode.
func (d *webhookDisruptor) Cleanup(_ context.Context) error {
	return nil
}

// get fetches the current webhook configuration, used only to fail fast on construction.
func (d *webhookDisruptor) get(ctx context.Context) (any, error) {
	switch d.kind {
	case WebhookKindMutating:
		return d.client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, d.name, metav1.GetOptions{})
	default:
		return d.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, d.name, metav1.GetOptions{})
	}
}

var garbageCABundle = []byte("xk6-disruptor-invalidated-ca-bundle")

func applyWebhookFault(failurePolicy *admissionregistrationv1.FailurePolicyType, timeoutSeconds *int32, caBundle *[]byte, fault WebhookConfigFault) {
	if fault.FailurePolicy != "" {
		policy := admissionregistrationv1.FailurePolicyType(fault.FailurePolicy)
		*failurePolicy = policy
	}
	if fault.TimeoutSeconds != 0 {
		*timeoutSeconds = fault.TimeoutSeconds
	}
	if fault.InvalidateCABundle {
		*caBundle = garbageCABundle
	}
}

// InjectWebhookConfigFault patches every webhook entry in the target configuration,
// waits for duration (or context cancellation), then restores the original
// configuration.
func (d *webhookDisruptor) InjectWebhookConfigFault(
	ctx context.Context,
	fault WebhookConfigFault,
	duration time.Duration,
) error {
	if err := fault.Validate(); err != nil {
		return err
	}

	switch d.kind {
	case WebhookKindMutating:
		return d.injectMutating(ctx, fault, duration)
	default:
		return d.injectValidating(ctx, fault, duration)
	}
}

func (d *webhookDisruptor) injectMutating(ctx context.Context, fault WebhookConfigFault, duration time.Duration) error {
	client := d.client.AdmissionregistrationV1().MutatingWebhookConfigurations()

	original, err := client.Get(ctx, d.name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting mutating webhook configuration: %w", err)
	}
	restore := original.DeepCopy()

	patched := original.DeepCopy()
	for i := range patched.Webhooks {
		w := &patched.Webhooks[i]
		if w.FailurePolicy == nil {
			ignore := admissionregistrationv1.Ignore
			w.FailurePolicy = &ignore
		}
		if w.TimeoutSeconds == nil {
			var zero int32
			w.TimeoutSeconds = &zero
		}
		applyWebhookFault(w.FailurePolicy, w.TimeoutSeconds, &w.ClientConfig.CABundle, fault)
	}
	if _, err := client.Update(ctx, patched, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("patching mutating webhook configuration: %w", err)
	}

	waitOrCancel(ctx, duration)

	restore.ResourceVersion = ""
	if latest, err := client.Get(ctx, d.name, metav1.GetOptions{}); err == nil {
		restore.ResourceVersion = latest.ResourceVersion
	}
	if _, err := client.Update(context.Background(), restore, metav1.UpdateOptions{}); err != nil { //nolint:contextcheck
		return fmt.Errorf("restoring mutating webhook configuration: %w", err)
	}
	return nil
}

func (d *webhookDisruptor) injectValidating(ctx context.Context, fault WebhookConfigFault, duration time.Duration) error {
	client := d.client.AdmissionregistrationV1().ValidatingWebhookConfigurations()

	original, err := client.Get(ctx, d.name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting validating webhook configuration: %w", err)
	}
	restore := original.DeepCopy()

	patched := original.DeepCopy()
	for i := range patched.Webhooks {
		w := &patched.Webhooks[i]
		if w.FailurePolicy == nil {
			ignore := admissionregistrationv1.Ignore
			w.FailurePolicy = &ignore
		}
		if w.TimeoutSeconds == nil {
			var zero int32
			w.TimeoutSeconds = &zero
		}
		applyWebhookFault(w.FailurePolicy, w.TimeoutSeconds, &w.ClientConfig.CABundle, fault)
	}
	if _, err := client.Update(ctx, patched, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("patching validating webhook configuration: %w", err)
	}

	waitOrCancel(ctx, duration)

	restore.ResourceVersion = ""
	if latest, err := client.Get(ctx, d.name, metav1.GetOptions{}); err == nil {
		restore.ResourceVersion = latest.ResourceVersion
	}
	if _, err := client.Update(context.Background(), restore, metav1.UpdateOptions{}); err != nil { //nolint:contextcheck
		return fmt.Errorf("restoring validating webhook configuration: %w", err)
	}
	return nil
}

// waitOrCancel blocks until duration elapses or ctx is done, whichever comes first.
func waitOrCancel(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
