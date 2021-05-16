package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var fluentpvcoperatorlog = logf.Log.WithName("fluentpvcoperator-resource")

func (r *FluentPVCOperator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-fluent-pvc-operator-tech-zozo-com-v1alpha1-fluentpvcoperator,mutating=true,failurePolicy=fail,sideEffects=None,groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcoperators,verbs=create;update,versions=v1alpha1,name=mfluentpvcoperator.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &FluentPVCOperator{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *FluentPVCOperator) Default() {
	fluentpvcoperatorlog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-fluent-pvc-operator-tech-zozo-com-v1alpha1-fluentpvcoperator,mutating=false,failurePolicy=fail,sideEffects=None,groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcoperators,verbs=create;update,versions=v1alpha1,name=vfluentpvcoperator.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &FluentPVCOperator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *FluentPVCOperator) ValidateCreate() error {
	fluentpvcoperatorlog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *FluentPVCOperator) ValidateUpdate(old runtime.Object) error {
	fluentpvcoperatorlog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *FluentPVCOperator) ValidateDelete() error {
	fluentpvcoperatorlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}
