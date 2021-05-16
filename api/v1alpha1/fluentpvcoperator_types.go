package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FluentPVCOperatorSpec defines the desired state of FluentPVCOperator
type FluentPVCOperatorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of FluentPVCOperator. Edit fluentpvcoperator_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// FluentPVCOperatorStatus defines the observed state of FluentPVCOperator
type FluentPVCOperatorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// FluentPVCOperator is the Schema for the fluentpvcoperators API
type FluentPVCOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FluentPVCOperatorSpec   `json:"spec,omitempty"`
	Status FluentPVCOperatorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FluentPVCOperatorList contains a list of FluentPVCOperator
type FluentPVCOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FluentPVCOperator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FluentPVCOperator{}, &FluentPVCOperatorList{})
}
