package disruptors

import (
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/danhngo-lx/xk6-disruptor/pkg/kubernetes"
)

func newMutatingWebhookConfig(name string) *admissionregistrationv1.MutatingWebhookConfiguration {
	fail := admissionregistrationv1.Fail
	timeout := int32(10)
	sideEffects := admissionregistrationv1.SideEffectClassNone
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name:                    "webhook1.example.com",
				FailurePolicy:           &fail,
				TimeoutSeconds:          &timeout,
				SideEffects:             &sideEffects,
				AdmissionReviewVersions: []string{"v1"},
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					CABundle: []byte("original-ca-bundle"),
				},
			},
		},
	}
}

func TestNewWebhookDisruptor(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when target exists", func(t *testing.T) {
		t.Parallel()
		client := fake.NewSimpleClientset(newMutatingWebhookConfig("my-webhook"))
		k8s, _ := kubernetes.NewFakeKubernetes(client)

		_, err := NewWebhookDisruptor(t.Context(), k8s, WebhookKindMutating, "my-webhook")
		if err != nil {
			t.Fatalf("NewWebhookDisruptor: %v", err)
		}
	})

	t.Run("fails when target missing", func(t *testing.T) {
		t.Parallel()
		client := fake.NewSimpleClientset()
		k8s, _ := kubernetes.NewFakeKubernetes(client)

		_, err := NewWebhookDisruptor(t.Context(), k8s, WebhookKindMutating, "ghost")
		if err == nil {
			t.Error("expected error for missing target")
		}
	})

	t.Run("fails for unsupported kind", func(t *testing.T) {
		t.Parallel()
		client := fake.NewSimpleClientset()
		k8s, _ := kubernetes.NewFakeKubernetes(client)

		_, err := NewWebhookDisruptor(t.Context(), k8s, "Bogus", "my-webhook")
		if err == nil {
			t.Error("expected error for unsupported kind")
		}
	})
}

func TestWebhookDisruptor_InjectWebhookConfigFault(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(newMutatingWebhookConfig("my-webhook"))
	k8s, _ := kubernetes.NewFakeKubernetes(client)

	d, err := NewWebhookDisruptor(t.Context(), k8s, WebhookKindMutating, "my-webhook")
	if err != nil {
		t.Fatalf("NewWebhookDisruptor: %v", err)
	}

	fault := WebhookConfigFault{
		FailurePolicy:      string(admissionregistrationv1.Ignore),
		TimeoutSeconds:     5,
		InvalidateCABundle: true,
	}

	err = d.InjectWebhookConfigFault(t.Context(), fault, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("InjectWebhookConfigFault: %v", err)
	}

	restored, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().
		Get(t.Context(), "my-webhook", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting restored config: %v", err)
	}

	w := restored.Webhooks[0]
	if *w.FailurePolicy != admissionregistrationv1.Fail {
		t.Errorf("expected failurePolicy restored to %q, got %q", admissionregistrationv1.Fail, *w.FailurePolicy)
	}
	if *w.TimeoutSeconds != 10 {
		t.Errorf("expected timeoutSeconds restored to 10, got %d", *w.TimeoutSeconds)
	}
	if string(w.ClientConfig.CABundle) != "original-ca-bundle" {
		t.Errorf("expected caBundle restored to original, got %q", w.ClientConfig.CABundle)
	}
}

func TestWebhookConfigFault_Validate(t *testing.T) {
	t.Parallel()

	if err := (WebhookConfigFault{}).Validate(); err == nil {
		t.Error("expected error for empty fault")
	}

	if err := (WebhookConfigFault{FailurePolicy: "Bogus"}).Validate(); err == nil {
		t.Error("expected error for invalid failurePolicy")
	}

	if err := (WebhookConfigFault{FailurePolicy: string(admissionregistrationv1.Ignore)}).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
