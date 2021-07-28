package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
	hashutils "github.com/st-tech/fluent-pvc-operator/utils/hash"
	podutils "github.com/st-tech/fluent-pvc-operator/utils/pod"
)

//+kubebuilder:webhook:path=/pod/mutate,mutating=true,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,verbs=create,versions=v1,name=pod-mutation-webhook.fluent-pvc-operator.tech.zozo.com,admissionReviewVersions={v1,v1beta1}
//+kubebuilder:webhook:path=/pod/validate,mutating=false,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,verbs=create,versions=v1,name=pod-validation-webhook.fluent-pvc-operator.tech.zozo.com,admissionReviewVersions={v1,v1beta1}

//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;delete

func PodAdmissionResponse(pod *corev1.Pod, req admission.Request) admission.Response {
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func SetupPodWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register("/pod/validate", &webhook.Admission{Handler: NewPodValidator(mgr.GetClient())})
	mgr.GetWebhookServer().Register("/pod/mutate", &webhook.Admission{Handler: NewPodMutator(mgr.GetClient())})
	return nil
}

type podMutator struct {
	client.Client
	decoder *admission.Decoder
}

func NewPodMutator(c client.Client) admission.Handler {
	return &podMutator{Client: c}
}

func (m *podMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := ctrl.LoggerFrom(ctx).WithName("podMutator").WithName("Handle")
	pod := &corev1.Pod{}
	if err := m.decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if len(pod.Spec.Containers) == 0 {
		return admission.Denied("pod has no containers")
	}
	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	if n, ok := pod.Labels[constants.PodLabelFluentPVCName]; !ok {
		return admission.Denied(fmt.Sprintf("pod does not have %s label.", constants.PodLabelFluentPVCName))
	} else {
		if err := m.Get(ctx, client.ObjectKey{Name: n}, fpvc); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
	}

	// TODO: Consider too long fluent-pvc name
	collisionCount := int32(rand.IntnRange(math.MinInt32, math.MaxInt32)) // Using the count for collision avoidance
	name := fmt.Sprintf(
		"%s-%s-%s",
		fpvc.Name, hashutils.ComputeHash(fpvc, nil), hashutils.ComputeHash(pod, &collisionCount),
	)

	logger.Info(fmt.Sprintf("Create PVC='%s'(namespace='%s').", name, req.Namespace))
	pvc := &corev1.PersistentVolumeClaim{}
	pvc.SetName(name)
	pvc.SetNamespace(req.Namespace)
	pvc.Spec = *fpvc.Spec.PVCSpecTemplate.DeepCopy()
	controllerutil.AddFinalizer(pvc, constants.PVCFinalizerName)
	// NOTE: fluentpvcbinding does not own pvc for preventing pvc from becoming terminating when fluentpvcbinding
	//       is deleted. This is because the finalizer job cannot mount the pvc if it is terminating.
	if err := m.Create(ctx, pvc, &client.CreateOptions{}); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot Create PVC='%s'(namespace='%s').", name, req.Namespace))
		return admission.Errored(http.StatusInternalServerError, err)
	}

	logger.Info(fmt.Sprintf("Create FluentPVCBinding='%s'(namespace='%s').", name, req.Namespace))
	b := &fluentpvcv1alpha1.FluentPVCBinding{}
	b.SetName(name)
	b.SetNamespace(req.Namespace)
	b.SetFluentPVC(fpvc)
	b.SetPod(pod)
	b.SetPVC(pvc)
	controllerutil.AddFinalizer(b, constants.FluentPVCBindingFinalizerName)
	if err := ctrl.SetControllerReference(fpvc, b, m.Scheme()); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot set FluentPVC as a Controller OwnerReference on owned for FluentPVCBinding='%s'.", name))
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if err := m.Create(ctx, b, &client.CreateOptions{}); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot Create FluentPVCBinding='%s'.", name))
		return admission.Errored(http.StatusInternalServerError, err)
	}
	b.SetPhasePending()
	if err := m.Status().Update(ctx, b); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot update the status of FluentPVCBinding='%s'.", name))
		return admission.Errored(http.StatusInternalServerError, err)
	}

	logger.Info(fmt.Sprintf(
		"Inject PVC='%s' into Pod='%s'(namespace='%s', generatorName='%s').",
		name, pod.Name, req.Namespace, pod.GenerateName,
	))
	podPatched := pod.DeepCopy()
	if podPatched.Labels == nil {
		podPatched.Labels = map[string]string{}
	}
	podPatched.Labels[constants.PodLabelFluentPVCBindingName] = name
	for _, v := range fpvc.Spec.CommonVolumes {
		podutils.InjectOrReplaceVolume(&podPatched.Spec, v.DeepCopy())
	}
	podutils.InjectOrReplaceVolume(&podPatched.Spec, &corev1.Volume{
		Name: fpvc.Spec.PVCVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: name,
			},
		},
	})
	podutils.InjectOrReplaceContainer(&podPatched.Spec, fpvc.Spec.SidecarContainerTemplate.DeepCopy())
	for _, vm := range fpvc.Spec.CommonVolumeMounts {
		podutils.InjectOrReplaceVolumeMount(&podPatched.Spec, vm.DeepCopy())
	}
	podutils.InjectOrReplaceVolumeMount(&podPatched.Spec, &corev1.VolumeMount{
		Name:      fpvc.Spec.PVCVolumeName,
		MountPath: fpvc.Spec.PVCVolumeMountPath,
	})
	for _, e := range fpvc.Spec.CommonEnvs {
		podutils.InjectOrReplaceEnv(&podPatched.Spec, e.DeepCopy())
	}

	logger.Info(fmt.Sprintf(
		"Patch Pod='%s'(namespace='%s', generatorName='%s') with PVC='%s' and FluentPVCBinding='%s' by FluentPVC='%s'.",
		podPatched.Name, req.Namespace, podPatched.GenerateName, pvc.Name, b.Name, fpvc.Name,
	))
	return PodAdmissionResponse(podPatched, req)
}

func (m *podMutator) InjectDecoder(d *admission.Decoder) error {
	m.decoder = d
	return nil
}

type podValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func NewPodValidator(c client.Client) admission.Handler {
	return &podValidator{Client: c}
}

func (v *podValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}

	err := v.decoder.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// TODO: implement
	// key := "example-mutating-admission-webhook"
	// anno, found := pod.Annotations[key]
	// if !found {
	// 	return admission.Denied(fmt.Sprintf("missing annotation %s", key))
	// }
	// if anno != "foo" {
	// 	return admission.Denied(fmt.Sprintf("annotation %s did not have value %q", key, "foo"))
	// }

	return admission.Allowed("")
}

func (v *podValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
