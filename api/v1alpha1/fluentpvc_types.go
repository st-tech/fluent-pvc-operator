package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FluentPVCSpec defines the desired state of FluentPVC
type FluentPVCSpec struct {
	// PVC spec template to inject into pod manifests.
	//+kubebuiler:validation:Required
	PVCSpecTemplate corev1.PersistentVolumeClaimSpec `json:"pvcSpecTemplate"`
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
	SidecarFluentdRpcPort int64 `json:"sidecarFluentdRpcPort"`
	// Pod spec template to finalize PVCs.
	//+kubebuiler:validation:Required
	PVCFinalizerPodSpecTemplate corev1.PodSpec `json:"pvcFinalizerPodSpecTemplate"`
	// Name of the fluentd container in finalizer pod containers.
	//+kubebuiler:validation:Required
	PVCFinalizerFluentdContainerName string `json:"pvcFinalizerFluentdContainerName"`
	// Port for the sidecar fluentd RPC.
	//+kubebuiler:validation:Required
	PVCFinalizerFluentdRpcPort int64 `json:"pvcFinalizerFluentdRpcPort"`
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
//+kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
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

func init() {
	SchemeBuilder.Register(&FluentPVC{}, &FluentPVCList{})
}
