package controllers

import (
	"context"
	"fmt"

	"golang.org/x/xerrors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
)

//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs,verbs=get;list;watch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs/status,verbs=get
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete

type podReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func NewPodReconciler(mgr ctrl.Manager) *podReconciler {
	return &podReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
}

func (r *podReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("podReconciler").WithName("Reconcile")

	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}

	var fluentPVCName string
	if v, ok := pod.Annotations[constants.PodAnnotationFluentPVCName]; !ok {
		// not target
		return ctrl.Result{}, nil
	} else {
		fluentPVCName = v
	}

	if !isPodRunningPhase(pod) {
		logger.Info(fmt.Sprintf("Skip processing because pod='%s' is '%s' phase.", pod.Name, pod.Status.Phase))
		return ctrl.Result{}, nil
	}

	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	if err := r.Get(ctx, client.ObjectKey{Name: fluentPVCName}, fpvc); err != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}

	if !fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected {
		logger.Info(fmt.Sprintf(
			"Skip processing because deletePodIfSidecarContainerTerminationDetected=false in fluentpvc='%s' for pod='%s'",
			fpvc.Name, pod.Name,
		))
		return ctrl.Result{}, nil
	}

	containerName := fpvc.Spec.SidecarContainerTemplate.Name
	status := findContainerStatusByName(&pod.Status, containerName)
	if status == nil {
		return ctrl.Result{}, xerrors.New(fmt.Sprintf("Container='%s' does not have any status.", containerName))
	}
	if status.RestartCount == 0 && status.State.Terminated == nil {
		logger.Info(fmt.Sprintf(
			"Container='%s' in the pod='%s' has never been terminated.",
			containerName, pod.Name,
		))
		return ctrl.Result{}, nil
	} else if status.RestartCount == 0 && status.State.Terminated.ExitCode == 0 {
		logger.Info(fmt.Sprintf(
			"Container='%s' in the pod='%s' exited and the exitcode is 0.",
			containerName, pod.Name,
		))
		return ctrl.Result{}, nil
	} else if status.RestartCount != 0 && status.LastTerminationState.Terminated.ExitCode == 0 {
		logger.Info(fmt.Sprintf(
			"Container='%s' in the pod='%s' exited and the exitcode is 0.",
			containerName, pod.Name,
		))
		return ctrl.Result{}, nil
	}

	if status.RestartCount == 0 {
		logger.Info(fmt.Sprintf(
			"Delete the pod='%s' in the background because the container='%s' exited and the exitcode is %d.",
			pod.Name, containerName, status.State.Terminated.ExitCode,
		))
	} else if status.RestartCount > 0 {
		logger.Info(fmt.Sprintf(
			"Delete the pod='%s' in the background because the container='%s' restarted and the exitcode is %d.",
			pod.Name, containerName, status.LastTerminationState.Terminated.ExitCode,
		))
	}
	// TODO: Respects PodDisruptionBudget.
	deleteOptions := deleteOptionsBackground(&pod.UID, &pod.ResourceVersion)
	if err := r.Delete(ctx, pod, deleteOptions); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *podReconciler) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
	// TODO: Monitor at shorter intervals.

	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(pred).
		For(&corev1.Pod{}).
		Complete(r)
}
