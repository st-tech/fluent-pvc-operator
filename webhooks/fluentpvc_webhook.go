package webhooks

import (
	"context"
	"fmt"
	"net/http"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	podutils "github.com/st-tech/fluent-pvc-operator/utils/pod"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func SetupFluentPVCWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register("/fluent-pvc/validate", &webhook.Admission{Handler: NewFluentPVCValidator(mgr.GetClient())})
	return nil
}

//+kubebuilder:webhook:path=/fluent-pvc/validate,mutating=false,failurePolicy=fail,sideEffects=None,groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs,verbs=create,versions=v1alpha1,name=fluent-pvc-validation-webhook.fluent-pvc-operator.tech.zozo.com,admissionReviewVersions={v1,v1beta1}
//+kubebuilder:rbac:groups="storage.k8s.io",resources=storageclasses,verbs=get;list;watch

type FluentPVCValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func NewFluentPVCValidator(c client.Client) admission.Handler {
	return &FluentPVCValidator{Client: c}
}

func (v *FluentPVCValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := ctrl.LoggerFrom(ctx).WithName("FluentPVCValidator").WithName("Handle")
	fpvc := &fluentpvcv1alpha1.FluentPVC{}

	err := v.decoder.Decode(req, fpvc)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	for _, m := range fpvc.Spec.PVCSpecTemplate.AccessModes {
		if m != corev1.ReadWriteOnce {
			return admission.Denied(fmt.Sprintf("Only 'ReadWriteOnce' is acceptable for FluentPVC.spec.pvcSpecTemplate.accessModes, but '%s' is specified.", fpvc.Spec.PVCSpecTemplate.AccessModes))
		}
	}

	storageClass := &storagev1.StorageClass{}
	if err := v.Client.Get(ctx, client.ObjectKey{Name: *fpvc.Spec.PVCSpecTemplate.StorageClassName}, storageClass); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot Get StorageClass with FluentPVC.Spec.PVCSpecTemplate.StorageClassName: '%s'", *fpvc.Spec.PVCSpecTemplate.StorageClassName))
		return admission.Errored(http.StatusInternalServerError, err)
	}

	j := &batchv1.Job{}
	j.SetName(fpvc.Name)
	j.SetNamespace(corev1.NamespaceDefault)
	j.Spec = *fpvc.Spec.PVCFinalizerJobSpecTemplate.DeepCopy()

	for _, e := range fpvc.Spec.CommonEnvs {
		podutils.InjectOrReplaceEnv(&j.Spec.Template.Spec, e.DeepCopy())
	}

	if err := v.Client.Create(ctx, j, client.DryRunAll); err != nil {
		logger.Error(err, fmt.Sprintf("JobSpec is invalid. FluentPVC Name: '%s'", fpvc.Name))
		return admission.Errored(http.StatusInternalServerError, err)
	}

	pvc := &corev1.PersistentVolumeClaim{}
	pvc.SetName(fpvc.Name)
	pvc.SetNamespace(corev1.NamespaceDefault)
	pvc.Spec = *fpvc.Spec.PVCSpecTemplate.DeepCopy()

	if err := v.Client.Create(ctx, pvc, client.DryRunAll); err != nil {
		logger.Error(err, fmt.Sprintf("PVCSpec is invalid. FluentPVC Name: '%s'", fpvc.Name))
		return admission.Errored(http.StatusInternalServerError, err)
	}

	pod := &corev1.Pod{}
	pod.SetName(fpvc.Name)
	pod.SetNamespace(corev1.NamespaceDefault)
	pod.Spec.Containers = append(pod.Spec.Containers, fpvc.Spec.SidecarContainerTemplate)

	if err := v.Client.Create(ctx, pod, client.DryRunAll); err != nil {
		logger.Error(err, fmt.Sprintf("SidecarContainerSpec is invalid. FluentPVC Name: '%s'", fpvc.Name))
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.Allowed("")
}

func (v *FluentPVCValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
