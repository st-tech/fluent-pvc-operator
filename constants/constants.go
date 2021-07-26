package constants

const (
	OwnerControllerField          = ".metadata.ownerReference.controller"
	PodLabelFluentPVCName         = "fluent-pvc-operator.tech.zozo.com/fluent-pvc-name"
	PodLabelFluentPVCBindingName  = "fluent-pvc-operator.tech.zozo.com/fluent-pvc-binding-name"
	PVCFinalizerName              = "fluent-pvc-operator.tech.zozo.com/pvc-protection"
	FluentPVCBindingFinalizerName = "fluent-pvc-operator.tech.zozo.com/fluentpvcbinding-protection"
	FluentPVCFinalizerName        = "fluent-pvc-operator.tech.zozo.com/fluentpvc-protection"
)
