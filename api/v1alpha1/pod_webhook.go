package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

	if pod.Annotations == nil {
		podPatched.Annotations = map[string]string{}
	}
	if pod.Annotations["fluent-pvc.enabled"] != "true" {
		return admission.Allowed(fmt.Sprintf("Pod: %s is not a target for fluent-pvc.", pod.Name))
	}
	if pod.Annotations["fluent-pvc.on-create.processed"] == "true" {
		return admission.Allowed(fmt.Sprintf("fluent-pvc is already exist for Pod: %s", pod.Name))
	}
	if pod.Annotations["fluent-pvc.storage-size"] == "" {
		// TODO: Fetch default storage size from FluentPVCOperator
		// Set default storage size if pod annotation is not exist
		podPatched.Annotations["fluent-pvc.storage-size"] = "8Gi"
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// TODO: Fix storage class name to custom storage class
	storageClassName := "standard"
	pvcName := "fluent-pvc-" + pod.Name + "-" + RandStringRunes(8)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName,
			Labels: map[string]string{
				"fluent-pvc.pod-name": pod.Name,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(
						podPatched.Annotations["fluent-pvc.storage-size"],
					),
				},
			},
			StorageClassName: &storageClassName,
		},
	}

	namespace := pod.Namespace
	if pod.Namespace == "" {
		namespace = corev1.NamespaceDefault
	}

	podwebhooklog.Info("Creating PVC...")
	pvcResult, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	podwebhooklog.Info("Created PVC %q.\n", pvcResult.GetObjectMeta().GetName())

	pvcVolume := corev1.Volume{
		Name: "fluent-pvc",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
			},
		},
	}
	podPatched.Spec.Volumes = append(podPatched.Spec.Volumes, pvcVolume)

	podPatched.Annotations["fluent-pvc.on-create.processed"] = "true"

	for i := range pod.Spec.Containers {
		podPatched.Spec.Containers[i].VolumeMounts = append(podPatched.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
			Name:      "fluent-pvc",
			MountPath: "/tmp/fluent-pvc",
		})
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

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	if pod.Annotations["fluent-pvc.enabled"] != "true" {
		return admission.Allowed(fmt.Sprintf("Pod: %s is not a target for fluent-pvc.", pod.Name))
	}
	if pod.Annotations["fluent-pvc.on-create.processed"] != "true" {
		return admission.Allowed(fmt.Sprintf("fluent-pvc is not exist for Pod: %s", pod.Name))
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	namespace := pod.Namespace
	if pod.Namespace == "" {
		namespace = corev1.NamespaceDefault
	}

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"fluent-pvc.pod-name": pod.Name,
		},
	}
	pvcList, err := clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(labelSelector),
	})
	if err != nil {
		return admission.Allowed(fmt.Sprintf("PVC for %s does not exist.", pod.Name))
	}
	pvcName := pvcList.Items[0].ObjectMeta.Name

	payload := []jsonpatch.Operation{{
		Operation: "add",
		Path:      "/metadata/labels/fluent-pvc.deleted",
		Value:     "true",
	}}
	payloadBytes, _ := json.Marshal(payload)

	_, err = clientset.CoreV1().PersistentVolumeClaims(namespace).Patch(ctx, pvcName, types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
	if err != nil {
		return admission.Allowed(fmt.Sprintf("Label addition to %s failed.", pvcName))
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
