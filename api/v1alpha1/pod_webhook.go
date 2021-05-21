package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
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

//+kubebuilder:webhook:path=/mutate-core-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,verbs=create,versions=v1,name=mpod.kb.io,admissionReviewVersions={v1,v1beta1}
//+kubebuilder:webhook:path=/validate-core-v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,verbs=create,versions=v1,name=vpod.kb.io,admissionReviewVersions={v1,v1beta1}
//+kubebuilder:webhook:path=/mutate-ondelete-core-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,verbs=delete,versions=v1,name=mdpod.kb.io,admissionReviewVersions={v1,v1beta1}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

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
	pmd := NewpodOnDeletionMutator(mgr.GetClient())
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
		podwebhooklog.Info(fmt.Sprintf("Pod: %s is not a target for fluent-pvc.", pod.Name))
		return PodAdmissionResponse(pod, req)
	}
	if pod.Annotations["fluent-pvc.on-create.processed"] == "true" {
		podwebhooklog.Info(fmt.Sprintf("fluent-pvc is already exist for Pod: %s", pod.Name))
		return PodAdmissionResponse(pod, req)
	}
	if pod.Annotations["fluent-pvc.storage-size"] == "" {
		// TODO: Fetch default storage size from FluentPVCOperator
		// Set default storage size if pod annotation is not exist
		podPatched.Annotations["fluent-pvc.storage-size"] = "8Gi"
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	pvcClient := clientset.CoreV1().PersistentVolumeClaims(corev1.NamespaceDefault)

	// TODO: Fix storage class name to custom storage class
	storageClassName := "standard"
	pvcName := "fluent-pvc-" + pod.Name + "-" + RandStringRunes(8)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName,
			Annotations: map[string]string{
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

	fmt.Println("Creating PVC...")
	pvcResult, err := pvcClient.Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	fmt.Printf("Created PVC %q.\n", pvcResult.GetObjectMeta().GetName())

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
	podPatched.Annotations["fluent-pvc.pod-name"] = pod.Name
	podPatched.Annotations["fluent-pvc.pvc-name"] = pvcName

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

func NewpodOnDeletionMutator(c client.Client) admission.Handler {
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
	if pod.Annotations["fluent-pvc.pvc-name"] == "" {
		return admission.Allowed(fmt.Sprintf("fluent-pvc is not specified for Pod: %s", pod.Name))
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	pvcClient := clientset.CoreV1().PersistentVolumeClaims(corev1.NamespaceDefault)

	// pvc, err := pvcClient.Get(ctx, pod.Annotations["fluent-pvc.pvc-name"], metav1.GetOptions{})
	// if err != nil {
	// 	podwebhooklog.Info(fmt.Sprintf("pvcClient.Get Failed. %s", pod.Annotations["fluent-pvc.pvc-name"]))
	// 	return admission.Errored(http.StatusInternalServerError, err)
	// }

	// marshaledOriginalPvc, err := json.Marshal(pvc)

	// if pvc.Labels == nil {
	// 	pvc.Labels = map[string]string{}
	// }
	// pvc.Labels["fluent-pvc.deleted"] = "true"

	// marshaledPvc, err := json.Marshal(pvc)
	// if err != nil {
	// 	podwebhooklog.Info("json.Marshal Failed.")
	// 	return admission.Errored(http.StatusInternalServerError, err)
	// }

	payload := []jsonpatch.Operation{{
		Operation: "add",
		Path:      "/metadata/annotations/fluent-pvc.deleted",
		Value:     "true",
	}}
	payloadBytes, _ := json.Marshal(payload)

	// patches, err := jsonpatch.CreatePatch(marshaledOriginalPvc, marshaledPvc)
	// marshaledPatches, err := json.Marshal(patches)

	_, err = pvcClient.Patch(ctx, pod.Annotations["fluent-pvc.pvc-name"], types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.Allowed(fmt.Sprintf("Label addition to %s is succeeded.", pod.Annotations["fluent-pvc.pvc-name"]))
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
