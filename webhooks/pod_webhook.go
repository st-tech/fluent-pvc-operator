package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
)

var podwebhooklog = logf.Log.WithName("pod-webhook")

//+kubebuilder:webhook:path=/mutate-on-creation-core-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,verbs=create,versions=v1,name=mpod.kb.io,admissionReviewVersions={v1,v1beta1}
//+kubebuilder:webhook:path=/validate-core-v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,verbs=create,versions=v1,name=vpod.kb.io,admissionReviewVersions={v1,v1beta1}
//+kubebuilder:webhook:path=/mutate-on-deletion-core-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,verbs=delete,versions=v1,name=mdpod.kb.io,admissionReviewVersions={v1,v1beta1}

func PodAdmissionResponse(pod *corev1.Pod, req admission.Request) admission.Response {
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func isOwnerFluentPVC(owner *metav1.OwnerReference) bool {
	return owner != nil &&
		owner.APIVersion == fluentpvcv1alpha1.GroupVersion.String() &&
		owner.Kind == "FluentPVC"
}

func SetupPodWebhookWithManager(mgr ctrl.Manager) error {
	pv := NewPodValidator(mgr.GetClient())
	mgr.GetWebhookServer().Register("/validate-core-v1-pod", &webhook.Admission{Handler: pv})
	pmc := NewPodOnCreationMutator(mgr.GetClient())
	mgr.GetWebhookServer().Register("/mutate-on-creation-core-v1-pod", &webhook.Admission{Handler: pmc})
	pmd := NewPodOnDeletionMutator(mgr.GetClient())
	mgr.GetWebhookServer().Register("/mutate-on-deletion-core-v1-pod", &webhook.Admission{Handler: pmd})
	return nil
}

type podMutator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func NewPodOnCreationMutator(c client.Client) admission.Handler {
	return &podMutator{Client: c}
}

func (m *podMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	err := m.decoder.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	podPatched := pod.DeepCopy()

	if _, ok := pod.Annotations["fluent-pvc-operator.tech.zozo.com/fluent-pvc-name"]; !ok {
		return admission.Allowed(fmt.Sprintf("Pod: %s is not a target for fluent-pvc.", pod.Name))
	}

	namespace := pod.Namespace
	if pod.Namespace == "" {
		namespace = corev1.NamespaceDefault
	}

	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}
		pvc := &corev1.PersistentVolumeClaim{}
		err = m.Client.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      volume.PersistentVolumeClaim.ClaimName,
		}, pvc)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		if isOwnerFluentPVC(metav1.GetControllerOf(pvc)) {
			return admission.Allowed(fmt.Sprintf("fluent-pvc is already exist for Pod: %s", pod.Name))
		}
	}

	// TODO: Consider too long pod name
	pvcName := "fluent-pvc-" + pod.Name + "-" + fluentpvcv1alpha1.RandStringRunes(8)

	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	err = m.Client.Get(ctx, client.ObjectKey{
		Name: pod.Annotations["fluent-pvc-operator.tech.zozo.com/fluent-pvc-name"],
	}, fpvc)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Annotations: map[string]string{
				"fluent-pvc-operator.tech.zozo.com/dedicated-pod": pod.Name,
			},
		},
		Spec: fpvc.Spec.PVCSpecTemplate,
	}

	err = ctrl.SetControllerReference(fpvc, pvc, m.Client.Scheme())
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	podwebhooklog.Info("Creating PVC...")
	err = m.Client.Create(ctx, pvc)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	podwebhooklog.Info(fmt.Sprintf("Created PVC %q.\n", pvc.GetObjectMeta().GetName()))

	pvcVolume := corev1.Volume{
		Name: fpvc.Spec.PVCVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
			},
		},
	}
	podPatched.Spec.Volumes = append(podPatched.Spec.Volumes, pvcVolume)

	podPatched.Spec.Containers = append(podPatched.Spec.Containers, fpvc.Spec.SidecarContainersTemplate...)

	for i := range pod.Spec.Containers {
		podPatched.Spec.Containers[i].VolumeMounts = append(podPatched.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
			Name:      fpvc.Spec.PVCVolumeName,
			MountPath: fpvc.Spec.PVCMountPath,
		})
		podPatched.Spec.Containers[i].Env = fpvc.Spec.CommonEnv
	}

	return PodAdmissionResponse(podPatched, req)
}
func (m *podMutator) InjectDecoder(d *admission.Decoder) error {
	m.decoder = d
	return nil
}

type podMutatorOnDelete struct {
	Client  client.Client
	decoder *admission.Decoder
}

func NewPodOnDeletionMutator(c client.Client) admission.Handler {
	return &podMutatorOnDelete{Client: c}
}

func (v *podMutatorOnDelete) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}

	err := v.decoder.DecodeRaw(req.OldObject, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if _, ok := pod.Annotations["fluent-pvc-operator.tech.zozo.com/fluent-pvc-name"]; !ok {
		return admission.Allowed(fmt.Sprintf("Pod: %s is not a target for fluent-pvc.", pod.Name))
	}

	namespace := pod.Namespace
	if pod.Namespace == "" {
		namespace = corev1.NamespaceDefault
	}

	pvc := &corev1.PersistentVolumeClaim{}

	isPvcExist := false
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}
		refPvc := &corev1.PersistentVolumeClaim{}
		err = v.Client.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      volume.PersistentVolumeClaim.ClaimName,
		}, refPvc)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		if isOwnerFluentPVC(metav1.GetControllerOf(refPvc)) {
			isPvcExist = true
			pvc = refPvc
			break
		}
	}
	if !isPvcExist {
		return admission.Allowed(fmt.Sprintf("fluent-pvc is not exist for Pod: %s", pod.Name))
	}

	patchPvc := &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			Kind:       pvc.Kind,
			APIVersion: pvc.APIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        pvc.Name,
			Namespace:   pvc.Namespace,
			Annotations: map[string]string{"fluent-pvc-operator.tech.zozo.com/out-of-use": "true"},
		},
	}

	err = v.Client.Patch(ctx, patchPvc, client.Apply, &client.PatchOptions{
		FieldManager: "fluent-pvc-operator",
	})
	if err != nil {
		podwebhooklog.Error(err, fmt.Sprintf("Label addition to %s failed.", pvc.Name))
		return admission.Allowed("")
	}

	return admission.Allowed(fmt.Sprintf("Label addition to %s succeeded.", pvc.Name))
}

func (v *podMutatorOnDelete) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
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

	key := "example-mutating-admission-webhook"
	anno, found := pod.Annotations[key]
	if !found {
		return admission.Denied(fmt.Sprintf("missing annotation %s", key))
	}
	if anno != "foo" {
		return admission.Denied(fmt.Sprintf("annotation %s did not have value %q", key, "foo"))
	}

	return admission.Allowed("")
}

func (v *podValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
