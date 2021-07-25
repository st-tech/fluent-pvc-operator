package v1alpha1

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
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

func (b *FluentPVCBinding) SetConditionNotReady(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionReady, reason, message)
}

func (b *FluentPVCBinding) SetConditionNotOutOfUse(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionOutOfUse, reason, message)
}

func (b *FluentPVCBinding) SetConditionNotFinalizerJobApplied(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionFinalizerJobApplied, reason, message)
}

func (b *FluentPVCBinding) SetConditionNotFinalizerJobSucceeded(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionFinalizerJobSucceeded, reason, message)
}

func (b *FluentPVCBinding) SetConditionNotFinalizerJobFailed(reason, message string) {
	b.setConditionFalse(FluentPVCBindingConditionFinalizerJobFailed, reason, message)
}

func (b *FluentPVCBinding) SetConditionNotUnknown(reason, message string) {
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
	b.resetPhase()
}

func (b *FluentPVCBinding) resetPhase() {
	conditions := []metav1.Condition{}
	for _, c := range b.Status.Conditions {
		if c.Status == metav1.ConditionTrue {
			conditions = append(conditions, c)
		}
	}
	if len(conditions) == 0 {
		b.SetPhasePending()
		return
	}
	sort.Slice(conditions, func(i, j int) bool {
		// NOTE: Sort in order of LastTransisionTime from newest to oldest.
		return conditions[j].LastTransitionTime.Before(&conditions[i].LastTransitionTime)
	})
	// NOTE: Use the latest condition
	b.Status.Phase = FluentPVCBindingPhase(conditions[0].Type)
}

func (b *FluentPVCBinding) SetFluentPVC(fpvc *FluentPVC) {
	b.Spec.FluentPVC = b.toObjectIdentity(&fpvc.ObjectMeta)
}
func (b *FluentPVCBinding) SetPVC(pvc *corev1.PersistentVolumeClaim) {
	b.Spec.PVC = b.toObjectIdentity(&pvc.ObjectMeta)
}

func (b *FluentPVCBinding) SetPod(pod *corev1.Pod) {
	b.Spec.Pod = b.toObjectIdentity(&pod.ObjectMeta)
}

func (b *FluentPVCBinding) toObjectIdentity(o *metav1.ObjectMeta) ObjectIdentity {
	return ObjectIdentity{
		Name: o.Name,
		UID:  o.UID,
	}
}

func (b *FluentPVCBinding) SetPhasePending() {
	b.Status.Phase = FluentPVCBindingPhasePending
}

func (b *FluentPVCBinding) IsControlledBy(fpvc *FluentPVC) bool {
	return metav1.IsControlledBy(b, fpvc)
}

func (b *FluentPVCBinding) IsBindingPod(pod *corev1.Pod) bool {
	return pod.UID == b.Spec.Pod.UID
}

func (b *FluentPVCBinding) IsBindingPVC(pvc *corev1.PersistentVolumeClaim) bool {
	return pvc.UID == b.Spec.PVC.UID
}
