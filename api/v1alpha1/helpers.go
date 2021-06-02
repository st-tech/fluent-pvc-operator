package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (b *FluentPVCBinding) IsConditionReady() bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(FluentPVCBindingConditionReady))
}

func (b *FluentPVCBinding) IsConditionOutOfUse() bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(FluentPVCBindingConditionOutOfUse))
}

func (b *FluentPVCBinding) IsConditionFinalizerJobApplied() bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(FluentPVCBindingConditionFinalizerJobApplied))
}

func (b *FluentPVCBinding) IsConditionFinalizerJobSucceeded() bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(FluentPVCBindingConditionFinalizerJobSucceeded))
}

func (b *FluentPVCBinding) IsConditionFinalizerJobFailed() bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(FluentPVCBindingConditionFinalizerJobFailed))
}

func (b *FluentPVCBinding) IsConditionUnknown() bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(FluentPVCBindingConditionUnknown))
}

func (b *FluentPVCBinding) SetConditionReady(reason, message string) {
	b.setConditionTrue(FluentPVCBindingConditionReady, reason, message)
}

func (b *FluentPVCBinding) SetConditionOutOfUse(reason, message string) {
	b.setConditionTrue(FluentPVCBindingConditionOutOfUse, reason, message)
}

func (b *FluentPVCBinding) SetConditionFinalizerJobApplied(reason, message string) {
	b.setConditionTrue(FluentPVCBindingConditionFinalizerJobApplied, reason, message)
}

func (b *FluentPVCBinding) SetConditionFinalizerJobSucceeded(reason, message string) {
	b.setConditionTrue(FluentPVCBindingConditionFinalizerJobSucceeded, reason, message)
}

func (b *FluentPVCBinding) SetConditionFinalizerJobFailed(reason, message string) {
	b.setConditionTrue(FluentPVCBindingConditionFinalizerJobFailed, reason, message)
}

func (b *FluentPVCBinding) SetConditionUnknown(reason, message string) {
	b.setConditionTrue(FluentPVCBindingConditionUnknown, reason, message)
}

func (b *FluentPVCBinding) UnsetConditionReady(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionReady, reason, message)
}

func (b *FluentPVCBinding) UnsetConditionOutOfUse(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionOutOfUse, reason, message)
}

func (b *FluentPVCBinding) UnsetConditionFinalizerJobApplied(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionFinalizerJobApplied, reason, message)
}

func (b *FluentPVCBinding) UnsetConditionFinalizerJobSucceeded(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionFinalizerJobSucceeded, reason, message)
}

func (b *FluentPVCBinding) UnsetConditionFinalizerJobFailed(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionFinalizerJobFailed, reason, message)
}

func (b *FluentPVCBinding) UnsetConditionUnknown(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionUnknown, reason, message)
}

func (b *FluentPVCBinding) setConditionTrue(t FluentPVCBindingConditionType, reason, message string) {
	b.setCondition(t, metav1.ConditionTrue, reason, message)
}

func (b *FluentPVCBinding) setConditionFalse(t FluentPVCBindingConditionType, reason, message string) {
	b.setCondition(t, metav1.ConditionFalse, reason, message)
}

func (b *FluentPVCBinding) setCondition(t FluentPVCBindingConditionType, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
		Type:    string(t),
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}
