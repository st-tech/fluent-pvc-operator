package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FluentPVCSpec defines the desired state of FluentPVC
type FluentPVCSpec struct {
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
