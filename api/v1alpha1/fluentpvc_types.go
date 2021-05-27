package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FluentPVCSpec defines the desired state of FluentPVC
type FluentPVCSpec struct {
	// PVC spec template to inject into pod manifests.
	//+kubebuiler:validation:Required
	PVCSpecTemplate corev1.PersistentVolumeClaimSpec `json:"pvcSpecTemplate"`
	// Name of the Volume to mount the PVC.
	//+kubebuiler:validation:Required
	PVCVolumeName string `json:"pvcVolumeName"`
	// Path to mount the PVC.
	//+kubebuiler:validation:Required
	PVCMountPath string `json:"pvcMountPath"`
	// Common environment variables to inject into all containers.
	//+optional
	CommonEnv []corev1.EnvVar `json:"commonEnv,omitempty"`
	// Sidecare containers templates that must include a fluentd definition.
	//+kubebuiler:validation:Required
	//+kubebuiler:validation:MinItems=1
	SidecarContainersTemplate []corev1.Container `json:"sidecarContainersTemplate"`
	// Name of the fluentd container in sidecar containers.
	//+kubebuiler:validation:Required
	SidecarFluentdContainerName string `json:"sidecarFluentdContainerName"`
	// Port for the sidecar fluentd RPC.
	//+kubebuiler:validation:Required
	SidecarFluentdRpcPort uint32 `json:"sidecarFluentdRpcPort"`
	// Job template to finalize PVCs.
	//+kubebuiler:validation:Required
	PVCFinalizerJobSpecTemplate batchv1.JobSpec `json:"pvcFinalizerJobSpecTemplate"`
	// Name of the fluentd container in finalizer pod containers.
	//+kubebuiler:validation:Required
	PVCFinalizerFluentdContainerName string `json:"pvcFinalizerFluentdContainerName"`
	// Port for the sidecar fluentd RPC.
	//+kubebuiler:validation:Required
	PVCFinalizerFluentdRpcPort uint32 `json:"pvcFinalizerFluentdRpcPort"`
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

const (
	ConditionReady string = "Ready"
)

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
	//+kubebuiler:validation:Required
	FluentPVCName string `json:"fluentPVCName"`
	// PVC Name to bind.
	//+kubebuiler:validation:Required
	PVCName string `json:"pvcName"`
	// Pod Name to bind.
	//+kubebuiler:validation:Required
	PodName string `json:"podName"`
}

type FluentPVCBindingConditionType string

const (
	FluentPVCBindingConditionReady                 FluentPVCBindingConditionType = "Ready"
	FluentPVCBindingConditionOutOfUse              FluentPVCBindingConditionType = "OutOfUse"
	FluentPVCBindingConditionFinalizerPodApplied   FluentPVCBindingConditionType = "FinalizerPodApplied"
	FluentPVCBindingConditionFinalizerPodSucceeded FluentPVCBindingConditionType = "FinalizerPodSucceeded"
	FluentPVCBindingConditionFinalizerPodFailed    FluentPVCBindingConditionType = "FinalizerPodFailed"
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
