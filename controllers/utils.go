package controllers

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
)

func indexPVCByOwnerFluentPVCBinding(obj client.Object) []string {
	pvc := obj.(*corev1.PersistentVolumeClaim)
	owner := metav1.GetControllerOf(pvc)
	if !isOwnerFluentPVCBinding(owner) {
		return nil
	}
	return []string{owner.Name}
}

func indexJobByOwnerFluentPVCBinding(obj client.Object) []string {
	j := obj.(*batchv1.Job)
	owner := metav1.GetControllerOf(j)
	if !isOwnerFluentPVCBinding(owner) {
		return nil
	}
	return []string{owner.Name}
}

func isOwnerFluentPVCBinding(owner *metav1.OwnerReference) bool {
	return owner != nil &&
		owner.APIVersion == fluentpvcv1alpha1.GroupVersion.String() &&
		owner.Kind == "FluentPVCBinding"
}

func indexFluentPVCBindingByOwnerFluentPVC(obj client.Object) []string {
	b := obj.(*fluentpvcv1alpha1.FluentPVCBinding)
	owner := metav1.GetControllerOf(b)
	if !isOwnerFluentPVC(owner) {
		return nil
	}
	return []string{owner.Name}
}

func isOwnerFluentPVC(owner *metav1.OwnerReference) bool {
	return owner != nil &&
		owner.APIVersion == fluentpvcv1alpha1.GroupVersion.String() &&
		owner.Kind == "FluentPVC"
}

func isFluentPVCBindingReady(b *fluentpvcv1alpha1.FluentPVCBinding) bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionReady))
}

func isFluentPVCBindingOutOfUse(b *fluentpvcv1alpha1.FluentPVCBinding) bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionOutOfUse))
}

func isFluentPVCBindingFinalizerJobApplied(b *fluentpvcv1alpha1.FluentPVCBinding) bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobApplied))
}

func isFluentPVCBindingFinalizerJobSucceeded(b *fluentpvcv1alpha1.FluentPVCBinding) bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobSucceeded))
}

func isFluentPVCBindingFinalizerJobFailed(b *fluentpvcv1alpha1.FluentPVCBinding) bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobFailed))
}

func isFluentPVCBindingUnknown(b *fluentpvcv1alpha1.FluentPVCBinding) bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionUnknown))
}

func requeueResult(requeueAfter time.Duration) ctrl.Result {
	return ctrl.Result{
		Requeue:      true,
		RequeueAfter: requeueAfter,
	}
}

func getFinishedStatus(j *batchv1.Job) (bool, batchv1.JobConditionType) {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true, c.Type
		}
	}
	return false, ""
}

// isJobFinished returns whether or not a job has completed successfully or failed.
func isJobFinished(j *batchv1.Job) bool {
	isFinished, _ := getFinishedStatus(j)
	return isFinished
}

func isJobSucceeded(j *batchv1.Job) bool {
	isFinished, t := getFinishedStatus(j)
	return isFinished && t == batchv1.JobComplete
}

func isJobFailed(j *batchv1.Job) bool {
	isFinished, t := getFinishedStatus(j)
	return isFinished && t == batchv1.JobFailed
}
