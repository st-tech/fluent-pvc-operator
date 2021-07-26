package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/xerrors"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
)

//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs,verbs=get;list;watch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs/status,verbs=get
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch

type fluentPVCBindingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func NewFluentPVCBindingReconciler(mgr ctrl.Manager) *fluentPVCBindingReconciler {
	return &fluentPVCBindingReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
}

func (r *fluentPVCBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("fluentPVCBindingReconciler").WithName("Reconcile")
	b := &fluentpvcv1alpha1.FluentPVCBinding{}
	if err := r.Get(ctx, req.NamespacedName, b); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}

	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	if err := r.Get(ctx, client.ObjectKey{Name: b.Spec.FluentPVC.Name}, fpvc); err != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	if !b.IsControlledBy(fpvc) {
		if err := r.updateControllerFluentPVC(ctx, b, fpvc); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred: %w", err)
		}
		// NOTE: Wait until next #Reconcile to avoid update confliction.
		return ctrl.Result{}, nil
	}

	if b.IsConditionUnknown() {
		logger.Info(fmt.Sprintf("Skip processing because fluentpvcbinding='%s' is unknown status.", b.Name))
		return ctrl.Result{}, nil
	}

	pod := &corev1.Pod{}
	podFound := true
	if filled, err := r.fulfillFluentPVCBindingPod(ctx, b, pod); err != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	} else {
		podFound = filled
	}

	pvc := &corev1.PersistentVolumeClaim{}
	pvcFound := true
	if filled, err := r.fulfillFluentPVCBindingPVC(ctx, b, pvc); err != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	} else {
		pvcFound = filled
	}

	if pvcFound && pvc.Status.Phase == corev1.ClaimLost {
		if err := r.updateConditionUnknownPVCLost(ctx, b); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
		return ctrl.Result{}, nil
	}

	if b.IsConditionFinalizerJobSucceeded() {
		if pvcFound && controllerutil.ContainsFinalizer(pvc, constants.PVCFinalizerName) {
			logger.Info(fmt.Sprintf("Skip processing because the finalizer of fluentpvcbinding='%s' is not removed.", b.Name))
			return ctrl.Result{}, nil
		}
		if err := r.deleteFluentPVCBinding(ctx, b); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
		return ctrl.Result{}, nil
	}

	if !podFound && !b.IsConditionReady() {
		if isCreatedBefore(b, 1*time.Hour) { // TODO: make it configurable?
			if err := r.updateConditionUnknownPodNotFoundLongTime(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		} else {
			logger.Info(fmt.Sprintf("Skip processing until the next reconciliation because pod='%s' is not found.", b.Spec.Pod.Name))
		}
		return ctrl.Result{}, nil
	}

	switch {
	case !podFound && !pvcFound:
		if err := r.updateConditionUnknownPodAndPVCNotFound(ctx, b); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	case !podFound && pvcFound:
		if !b.IsConditionOutOfUse() {
			if err := r.updateConditionOutOfUsePodDeletedPVCFound(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
			return ctrl.Result{}, nil
		}
		if err := r.updateConditionByFinalizerJobStatus(ctx, b); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	case podFound && !pvcFound:
		switch pod.Status.Phase {
		case corev1.PodPending, corev1.PodUnknown:
			logger.Info(fmt.Sprintf(
				"Skip processing because pod='%s'(UID='%s') is '%s' phase and pvc='%s'(UID='%s') is not found",
				b.Spec.Pod.Name, b.Spec.Pod.UID, pod.Status.Phase, b.Spec.PVC.Name, b.Spec.PVC.UID,
			))
		case corev1.PodRunning:
			if err := r.updateConditionUnknownPodRunningPVCNotFound(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		case corev1.PodSucceeded, corev1.PodFailed:
			if err := r.updateConditionUnknownPodCompletedPVCNotFound(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		}
	case podFound && pvcFound:
		if !b.IsConditionReady() {
			if err := r.updateConditionReadyPodFoundPVCFound(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
			// NOTE: Avoid update conflictions.
			return ctrl.Result{}, nil
		}
		switch pod.Status.Phase {
		case corev1.PodPending, corev1.PodRunning, corev1.PodUnknown:
			logger.Info(fmt.Sprintf(
				"Skip processing because pod='%s'(UID='%s') is '%s' phase and pvc='%s'(UID='%s') is found",
				b.Spec.Pod.Name, b.Spec.Pod.UID, pod.Status.Phase, b.Spec.PVC.Name, b.Spec.PVC.UID,
			))
		case corev1.PodSucceeded, corev1.PodFailed:
			if !b.IsConditionOutOfUse() {
				if err := r.updateConditionOutOfUsePodCompletedPVCFound(ctx, b); err != nil {
					return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
				}
				// NOTE: Wait until a finalizer job is applied.
				return ctrl.Result{}, nil
			}
			if err := r.updateConditionByFinalizerJobStatus(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *fluentPVCBindingReconciler) fulfillFluentPVCBindingPod(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding, pod *corev1.Pod) (bool, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("fluentPVCBindingReconciler").WithName("fulfillFluentPVCBindingPod")
	podFound := true
	if b.Spec.Pod.Name == "" {
		// NOTE: Pods created by Deployments don't have the Name when the webhook is invoked,
		//       so use a label for FluentPVCBinding to find it.
		pods := &corev1.PodList{}
		if err := r.List(ctx, pods, &client.ListOptions{
			Namespace: b.Namespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{
				constants.PodLabelFluentPVCBindingName: b.Name,
			}),
		}); err != nil {
			return false, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
		if len(pods.Items) == 0 {
			podFound = false
		} else if len(pods.Items) != 1 {
			return false, xerrors.New(fmt.Sprintf("Illegal number of pods(n=%d) is found.", len(pods.Items)))
		} else {
			pods.Items[0].DeepCopyInto(pod)
		}
	} else {
		if err := r.Get(ctx, client.ObjectKey{Namespace: b.Namespace, Name: b.Spec.Pod.Name}, pod); err != nil {
			if apierrors.IsNotFound(err) {
				podFound = false
			} else {
				return false, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		}
	}
	if podFound && (b.Spec.Pod.UID == "" || b.Spec.Pod.Name == "") {
		// NOTE: When the pod_webhook is invoked, the pod does not have a UID, so it is an empty string.
		//       Therefore, the first pod that is found with an empty UID is considered to be the target.
		if err := r.fillPodUID(ctx, b, pod); err != nil {
			return false, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
		// NOTE: Wait until next #Reconcile to avoid update confliction.
		return true, nil
	}
	if !b.IsBindingPod(pod) {
		logger.Info(fmt.Sprintf("pod.UID='%s' is different from the binding pod.UID='%s' for name='%s'.", pod.UID, b.Spec.Pod.UID, b.Name))
		podFound = false
	}
	return podFound, nil
}

func (r *fluentPVCBindingReconciler) fulfillFluentPVCBindingPVC(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("fluentPVCBindingReconciler").WithName("fulfillFluentPVCBindingPVC")
	pvcFound := true
	if err := r.Get(ctx, client.ObjectKey{Namespace: b.Namespace, Name: b.Spec.PVC.Name}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			pvcFound = false
		} else {
			return false, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	}
	if !b.IsBindingPVC(pvc) {
		logger.Info(fmt.Sprintf("pvc.UID='%s' is different from the binding pvc.UID='%s' for name='%s'.", pvc.UID, b.Spec.PVC.UID, b.Name))
		pvcFound = false
	}
	return pvcFound, nil
}

func (r *fluentPVCBindingReconciler) updateControllerFluentPVC(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding, fpvc *fluentpvcv1alpha1.FluentPVC) error {
	if err := ctrl.SetControllerReference(fpvc, b, r.Scheme); err != nil {
		return xerrors.Errorf("Unexpected error occurred: %w", err)
	}
	b.SetFluentPVC(fpvc)
	if err := r.Update(ctx, b); err != nil {
		return xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	return nil
}

func (r *fluentPVCBindingReconciler) fillPodUID(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding, pod *corev1.Pod) error {
	podHasPVC := false
	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == b.Spec.PVC.Name {
			podHasPVC = true
		}
	}
	if !podHasPVC {
		return xerrors.New(fmt.Sprintf(
			"There is an inconsistency in the definition of fluentpvcbinding='%s' because pod='%s' does not have pvc='%s'.",
			b.Name, pod.Name, b.Spec.PVC.Name,
		))
	}
	podHasFluentPVCLabel := false
	if v, ok := pod.Labels[constants.PodLabelFluentPVCName]; ok {
		podHasFluentPVCLabel = v == b.Spec.FluentPVC.Name
	}
	if !podHasFluentPVCLabel {
		return xerrors.New(fmt.Sprintf(
			"There is an inconsistency in the definition of fluentpvcbinding='%s' because pod='%s' does not have fluentpvc='%s'",
			b.Name, pod.Name, b.Spec.FluentPVC.Name,
		))
	}
	b.SetPod(pod)
	if err := r.Update(ctx, b); err != nil {
		return xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	return nil
}

func (r *fluentPVCBindingReconciler) deleteFluentPVCBinding(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	logger := ctrl.LoggerFrom(ctx).WithName("fluentPVCBindingReconciler").WithName("deleteFluentPVCBinding")
	if controllerutil.ContainsFinalizer(b, constants.FluentPVCBindingFinalizerName) {
		logger.Info(fmt.Sprintf("Remove the finalizer of fluentpvcbinding='%s' because the condition is 'FinalizerJobSucceeded'.", b.Name))
		controllerutil.RemoveFinalizer(b, constants.FluentPVCBindingFinalizerName)
		if err := r.Update(ctx, b); client.IgnoreNotFound(err) != nil {
			return xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	}
	logger.Info(fmt.Sprintf("Delete fluentpvcbinding='%s' because the finalizer jobs are succeeded", b.Name))
	if err := r.Delete(ctx, b, deleteOptionsBackground(&b.UID, &b.ResourceVersion)); client.IgnoreNotFound(err) != nil {
		return xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	return nil
}

func (r *fluentPVCBindingReconciler) updateConditionUnknownPVCLost(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	message := fmt.Sprintf("pvc='%s'(UID='%s') is lost.(fluentpvcbinding='%s')", b.Spec.PVC.Name, b.Spec.PVC.UID, b.Name)
	return r.updateConditionUnknown(ctx, b, "PVCLost", message)
}

func (r *fluentPVCBindingReconciler) updateConditionUnknownPodNotFoundLongTime(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	message := fmt.Sprintf(
		"Pod='%s'(UID='%s') is not found even though it hasn't been finalized since it became Ready status.(fluentpvcbinding='%s')",
		b.Spec.Pod.Name, b.Spec.Pod.UID, b.Name,
	)
	return r.updateConditionUnknown(ctx, b, "PodNotFound", message)
}

func (r *fluentPVCBindingReconciler) updateConditionUnknownPodAndPVCNotFound(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	message := fmt.Sprintf(
		"Both pod='%s'(UID='%s') and pvc='%s'(UID='%s') are not found even though it hasn't been finalized since it became Ready status.(fluentpvcbinding='%s')",
		b.Spec.Pod.Name, b.Spec.Pod.UID, b.Spec.PVC.Name, b.Spec.PVC.UID, b.Name,
	)
	return r.updateConditionUnknown(ctx, b, "PodAndPVCNotFound", message)
}

func (r *fluentPVCBindingReconciler) updateConditionUnknownPodRunningPVCNotFound(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	message := fmt.Sprintf(
		"Pod='%s'(UID='%s') is running, but pvc='%s'(UID='%s') is not found.(fluentpvcbinding='%s')",
		b.Spec.Pod.Name, b.Spec.Pod.UID, b.Spec.PVC.Name, b.Spec.PVC.UID, b.Name,
	)
	return r.updateConditionUnknown(ctx, b, "PodRunningButPVCNotFound", message)
}

func (r *fluentPVCBindingReconciler) updateConditionUnknownPodCompletedPVCNotFound(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	message := fmt.Sprintf(
		"pod='%s'(UID='%s') is completed, but pvc='%s'(UID='%s') is not found.(fluentpvcbinding='%s')",
		b.Spec.Pod.Name, b.Spec.Pod.UID, b.Spec.PVC.Name, b.Spec.PVC.UID, b.Name,
	)
	return r.updateConditionUnknown(ctx, b, "PodCompletedPVCNotFound", message)
}

func (r *fluentPVCBindingReconciler) updateConditionUnknown(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding, reason, message string) error {
	logger := ctrl.LoggerFrom(ctx).WithName("fluentPVCBindingReconciler").WithName("updateConditionUnknown")
	logger.Error(xerrors.New(message), message)
	b.SetConditionUnknown(reason, message)
	if err := r.Status().Update(ctx, b); err != nil {
		return xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	return nil
}

func (r *fluentPVCBindingReconciler) updateConditionOutOfUsePodDeletedPVCFound(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	message := fmt.Sprintf(
		"pod='%s'(UID='%s') is deleted and pvc='%s'(UID='%s') is found.(fluentpvcbinding='%s')",
		b.Spec.Pod.Name, b.Spec.Pod.UID, b.Spec.PVC.Name, b.Spec.PVC.UID, b.Name,
	)
	return r.updateConditionOutOfUse(ctx, b, "PodDeletedPVCFound", message)
}

func (r *fluentPVCBindingReconciler) updateConditionOutOfUsePodCompletedPVCFound(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	message := fmt.Sprintf(
		"pod='%s'(UID='%s') is completed and pvc='%s'(UID='%s') is found.(fluentpvcbinding='%s')",
		b.Spec.Pod.Name, b.Spec.Pod.UID, b.Spec.PVC.Name, b.Spec.PVC.UID, b.Name,
	)
	return r.updateConditionOutOfUse(ctx, b, "PodCompletedPVCFound", message)
}

func (r *fluentPVCBindingReconciler) updateConditionOutOfUse(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding, reason, message string) error {
	return r.updateCondition(ctx, b, reason, message, b.SetConditionOutOfUse)
}

func (r *fluentPVCBindingReconciler) updateConditionReadyPodFoundPVCFound(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	message := fmt.Sprintf(
		"Both pod='%s'(UID='%s') and pvc='%s'(UID='%s') are found.(fluentpvcbinding='%s')",
		b.Spec.Pod.Name, b.Spec.Pod.UID, b.Spec.PVC.Name, b.Spec.PVC.UID, b.Name,
	)
	return r.updateConditionReady(ctx, b, "PodFoundPVCFound", message)
}

func (r *fluentPVCBindingReconciler) updateConditionReady(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding, reason, message string) error {
	return r.updateCondition(ctx, b, reason, message, b.SetConditionReady)
}

func (r *fluentPVCBindingReconciler) updateCondition(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding, reason, message string, conditionUpdateFunc func(string, string)) error {
	logger := ctrl.LoggerFrom(ctx).WithName("fluentPVCBindingReconciler").WithName("updateCondition")
	logger.Info(message)
	conditionUpdateFunc(reason, message)
	if err := r.Status().Update(ctx, b); err != nil {
		return xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	return nil
}

func (r *fluentPVCBindingReconciler) updateConditionByFinalizerJobStatus(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	logger := ctrl.LoggerFrom(ctx).WithName("fluentPVCBindingReconciler").WithName("updateConditionByFinalizerJobStatus")
	logger.Info(fmt.Sprintf("Check the finalizer jobs for fluentpvcbinding='%s'.", b.Name))
	jobs := &batchv1.JobList{}
	if err := r.List(ctx, jobs, matchingOwnerControllerField(b.Name)); client.IgnoreNotFound(err) != nil {
		return xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	if len(jobs.Items) == 0 {
		reason := "FinalizerJobNotFound"
		message := fmt.Sprintf("Finalizer jobs for fluentpvcbinding='%s' is not found.", b.Name)
		needUpdate := false
		if b.IsConditionFinalizerJobApplied() {
			b.SetConditionNotFinalizerJobApplied(reason, message)
			needUpdate = true
		}
		if b.IsConditionFinalizerJobFailed() {
			b.SetConditionNotFinalizerJobFailed(reason, message)
			needUpdate = true
		}
		if needUpdate {
			if err := r.Status().Update(ctx, b); err != nil {
				return xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		}
		logger.Info(fmt.Sprintf("Wait for applying some finalizer jobs for fluentpvcbinding='%s'", b.Name))
		return nil
	}
	if len(jobs.Items) != 1 {
		reason := "MultipleFinalizerJobsFound"
		var jobNames []string
		for _, j := range jobs.Items {
			jobNames = append(jobNames, j.Name)
		}
		message := fmt.Sprintf("Found an illegal state that multiple finalizer jobs [%s] are found.", strings.Join(jobNames, ","))
		logger.Error(xerrors.New(message), message)
		b.SetConditionUnknown(reason, message)
		if err := r.Status().Update(ctx, b); err != nil {
			return xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
		return xerrors.New(message)
	}
	j := &jobs.Items[0]
	needUpdate := false
	if !b.IsConditionFinalizerJobApplied() {
		needUpdate = true
		message := fmt.Sprintf("Update the status fluentpvcbinding='%s' 'FinalizerJobApplied' because some finalizer jobs are already applied: %+v", b.Name, j.Name)
		logger.Info(message)
		b.SetConditionFinalizerJobApplied("FinalizerJobFound", message)
	}
	if isJobSucceeded(j) {
		needUpdate = true
		message := fmt.Sprintf("Update the status fluentpvcbinding='%s' 'FinalizerJobSucceeded' because the finalizer job='%s' is succeeded", b.Name, j.Name)
		logger.Info(message)
		b.SetConditionFinalizerJobSucceeded("FinalizerJobSucceeded", message)
	}
	if isJobFailed(j) {
		needUpdate = true
		message := fmt.Sprintf("Update the status fluentpvcbinding='%s' 'FinalizerJobFailed' because the finalizer job='%s' is failed.", b.Name, j.Name)
		logger.Info(message)
		b.SetConditionFinalizerJobFailed("FinalizerJobFailed", message)
	}
	if needUpdate {
		if err := r.Status().Update(ctx, b); err != nil {
			return xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	}
	return nil
}

func (r *fluentPVCBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&batchv1.Job{},
		constants.OwnerControllerField,
		indexJobByOwnerFluentPVCBinding,
	); err != nil {
		return xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	ch := make(chan event.GenericEvent)
	watcher := &fluentPVCBindingWatcher{
		client:    mgr.GetClient(),
		ch:        ch,
		listLimit: 300,             // TODO: make it configurable.
		tick:      5 * time.Second, // TODO: make it configurable.
	}
	if err := mgr.Add(watcher); err != nil {
		return xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	src := source.Channel{Source: ch}
	pred := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return true },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&fluentpvcv1alpha1.FluentPVCBinding{}).
		Owns(&batchv1.Job{}).
		WithEventFilter(pred).
		Watches(&src, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

type fluentPVCBindingWatcher struct {
	client    client.Client
	ch        chan<- event.GenericEvent
	listLimit int64
	tick      time.Duration
}

func (w *fluentPVCBindingWatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.fireEvent(context.Background()); err != nil {
				return xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		}
	}
}

func (w *fluentPVCBindingWatcher) fireEvent(ctx context.Context) error {
	token := ""
	for {
		bindingList := &fluentpvcv1alpha1.FluentPVCBindingList{}
		if err := w.client.List(ctx, bindingList, &client.ListOptions{
			Limit:    w.listLimit,
			Continue: token,
		}); err != nil {
			return xerrors.Errorf("Unexpected error occurred.: %w", err)
		}

		for _, b := range bindingList.Items {
			w.ch <- event.GenericEvent{
				Object: b.DeepCopy(),
			}
		}

		token = bindingList.ListMeta.Continue
		if len(token) == 0 {
			return nil
		}
	}
}
