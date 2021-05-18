package v1

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:verbs=create;update,path=/mutate-core-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,versions=v1,name=mpod.kb.io,admissionReviewVersions={v1,v1beta1}

type podAnnotator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func NewPodAnnotator(c client.Client) admission.Handler {
	return &podAnnotator{Client: c}
}

func (a *podAnnotator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	err := a.decoder.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if pod.Annotations["fluent-pvc.enabled"] != "true" {
		return admission.Denied(fmt.Sprintf("Pod: %s is not a target for fluent-pvc.", pod.Name))
	}
	if pod.Annotations["fluent-pvc.on-create.processed"] == "true" {
		return admission.Denied(fmt.Sprintf("fluent-pvc is already exist for Pod: %s", pod.Name))
	}

	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	pvcClient := clientset.CoreV1().PersistentVolumeClaims(corev1.NamespaceDefault)

	storageClassName := "default-storage-class"
	pvcName := "fluent-pvc-" + pod.Name + "-" + RandStringRunes(8)

	// _, err = pvcClient.Get(ctx, pvcName, metav1.GetOptions{})
	// if err == nil {
	// 	return admission.Denied(fmt.Sprintf("fluent-pvc named %s is already exist", pvcName))
	// }

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("8Gi"),
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

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (a *podAnnotator) InjectDecoder(d *admission.Decoder) error {
	a.decoder = d
	return nil
}

func int32Ptr(i int32) *int32 { return &i }

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
