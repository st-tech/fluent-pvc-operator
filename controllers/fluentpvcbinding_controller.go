package controllers

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/xerrors"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
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

type FluentPVCBindingReconciler struct {
	client.Client
	APIReader client.Reader
	Log       logr.Logger
	Scheme    *runtime.Scheme
}

func (r *FluentPVCBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("controllers").WithName("fluentpvcbinding_controller")

	b := &fluentpvcv1alpha1.FluentPVCBinding{}
	if err := r.Get(ctx, req.NamespacedName, b); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}

	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	if err := r.Get(ctx, client.ObjectKey{Name: b.Spec.FluentPVCName}, fpvc); err != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	if err := updateOrNothingControllerReference(ctx, r.Client, fpvc, b); err != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}

	if b.IsConditionUnknown() {
		logger.Info(fmt.Sprintf("Skip processing because fluentpvcbinding='%s' is unknown status.", b.Name))
		return ctrl.Result{}, nil
	}

	podName := b.Spec.PodName
	pod := &corev1.Pod{}
	podFound := true
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: podName}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			podFound = false
		} else {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	}
	pvcName := b.Spec.PVCName
	pvc := &corev1.PersistentVolumeClaim{}
	pvcFound := true
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: pvcName}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			pvcFound = false
		} else {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	}
	if pvcFound {
		if err := updateOrNothingControllerReference(ctx, r.Client, b, pvc); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
		if pvc.Status.Phase == corev1.ClaimLost {
			message := fmt.Sprintf("pvc='%s' is lost.", pvcName)
			logger.Error(xerrors.New(message), message)
			b.SetConditionUnknown("PVCLost", message)
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		}
	}

	switch {
	case !podFound && !pvcFound:
		if !b.IsConditionReady() {
			if isCreatedBefore(b, 1*time.Hour) { // TODO: make it configurable?
				message := fmt.Sprintf("Both pod='%s' and pvc='%s' are not found even though it hasn't been finalized since it became Ready status.", podName, pvcName)
				logger.Error(xerrors.New(message), message)
				b.SetConditionUnknown("PodAndPVCNotFound", message)
				if err := r.Status().Update(ctx, b); err != nil {
					return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
				}
			} else {
				logger.Info(fmt.Sprintf("Skip processing until the next reconciliation because pod='%s' and pvc='%s' are not found.", podName, pvcName))
			}
			return ctrl.Result{}, nil
		}
		if !b.IsConditionFinalizerJobSucceeded() {
			message := fmt.Sprintf("Both pod='%s' and pvc='%s' are not found even though it hasn't been finalized since it became Ready status.", podName, pvcName)
			logger.Error(xerrors.New(message), message)
			b.SetConditionUnknown("PodAndPVCNotFound", message)
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
			return ctrl.Result{}, nil
		}

		logger.Info(fmt.Sprintf(
			"Delete fluentpvcbinding='%s' because it is already finalized and both pod='%s' and pvc='%s' are already deleted.", b.Name, podName, pvcName,
		))
		if err := r.Delete(ctx, b); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	case !podFound && pvcFound:
		if b.IsConditionFinalizerJobSucceeded() {
			if controllerutil.ContainsFinalizer(pvc, constants.PVCFinalizerName) {
				logger.Info(fmt.Sprintf("Skip processing because the finalizer of fluentpvcbinding='%s' is not removed.", b.Name))
				return ctrl.Result{}, nil
			}
			logger.Info(fmt.Sprintf("Delete fluentpvcbinding='%s' because the finalizer jobs are succeeded", b.Name))
			if err := r.Delete(ctx, b); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
			return ctrl.Result{}, nil
		}
		if !b.IsConditionReady() {
			if isCreatedBefore(b, 1*time.Hour) { // TODO: make it configurable?
				message := fmt.Sprintf("Pod='%s' is not found even though it hasn't been finalized since it became Ready status.", podName)
				logger.Error(xerrors.New(message), message)
				b.SetConditionUnknown("PodNotFound", message)
				if err := r.Status().Update(ctx, b); err != nil {
					return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
				}
			} else {
				logger.Info(fmt.Sprintf("Skip processing until the next reconciliation because pod='%s' is not found.", podName))
			}
			return ctrl.Result{}, nil
		}
		if !b.IsConditionOutOfUse() {
			// fluentpvc is ready, not out of use.
			message := fmt.Sprintf("Update the status of fluentpvcbinding='%s' 'OutOfUse' because it is ready status but pod='%s' is not found.", b.Name, podName)
			logger.Info(message)
			b.SetConditionOutOfUse("OutOfUse", message)
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
			return ctrl.Result{}, nil
		}

		logger.Info(fmt.Sprintf("Check the finalizer jobs for fluentpvcbinding='%s'.", b.Name))
		jobs := &batchv1.JobList{}
		if err := r.List(ctx, jobs, client.MatchingFields(map[string]string{constants.OwnerControllerField: b.Name})); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
		if len(jobs.Items) == 0 {
			logger.Info(fmt.Sprintf("Wait for applying some finalizer jobs for fluentpvcbinding='%s'", b.Name))
			return ctrl.Result{}, nil
		}
		message := fmt.Sprintf("Update the status fluentpvcbinding='%s' 'FinalizerJobApplied' because some finalizer jobs are already applied: %+v", b.Name, jobs.Items)
		logger.Info(message)
		b.SetConditionFinalizerJobApplied("FinalizerJobFound", message)

		for _, j := range jobs.Items {
			if !isJobFinished(&j) {
				continue
			}
			if isJobSucceeded(&j) {
				message := fmt.Sprintf("Update the status fluentpvcbinding='%s' 'FinalizerJobSucceeded' because the finalizer job is succeeded: %+v", b.Name, j)
				logger.Info(message)
				b.SetConditionFinalizerJobSucceeded("FinalizerJobSucceeded", message)
				break
			}
			message := fmt.Sprintf("Update the status fluentpvcbinding='%s' 'FinalizerJobFailed' because the finalizer job is failed: %+v", b.Name, j)
			logger.Info(message)
			b.SetConditionFinalizerJobFailed("FinalizerJobFailed", message)
		}
		if err := r.Status().Update(ctx, b); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	case podFound && !pvcFound:
		switch pod.Status.Phase {
		case corev1.PodPending:
		case corev1.PodRunning:
			message := fmt.Sprintf("Pod='%s' is found and ready, but pvc='%s' is not found.", podName, pvcName)
			logger.Error(xerrors.New(message), message)
			b.SetConditionUnknown("PodFoundAndReadyButPVCNotFound", message)
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		case corev1.PodSucceeded, corev1.PodFailed:
			if !b.IsConditionFinalizerJobSucceeded() {
				message := fmt.Sprintf("Pod='%s' is finished and pvc='%s' is not found, but fluentpvcbinding='%s' is not finalized.", podName, pvcName, b.Name)
				logger.Error(xerrors.New(message), message)
				b.SetConditionUnknown("PodFoundAndFinishedAndPVCNotFoundButFluentPVCBindingNotFinalized", message)
				if err := r.Status().Update(ctx, b); err != nil {
					return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
				}
				return ctrl.Result{}, nil
			}
			logger.Info(fmt.Sprintf("Delete fluentpvcbinding='%s' because the finalizer job is succeeded.", b.Name))
			if err := r.Delete(ctx, b); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		case corev1.PodUnknown:
			logger.Info("Skip processing because pod='%s' is unknown status and pvc='%s' is not found", podName, pvcName)
		}
	case podFound && pvcFound:
		switch pod.Status.Phase {
		case corev1.PodPending, corev1.PodRunning:
			message := fmt.Sprintf("Both pod='%s' and pvc='%s' are found", podName, pvcName)
			b.SetConditionReady("PodAndPVCFound", message)
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		case corev1.PodSucceeded, corev1.PodFailed:
			if !b.IsConditionReady() {
				message := fmt.Sprintf("Both pod='%s' and pvc='%s' are found", podName, pvcName)
				b.SetConditionReady("PodAndPVCFound", message)
			}
			message := fmt.Sprintf(
				"fluentpvcbinding='%s' is out of use because pod='%s' is %s and pvc='%s' are found",
				b.Name, podName, pod.Status.Phase, pvcName,
			)
			b.SetConditionReady("PodAndPVCFound", message)
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		case corev1.PodUnknown:
			logger.Info("Skip processing because pod='%s' is unknown status and pvc='%s' is found", podName, pvcName)
		}
	}

	return ctrl.Result{}, nil
}

func (r *FluentPVCBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&batchv1.Job{},
		constants.OwnerControllerField,
		indexJobByOwnerFluentPVCBinding,
	); err != nil {
		return xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	logger := log.FromContext(ctx).
		WithName("controllers").
		WithName("fluentpvcbinding_controller").
		WithName("fluentPVCBindingWatcher")
	ch := make(chan event.GenericEvent)
	watcher := &fluentPVCBindingWatcher{
		client:    mgr.GetClient(),
		logger:    logger,
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
	logger    logr.Logger
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
