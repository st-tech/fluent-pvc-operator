package controllers

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
)

func matchingOwnerControllerField(ownerName string) client.MatchingFields {
	return client.MatchingFields(map[string]string{constants.OwnerControllerField: ownerName})
}

func deleteOptionsBackground(uid *types.UID, resourceVersion *string) *client.DeleteOptions {
	return &client.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID:             uid,
			ResourceVersion: resourceVersion,
		},
		PropagationPolicy: (*metav1.DeletionPropagation)(pointer.StringPtr(string(metav1.DeletePropagationBackground))),
	}
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

func isJobSucceeded(j *batchv1.Job) bool {
	isFinished, t := getFinishedStatus(j)
	return isFinished && t == batchv1.JobComplete
}

func isJobFailed(j *batchv1.Job) bool {
	isFinished, t := getFinishedStatus(j)
	return isFinished && t == batchv1.JobFailed
}

func isPodRunningPhase(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning
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
