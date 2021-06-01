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

//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs/finalizers,verbs=update
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="batch",resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;delete

type PVCReconciler struct {
	client.Client
	APIReader client.Reader
	Log       logr.Logger
	Scheme    *runtime.Scheme
}

func (r *PVCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("controllers").WithName("PVC")

	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, req.NamespacedName, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	if owner := metav1.GetControllerOf(pvc); !isOwnerFluentPVCBinding(owner) {
		return ctrl.Result{}, nil
	}
	// TODO: Can we attach the pvc to finalizer job?
	if pvc.DeletionTimestamp == nil {
		logger.Info("deleted not yet")
		return ctrl.Result{}, nil
	}
	// TODO: binding ready status check here?
	// TODO: Should we always requeue if an error occurs in the code below this?

	for _, f := range pvc.Finalizers {
		if f == constants.PVCFinalizerName {
			continue
		}
		logger.Info(fmt.Sprintf(
			"Requeue until other finalizers complete their jobs. (finalizer='%s' is found.)",
			f,
		))
		return requeueResult(10 * time.Second), nil
	}

	b := &fluentpvcv1alpha1.FluentPVCBinding{}
	{
		owner := metav1.GetControllerOf(pvc)
		if err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: owner.Name}, b); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	}
	if !isFluentPVCBindingOutOfUse(b) {
		logger.Info(fmt.Sprintf("FluentPVCBinding='%s' is not out of use yet.", b.Name))
		return requeueResult(10 * time.Second), nil
	}
	if isFluentPVCBindingFinalizerJobApplied(b) {
		jobs := &batchv1.JobList{}
		if err := r.List(ctx, jobs, client.MatchingFields(map[string]string{constants.OwnerControllerField: b.Name})); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
		if len(jobs.Items) == 0 {
			return ctrl.Result{}, xerrors.New(fmt.Sprintf("Any job is not found even though it should have been applied. (FluentPVCBinding='%s')", b.Name))
		}
		if len(jobs.Items) != 1 {
			return ctrl.Result{}, xerrors.New(fmt.Sprintf(
				"Found %d jobs even though only one job should have been applied. (FluentPVCBinding='%s')",
				len(jobs.Items), b.Name,
			))
		}
		j := &jobs.Items[0]
		if !isJobFinished(j) {
			logger.Info(fmt.Sprintf("FluentPVCBinding='%s' already apply the finalizer job='%s'.", b.Name, j.Name))
			return requeueResult(10 * time.Second), nil
		}
		pvc.Finalizers = nil
		if err := r.Update(ctx, pvc); err != nil {
			return ctrl.Result{}, xerrors.Errorf(
				"Failed to remove finalizer from PVC='%s'.: %w",
				pvc.Name, err,
			)
		}
		logger.Info(fmt.Sprintf("PVC='%s' is finalized.", pvc.Name))
		return ctrl.Result{}, nil
	}

	logger.Info(fmt.Sprintf("Apply a finalizer job for PVC='%s'", pvc.Name))
	{
		// NOTE: Check for the existence of jobs considering the case of status update delay.
		jobs := &batchv1.JobList{}
		if err := r.List(ctx, jobs, client.MatchingFields(map[string]string{constants.OwnerControllerField: b.Name})); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
			}
		}
		if len(jobs.Items) != 0 {
			logger.Info(fmt.Sprintf(
				"FluentPVCBinding='%s' status indicates any finalizer job is not applied, but some jobs are found: %+v",
				b.Name, jobs.Items,
			))
			return requeueResult(10 * time.Second), nil
		}
	}
	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	{
		owner := metav1.GetControllerOf(b)
		if err := r.Get(ctx, client.ObjectKey{Name: owner.Name}, fpvc); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
		}
	}
	j := &batchv1.Job{}
	j.SetName(b.Name)
	j.SetNamespace(b.Namespace)
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, j, func() error {
		j.Spec = *fpvc.Spec.PVCFinalizerJobSpecTemplate.DeepCopy()

		volumes := []corev1.Volume{}
		{
			for _, v := range j.Spec.Template.Spec.Volumes {
				if v.Name == fpvc.Spec.VolumeName {
					logger.Info(fmt.Sprintf(
						"Replace with the PVC='%s' though Job='%s' has the Volume='%v'.",
						pvc.Name, j.Name, v,
					))
					continue
				}
				volumes = append(volumes, *v.DeepCopy())
			}
			volumes = append(volumes, corev1.Volume{
				Name: fpvc.Spec.VolumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvc.Name,
					},
				},
			})
		}
		j.Spec.Template.Spec.Volumes = volumes
		containers := []corev1.Container{}
		{
			for _, c := range j.Spec.Template.Spec.Containers {
				containers = append(containers, *c.DeepCopy())
			}
			for i, c := range containers {
				volumeMounts := []corev1.VolumeMount{}
				for _, vm := range c.VolumeMounts {
					if vm.Name == fpvc.Spec.VolumeName {
						logger.Info(fmt.Sprintf(
							"Replace with the PVC='%s' though Container='%s' has the VolumeMount='%v'.",
							pvc.Name, c.Name, vm,
						))
						continue
					}
					volumeMounts = append(volumeMounts, *vm.DeepCopy())
				}
				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      fpvc.Spec.VolumeName,
					MountPath: fpvc.Spec.CommonMountPath,
				})
				c.VolumeMounts = volumeMounts
				containers[i] = c
			}
			for i, c := range containers {
				envs := []corev1.EnvVar{}
			OUTER:
				for _, e := range c.Env {
					for _, ce := range fpvc.Spec.CommonEnv {
						if e.Name == ce.Name {
							logger.Info(
								"Replace with Env='%v' though Container='%s' has the Env='%v'",
								ce, c.Name, e,
							)
							continue OUTER
						}
					}
					envs = append(envs, e)
				}
				for _, ce := range fpvc.Spec.CommonEnv {
					envs = append(envs, *ce.DeepCopy())
				}
				c.Env = envs
				containers[i] = c
			}
		}
		j.Spec.Template.Spec.Containers = containers
		return ctrl.SetControllerReference(b, j, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *PVCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}

	ctx := context.Background()
	if err := mgr.GetFieldIndexer().IndexField(ctx,
		&batchv1.Job{},
		constants.OwnerControllerField,
		indexJobByOwnerFluentPVCBinding,
	); err != nil {
		return xerrors.Errorf("Unexpected error occurred when indexing Job by FluentPVCBinding caused by: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(pred).
		For(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}
