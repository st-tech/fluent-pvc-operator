package controllers

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/xerrors"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
	podutils "github.com/st-tech/fluent-pvc-operator/utils/pod"
)

//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs,verbs=get;list;watch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings,verbs=get;list;watch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings/status,verbs=get
//+kubebuilder:rbac:groups="batch",resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;update

type pvcReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func NewPVCReconciler(mgr ctrl.Manager) *pvcReconciler {
	return &pvcReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
}

func (r *pvcReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("pvcReconciler").WithName("Reconcile")

	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, req.NamespacedName, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	b := &fluentpvcv1alpha1.FluentPVCBinding{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: pvc.Name}, b); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	if pvc.UID != b.Spec.PVC.UID {
		logger.Info(fmt.Sprintf(
			"Skip processing because pvc.UID='%s' is different from fluentpvcbinding.Spec.PVC.UID='%s' for name='%s'.",
			pvc.UID, b.Spec.PVC.UID, pvc.Name,
		))
		return ctrl.Result{}, nil
	}
	if b.IsConditionUnknown() {
		logger.Info(fmt.Sprintf("fluentpvcbinding='%s' is unknown status, so skip processing.", b.Name))
		return ctrl.Result{}, nil
	}
	if !b.IsConditionOutOfUse() {
		logger.Info(fmt.Sprintf("fluentpvcbinding='%s' is not out of use yet.", b.Name))
		return requeueResult(10 * time.Second), nil
	}

	logger.Info(fmt.Sprintf(
		"pvc='%s' is finalizing because the status of fluentpvcbinding='%s' is OutOfUse.",
		pvc.Name, b.Name,
	))
	if !b.IsConditionFinalizerJobApplied() {
		jobs := &batchv1.JobList{}
		if err := r.List(ctx, jobs, matchingOwnerControllerField(b.Name)); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
		if len(jobs.Items) != 0 {
			logger.Info(fmt.Sprintf(
				"fluentpvcbinding='%s' status indicates any finalizer job is not applied, but some jobs are found: %+v",
				b.Name, jobs.Items,
			))
			return requeueResult(10 * time.Second), nil
		}
		fpvc := &fluentpvcv1alpha1.FluentPVC{}
		if err := r.Get(ctx, client.ObjectKey{Name: metav1.GetControllerOf(b).Name}, fpvc); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}

		j := &batchv1.Job{}
		j.SetName(b.Name)
		j.SetNamespace(b.Namespace)
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, j, func() error {
			j.Spec = *fpvc.Spec.PVCFinalizerJobSpecTemplate.DeepCopy()
			podutils.InjectOrReplaceVolume(&j.Spec.Template.Spec, &corev1.Volume{
				Name: fpvc.Spec.VolumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvc.Name,
					},
				},
			})
			podutils.InjectOrReplaceVolumeMount(&j.Spec.Template.Spec, &corev1.VolumeMount{
				Name:      fpvc.Spec.VolumeName,
				MountPath: fpvc.Spec.CommonMountPath,
			})
			for _, e := range fpvc.Spec.CommonEnv {
				podutils.InjectOrReplaceEnv(&j.Spec.Template.Spec, e.DeepCopy())
			}
			return ctrl.SetControllerReference(b, j, r.Scheme)
		}); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	}
	if !b.IsConditionFinalizerJobSucceeded() && !b.IsConditionFinalizerJobFailed() {
		logger.Info(fmt.Sprintf(
			"pvc='%s' is finalizing by fluentpvcbinding='%s'.",
			pvc.Name, b.Name,
		))
		return requeueResult(10 * time.Second), nil
	}

	if b.IsConditionFinalizerJobFailed() {
		logger.Info(fmt.Sprintf("Skip processing because the finalizer job='%s' is failed.", b.Name))
		return requeueResult(10 * time.Second), nil
	}

	logger.Info(fmt.Sprintf("Remove the finalizer='%s' from pvc='%s'", constants.PVCFinalizerName, pvc.Name))
	controllerutil.RemoveFinalizer(pvc, constants.PVCFinalizerName)
	if err := r.Update(ctx, pvc); client.IgnoreNotFound(err) != nil {
		if apierrors.IsConflict(err) {
			// NOTE: Conflict with deleting the pvc in other pvcReconciler#Reconcile.
			return requeueResult(10 * time.Second), nil
		}
		return ctrl.Result{}, xerrors.Errorf(
			"Failed to remove finalizer from PVC='%s'.: %w",
			pvc.Name, err,
		)
	}
	logger.Info(fmt.Sprintf("Delete pvc='%s' because it is finalized.", pvc.Name))
	if err := r.Delete(ctx, pvc, deleteOptionsBackground(&pvc.UID, &pvc.ResourceVersion)); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *pvcReconciler) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}

	// NOTE: Avoid 'indexer conflict: map[field:.metadata.ownerReference.controller:{}]'
	// ctx := context.Background()
	// if err := mgr.GetFieldIndexer().IndexField(ctx,
	// 	&batchv1.Job{},
	// 	constants.OwnerControllerField,
	// 	indexJobByOwnerFluentPVCBinding,
	// ); err != nil {
	// 	return xerrors.Errorf("Unexpected error occurred.: %w", err)
	// }

	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(pred).
		For(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}
