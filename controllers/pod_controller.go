package controllers

import (
	"context"
	"fmt"

	"golang.org/x/xerrors"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
)

//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs,verbs=get;list;watch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs/status,verbs=get
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete

type PodReconciler struct {
	client.Client
	APIReader client.Reader
	Log       logr.Logger
	Scheme    *runtime.Scheme
}

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("controllers").WithName("pod_controller")

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

	if !isPodReady(pod) {
		logger.Info(fmt.Sprintf(
			"Skip processing because pod='%s' is '%s' status.",
			pod.Name, pod.Status.Phase,
		))
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
	if status.RestartCount == 0 {
		logger.Info(fmt.Sprintf(
			"Container='%s' in the pod='%s' has never been restarted.",
			containerName, pod.Name,
		))
		return ctrl.Result{}, nil
	}
	if status.LastTerminationState.Terminated.ExitCode == 0 {
		logger.Info(fmt.Sprintf(
			"Container='%s' in the pod='%s' exited and the exitcode is 0.",
			containerName, pod.Name,
		))
		return ctrl.Result{}, nil
	}

	logger.Info(fmt.Sprintf(
		"Delete the pod='%s' because the container='%s' exited and the exitcode is %d.",
		pod.Name, containerName, status.LastTerminationState.Terminated.ExitCode,
	))
	// TODO: Respects PodDisruptionBudget.
	gracePeriodSeconds := int64(60 * 5) // 5 minutes
	if *pod.DeletionGracePeriodSeconds > gracePeriodSeconds {
		gracePeriodSeconds = *pod.DeletionGracePeriodSeconds
	}
	deleteOptions := &client.DeleteOptions{
		GracePeriodSeconds: &gracePeriodSeconds,
		Preconditions: &metav1.Preconditions{
			UID:             &pod.UID,
			ResourceVersion: &pod.ResourceVersion,
		},
		PropagationPolicy: func(p metav1.DeletionPropagation) *metav1.DeletionPropagation { return &p }(metav1.DeletePropagationBackground),
	}
	if err := r.Delete(ctx, pod, deleteOptions); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	logger.Info(fmt.Sprintf("Deleted the pod='%s' in the background.", pod.Name))

	return ctrl.Result{}, nil
}

func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(pred).
		For(&corev1.Pod{}).
		Complete(r)
}
