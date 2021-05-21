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
	// TODO: annotationsではなく、podのmanifestにpvcが既に存在しているかを見に行くようにする
	if pod.Annotations["fluent-pvc.on-create.processed"] == "true" {
		return admission.Allowed(fmt.Sprintf("fluent-pvc is already exist for Pod: %s", pod.Name))
	}
	// if pod.Annotations["fluent-pvc.storage-size"] == "" {
	// 	// TODO: Fetch default storage size from FluentPVCOperator
	// 	// Set default storage size if pod annotation is not exist
	// 	podPatched.Annotations["fluent-pvc.storage-size"] = "8Gi"
	// }

	// config, err := rest.InClusterConfig()
	// if err != nil {
	// 	return admission.Errored(http.StatusInternalServerError, err)
	// }
	// clientset, err := kubernetes.NewForConfig(config)
	// if err != nil {
	// 	return admission.Errored(http.StatusInternalServerError, err)
	// }

	fpvc := &FluentPVC{}
	m.Client.Get(ctx, client.ObjectKey{
		Namespace: "",
		Name:      "fluent-pvc-sample",
	}, fpvc)
	// TODO: fpvcが存在しなかった時の処理が必要

	// TODO: Fix storage class name to custom storage class
	// storageClassName := "standard"
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
		// Spec: corev1.PersistentVolumeClaimSpec{
		// 	AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
		// 	Resources: corev1.ResourceRequirements{
		// 		Requests: corev1.ResourceList{
		// 			corev1.ResourceStorage: resource.MustParse(
		// 				podPatched.Annotations["fluent-pvc.storage-size"],
		// 			),
		// 		},
		// 	},
		// 	StorageClassName: &storageClassName,
		// },
	}

	// m.Client.Patch()
	// m.Client.List()

	podwebhooklog.Info("Creating PVC...")
	err = m.Client.Create(ctx, pvc)
	// pvcResult, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	// podwebhooklog.Info("Created PVC %q.\n", pvcResult.GetObjectMeta().GetName())

	pvcVolume := corev1.Volume{
		Name: fpvc.Spec.PVCVolumeName,
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

	// config, err := rest.InClusterConfig()
	// if err != nil {
	// 	return admission.Errored(http.StatusInternalServerError, err)
	// }
	// clientset, err := kubernetes.NewForConfig(config)
	// if err != nil {
	// 	return admission.Errored(http.StatusInternalServerError, err)
	// }

	namespace := pod.Namespace
	if pod.Namespace == "" {
		namespace = corev1.NamespaceDefault
	}

	// labelSelector := &metav1.LabelSelector{
	// 	MatchLabels: map[string]string{
	// 		"fluent-pvc.pod-name": pod.Name,
	// 	},
	// }

	var pvcList corev1.PersistentVolumeClaimList
	// pvcList := &[]corev1.PersistentVolumeClaim{}

	err = v.Client.List(ctx, &pvcList, &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{"fluent-pvc.pod-name": pod.Name}),
	})

	// pvcList, err := clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
	// 	LabelSelector: metav1.FormatLabelSelector(labelSelector),
	// })

	if err != nil {
		podwebhooklog.Error(err, fmt.Sprintf("PersistentVolumeClaims.List() for %s failed.", pod.Name))
		return admission.Allowed("")
	}
	if len(pvcList.Items) == 0 {
		return admission.Allowed(fmt.Sprintf("PVC for %s does not exist.", pod.Name))
	}
	pvcName := pvcList.Items[0].ObjectMeta.Name

	// payload := []jsonpatch.Operation{{
	// 	Operation: "add",
	// 	Path:      "/metadata/labels/fluent-pvc.deleted",
	// 	Value:     "true",
	// }}
	// payloadBytes, _ := json.Marshal(payload)

	patchPvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"fluent-pvc.deleted": "true",
			},
		},
	}

	err = v.Client.Patch(ctx, patchPvc, client.Apply)

	// _, err = clientset.CoreV1().PersistentVolumeClaims(namespace).Patch(ctx, pvcName, types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
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
