package controllers

import (
	"context"
	"time"

	"golang.org/x/xerrors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
)

func matchingOwnerControllerField(ownerName string) client.MatchingFields {
	return client.MatchingFields(map[string]string{constants.OwnerControllerField: ownerName})
}

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

// https://github.com/kubernetes/kubernetes/blob/c495744436fc94ebbef2fcbeb97699ca96fe02dd/pkg/api/pod/util.go#L242-L272
// isPodReadyCondition returns true if a pod is ready; false otherwise.
func isPodReadyCondition(pod *corev1.Pod) bool {
	return isPodReadyConditionTrue(pod.Status)
}

// isPodReadyConditionTrue returns true if a pod is ready; false otherwise.
func isPodReadyConditionTrue(status corev1.PodStatus) bool {
	condition := getPodReadyCondition(status)
	return condition != nil && condition.Status == corev1.ConditionTrue
}

// getPodReadyCondition extracts the pod ready condition from the given status and returns that.
// Returns nil if the condition is not present.
func getPodReadyCondition(status corev1.PodStatus) *corev1.PodCondition {
	_, condition := getPodCondition(&status, corev1.PodReady)
	return condition
}

func isPodReadyPhase(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning
}

// getPodCondition extracts the provided condition from the given status and returns that.
// Returns nil and -1 if the condition is not present, and the index of the located condition.
func getPodCondition(status *corev1.PodStatus, conditionType corev1.PodConditionType) (int, *corev1.PodCondition) {
	if status == nil {
		return -1, nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionType {
			return i, &status.Conditions[i]
		}
	}
	return -1, nil
}

func findContainerStatusByName(status *corev1.PodStatus, name string) *corev1.ContainerStatus {
	for _, c := range status.ContainerStatuses {
		if c.Name == name {
			return c.DeepCopy()
		}
	}
	return nil
}

func isCreatedBefore(obj client.Object, duration time.Duration) bool {
	threshold := metav1.NewTime(time.Now().Add(-duration))
	creationTimestamp := obj.GetCreationTimestamp()
	return creationTimestamp.Before(&threshold)
}

func updateOrNothingControllerReference(ctx context.Context, c client.Client, owner, controllee client.Object) error {
	if metav1.IsControlledBy(controllee, owner) {
		return nil
	}
	if err := ctrl.SetControllerReference(owner, controllee, c.Scheme()); err != nil {
		return xerrors.Errorf("Unexpected error occurred: %w", err)
	}
	if err := c.Update(ctx, controllee); err != nil {
		return xerrors.Errorf("Unexpected error occurred: %w", err)
	}
	return nil
}
