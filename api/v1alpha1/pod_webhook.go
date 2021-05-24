package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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

	if pod.Annotations == nil || pod.Annotations["fluent-pvc.enabled"] != "true" {
		return admission.Allowed(fmt.Sprintf("Pod: %s is not a target for fluent-pvc.", pod.Name))
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		} else if true {
			// TODO: OwnerReferenceでPVCがWebhookで作られたものかどうか確認する
			return admission.Allowed(fmt.Sprintf("fluent-pvc is already exist for Pod: %s", pod.Name))
		}
	}

	fpvc := &FluentPVC{}
	m.Client.Get(ctx, client.ObjectKey{
		Namespace: "",
		Name:      "fluent-pvc-sample",
	}, fpvc)
	if fpvc.Name == "" {
		return admission.Allowed("FluentPVC custom resource is not exist.")
	}

	pvcName := "fluent-pvc-" + pod.Name + "-" + RandStringRunes(8)

	namespace := pod.Namespace
	if pod.Namespace == "" {
		namespace = corev1.NamespaceDefault
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Labels: map[string]string{
				"fluent-pvc.pod-name": pod.Name,
			},
		},
		Spec: fpvc.Spec.PVCSpecTemplate,
	}

	ctrl.SetControllerReference(fpvc, pvc, m.Client.Scheme())

	podwebhooklog.Info("Creating PVC...")
	err = m.Client.Create(ctx, pvc)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	podwebhooklog.Info("Created PVC %q.\n", pvc.GetObjectMeta().GetName())

	pvcVolume := corev1.Volume{
		Name: fpvc.Spec.PVCVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
			},
		},
	}
	podPatched.Spec.Volumes = append(podPatched.Spec.Volumes, pvcVolume)

	for i := range pod.Spec.Containers {
		podPatched.Spec.Containers[i].VolumeMounts = append(podPatched.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
			Name:      fpvc.Spec.PVCVolumeName,
			MountPath: fpvc.Spec.PVCMountPath,
		})
	}

	podPatched.Spec.Containers = append(podPatched.Spec.Containers, fpvc.Spec.SidecarContainersTemplate...)

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

	if pod.Annotations == nil || pod.Annotations["fluent-pvc.enabled"] != "true" {
		return admission.Allowed(fmt.Sprintf("Pod: %s is not a target for fluent-pvc.", pod.Name))
	}
	// TODO: 下の方でやってる pvc.List() を使う or podのmanifestにPVCが存在するかどうかで処理の有無を決定する
	if pod.Annotations["fluent-pvc.on-create.processed"] != "true" {
		return admission.Allowed(fmt.Sprintf("fluent-pvc is not exist for Pod: %s", pod.Name))
	}

	namespace := pod.Namespace
	if pod.Namespace == "" {
		namespace = corev1.NamespaceDefault
	}

	var pvcList corev1.PersistentVolumeClaimList

	err = v.Client.List(ctx, &pvcList, &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{"fluent-pvc.pod-name": pod.Name}),
	})
	if err != nil {
		podwebhooklog.Error(err, fmt.Sprintf("PersistentVolumeClaims.List() for %s failed.", pod.Name))
		return admission.Allowed("")
	}
	if len(pvcList.Items) == 0 {
		return admission.Allowed(fmt.Sprintf("PVC for %s does not exist.", pod.Name))
	}

	pvcName := pvcList.Items[0].ObjectMeta.Name
	patchPvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"fluent-pvc.deleted": "true",
			},
		},
	}

	err = v.Client.Patch(ctx, patchPvc, client.Apply)

	if err != nil {
		podwebhooklog.Error(err, fmt.Sprintf("Label addition to %s failed.", pvcName))
		return admission.Allowed("")
	}

	return admission.Allowed(fmt.Sprintf("Label addition to %s succeeded.", pvcName))
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
