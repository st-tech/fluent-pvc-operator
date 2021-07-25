package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// FluentPVCSpec defines the desired state of FluentPVC
type FluentPVCSpec struct {
	// PVC spec template to inject into pod manifests.
	//+kubebuilder:validation:Required
	PVCSpecTemplate corev1.PersistentVolumeClaimSpec `json:"pvcSpecTemplate"`
	// Job template to finalize PVCs.
	//+kubebuilder:validation:Required
	PVCFinalizerJobSpecTemplate batchv1.JobSpec `json:"pvcFinalizerJobSpecTemplate"`
	// Name of the Volume to mount the PVC.
	// Must be a DNS_LABEL and unique within the pod
	// ref. https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	//+kubebuilder:validation:Required
	PVCVolumeName string `json:"pvcVolumeName"`
	// Path to mount the PVC.
	// Must not contain ':'
	//+kubebuilder:validation:Required
	PVCVolumeMountPath string `json:"pvcVolumeMountPath"`
	// Sidecare containers template.
	//+kubebuilder:validation:Required
	SidecarContainerTemplate corev1.Container `json:"sidecarContainerTemplate"`
	// Common environment variables to inject into all containers.
	//+optional
	CommonEnvs []corev1.EnvVar `json:"commonEnvs,omitempty"`
	// Common volumes to inject into all pods.
	//+optional
	CommonVolumes []corev1.Volume `json:"commonVolumes,omitempty"`
	// Common volumeMounts to inject into all containers.
	//+optional
	CommonVolumeMounts []corev1.VolumeMount `json:"commonVolumeMounts,omitempty"`
	// Delete the pod if the sidecar container termination is detected.
	//+kubebuilder:validation:Required
	DeletePodIfSidecarContainerTerminationDetected bool `json:"deletePodIfSidecarContainerTerminationDetected,omitempty"`
}

// FluentPVCStatus defines the observed state of FluentPVC
type FluentPVCStatus struct {
	// Conditions is an array of conditions.
	// Known .status.conditions.type are: "Ready"
	//+patchMergeKey=type
	//+patchStrategy=merge
	//+listType=map
	//+listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// FluentPVC is the Schema for the fluentpvcs API
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
type FluentPVC struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FluentPVCSpec   `json:"spec,omitempty"`
	Status FluentPVCStatus `json:"status,omitempty"`
}

// FluentPVCList contains a list of FluentPVC
//+kubebuilder:object:root=true
type FluentPVCList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FluentPVC `json:"items"`
}

type FluentPVCBindingSpec struct {
	// FluentPVC Name to bind.
	//+kubebuilder:validation:Required
	FluentPVC ObjectIdentity `json:"fluentPVC"`
	// PVC Name to bind.
	//+kubebuilder:validation:Required
	PVC ObjectIdentity `json:"pvc"`
	// Pod Name to bind.
	//+kubebuilder:validation:Required
	Pod ObjectIdentity `json:"pod"`
}

type ObjectIdentity struct {
	// Object Name
	//+kubebuilder:validation:Required
	Name string `json:"name"`
	// Object UID
	//+kubebuilder:validation:Required
	UID types.UID `json:"uid"`
}

type FluentPVCBindingConditionType string

const (
	FluentPVCBindingConditionReady                 FluentPVCBindingConditionType = "Ready"
	FluentPVCBindingConditionOutOfUse              FluentPVCBindingConditionType = "OutOfUse"
	FluentPVCBindingConditionFinalizerJobApplied   FluentPVCBindingConditionType = "FinalizerJobApplied"
	FluentPVCBindingConditionFinalizerJobSucceeded FluentPVCBindingConditionType = "FinalizerJobSucceeded"
	FluentPVCBindingConditionFinalizerJobFailed    FluentPVCBindingConditionType = "FinalizerJobFailed"
	FluentPVCBindingConditionUnknown               FluentPVCBindingConditionType = "Unknown"
)

// FluentPVCStatus defines the observed state of FluentPVC
type FluentPVCBindingStatus struct {
	// Conditions is an array of conditions.
	// Known .status.conditions.type are: "Ready"
	//+patchMergeKey=type
	//+patchStrategy=merge
	//+listType=map
	//+listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced
type FluentPVCBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FluentPVCBindingSpec   `json:"spec,omitempty"`
	Status FluentPVCBindingStatus `json:"status,omitempty"`
}

// FluentPVCList contains a list of FluentPVC
//+kubebuilder:object:root=true
type FluentPVCBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FluentPVCBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&FluentPVC{}, &FluentPVCList{},
		&FluentPVCBinding{}, &FluentPVCBindingList{},
	)
}
