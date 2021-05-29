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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
)

const (
	fluentPVCBindingWatcherInterval  = 5 * time.Second
	fluentPVCBindingWatcherListLimit = 300
	ownerControllerField             = ".metadata.ownerReference.controller"
)

/*
TODO: Adjust the message: xerrors.Errorf("Caused by: %w", err)
TODO: CreateOrUpdate(PVC Name = Pod Name) on Webhook
*/
type FluentPVCReconciler struct {
	client.Client
	APIReader client.Reader
	Log       logr.Logger
	Scheme    *runtime.Scheme
}

//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs/finalizers,verbs=update
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcbindings/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="batch",resources=jobs,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;delete

func (r *FluentPVCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	b := &fluentpvcv1alpha1.FluentPVCBinding{}
	if err := r.Get(ctx, req.NamespacedName, b); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, xerrors.Errorf(
				"Unexpected error occurred when finding FluentPVCBinding='%s' caused by: %w",
				req.Name,
				err,
			)
		}
		return ctrl.Result{}, nil
	}
	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	if err := r.Get(ctx, client.ObjectKey{Name: b.Spec.FluentPVCName}, fpvc); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, xerrors.Errorf(
				"Unexpected error occurred when finding FluentPVC='%s' caused by: %w",
				b.Spec.FluentPVCName,
				err,
			)
		}
		return ctrl.Result{}, nil
	}
	if owner := metav1.GetControllerOf(b); !isOwnerFluentPVC(owner) {
		logger.Info(fmt.Sprintf("Set ownerReference to FluentPVCBinding='%s' from FluentPVC='%s'", b.Name, fpvc.Name))
		if err := ctrl.SetControllerReference(fpvc, b, r.Scheme); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when setting owner reference caused by: %w", err)
		}
		if err := r.Update(ctx, b); err != nil {
			return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when setting owner reference caused by: %w", err)
		}
	}
	logger.V(2).Info(fmt.Sprintf("Process FluentPVCBinding: %v", b))
	if !meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionUnknown)) {
		logger.V(2).Info(fmt.Sprintf(
			"Skip to process FluentPVCBinding='%s' because the status is '%s'",
			b.Name, fluentpvcv1alpha1.FluentPVCBindingConditionUnknown,
		))
		return ctrl.Result{}, nil
	}

	pod := &corev1.Pod{}
	podFound := true
	if err := r.Get(ctx,
		client.ObjectKey{Namespace: req.Namespace, Name: b.Spec.PodName},
		pod,
	); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, xerrors.Errorf(
				"Unexpected error occurred when finding pod='%s' caused by: %w",
				b.Spec.PodName,
				err,
			)
		}
		podFound = false
	}
	pvc := &corev1.PersistentVolumeClaim{}
	pvcFound := true
	if err := r.Get(ctx,
		client.ObjectKey{Namespace: req.Namespace, Name: b.Spec.PVCName},
		pvc,
	); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, xerrors.Errorf(
				"Unexpected error occurred when finding pvc='%s' caused by: %w",
				b.Spec.PVCName,
				err,
			)
		}
		pvcFound = false
	}
	// pod phases = [Pending, Running, Succeeded, Failed, Unknown]
	// pvc phases = [Pending, Bound, Lost]
	// b phases = [Ready, OutOfUse, FinalizerPodApplied, Finalized, Unknown]
	switch {
	case !podFound && !pvcFound:
		if !meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionReady)) {
			creationThreshold := metav1.NewTime(time.Now().Add(-1 * time.Hour))
			if b.CreationTimestamp.Before(&creationThreshold) {
				meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
					Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionUnknown),
					Status:  metav1.ConditionTrue,
					Reason:  "Unknown",
					Message: "Both Pod and PVC are not found for more than 1 hour.",
				})
				if err := r.Status().Update(ctx, b); err != nil {
					return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
				}
				return ctrl.Result{}, xerrors.New(fmt.Sprintf("FluentPVCBinding='%s' is not ready for more than 1 hour", b.Name))
			}
			logger.Info(fmt.Sprintf(
				"Skip until the next reconciliation loop because Pod=%s and PVC=%s is not found.",
				b.Spec.PodName,
				b.Spec.PVCName,
			))
			return ctrl.Result{}, nil
		}
		if !meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobSucceeded)) {
			meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
				Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionUnknown),
				Status:  metav1.ConditionTrue,
				Reason:  "Unknown",
				Message: "Both Pod and PVC are not found even though it hasn't been finalized since it went to Ready status.",
			})
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
			}
			return ctrl.Result{}, xerrors.New(fmt.Sprintf(
				"FluentPVCBinding='%s' is not finalized even though it hasn't been finalized since it went to Ready status.",
				b.Name,
			))
		}
		// FluentPVCBinding is finalized status.
		logger.Info(fmt.Sprintf(
			"Delete FluentPVCBinding='%s' because this is already finalized and both the pod='%s' and the pvc='%s' are already deleted.",
			b.Name,
			b.Spec.PodName,
			b.Spec.PVCName,
		))
		if err := r.Delete(ctx, b); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when deleting FluentPVCBinding caused by: %w", err)
			}
		}
		return ctrl.Result{}, nil
	case !podFound && pvcFound:
		if owner := metav1.GetControllerOf(pvc); !isOwnerFluentPVCBinding(owner) {
			logger.Info(fmt.Sprintf("Set ownerReference to PVC='%s' from FluentPVCBinding='%s'", pvc.Name, b.Name))
			if err := ctrl.SetControllerReference(b, pvc, r.Scheme); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when setting owner reference caused by: %w", err)
			}
			if err := r.Update(ctx, pvc); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when setting owner reference caused by: %w", err)
			}
		}
		if !meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionReady)) {
			creationThreshold := metav1.NewTime(time.Now().Add(-1 * time.Hour))
			if b.CreationTimestamp.Before(&creationThreshold) {
				meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
					Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionUnknown),
					Status:  metav1.ConditionTrue,
					Reason:  "Unknown",
					Message: "PVC is found, but Pod is not found for more than 1 hour.",
				})
				if err := r.Status().Update(ctx, b); err != nil {
					return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
				}
				// TODO: delete option is needed when orphan pvc is found?
				// NOTE: The owner of PVC is FluentPVCBinding, so delete the PVC when deleting the FluentPVCBinding.
				return ctrl.Result{}, xerrors.New(fmt.Sprintf("FluentPVCBinding='%s' is not ready for more than 1 hour", b.Name))
			}
			logger.V(1).Info(fmt.Sprintf(
				"PVC='%s' is found, but Pod='%s' is not found. Maybe the Pod is initializing?",
				b.Spec.PVCName,
				b.Spec.PodName,
			))
			return ctrl.Result{}, nil
		}
		if !meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionOutOfUse)) {
			logger.Info(fmt.Sprintf(
				"FluentPVCBinding='%s' is out of use because PVC='%s' is found and Pod='%s' is not found.",
				b.Name,
				b.Spec.PVCName,
				b.Spec.PodName,
			))
			meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
				Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionOutOfUse),
				Status:  metav1.ConditionTrue,
				Reason:  "OutOfUse",
				Message: "FluentPVCBinding is ready, but Pod is already not found.",
			})
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
			}
			return ctrl.Result{}, nil
		}
		if !meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobApplied)) {
			j := &batchv1.Job{}
			j.SetNamespace(b.Namespace)
			j.SetName(b.Name)
			if _, err := ctrl.CreateOrUpdate(ctx, r.Client, j, func() error {
				j.Spec = *fpvc.Spec.PVCFinalizerJobSpecTemplate.DeepCopy()
				pvcVolume := corev1.Volume{
					Name: fpvc.Spec.VolumeName,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc.Name,
						},
					},
				}
				j.Spec.Template.Spec.Volumes = append(j.Spec.Template.Spec.Volumes, pvcVolume)
				for i := range pod.Spec.Containers {
					j.Spec.Template.Spec.Containers[i].VolumeMounts = append(j.Spec.Template.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
						Name:      fpvc.Spec.VolumeName,
						MountPath: fpvc.Spec.CommonMountPath,
					})
					j.Spec.Template.Spec.Containers[i].Env = append(j.Spec.Template.Spec.Containers[i].Env, fpvc.Spec.CommonEnv...)
				}
				return ctrl.SetControllerReference(b, j, r.Scheme)
			}); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when creating or updating a finalizer job caused by: %w", err)
			}
			meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
				Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobApplied),
				Status:  metav1.ConditionTrue,
				Reason:  "FinalizerJobApplied",
				Message: "Finalizer Job is applied.",
			})
			if err := r.Status().Update(ctx, b); err != nil {
				// NOTE: Update again with job in the next reconciliation.
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
			}
			return ctrl.Result{}, nil
		}
		if !meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobSucceeded)) {
			if meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobFailed)) {
				return ctrl.Result{}, nil
			}
			jl := &batchv1.JobList{}
			if err := r.List(ctx, jl, client.MatchingFields(map[string]string{ownerControllerField: b.Name})); err != nil {
				if apierrors.IsNotFound(err) {
					logger.Info("Rerun finalizer job because the finalizer job is not found unexpectedly.")
					meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
						Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobApplied),
						Status:  metav1.ConditionFalse,
						Reason:  "FinalizerJobNotFound",
						Message: "Finalizer job is not found regardless it was applied.",
					})
					if err := r.Status().Update(ctx, b); err != nil {
						return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
					}
					return ctrl.Result{}, nil
				}
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when listing Jobs caused by: %w", err)
			}
			j := jl.Items[0].DeepCopy()
			if finished, t := getFinishedStatus(j); !finished {
				logger.V(1).Info(fmt.Sprintf("Finalizer job='%s' is running yet.", j.Name))
				return ctrl.Result{}, nil
			} else {
				if t == batchv1.JobFailed {
					logger.Info(fmt.Sprintf("Finalizer job='%s' is failed.", j.Name))
					meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
						Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobFailed),
						Status:  metav1.ConditionTrue,
						Reason:  "FinalizerJobFailed",
						Message: "Finalizer job is failed.",
					})
					if err := r.Status().Update(ctx, b); err != nil {
						return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
					}
					return ctrl.Result{}, nil
				}
				if t == batchv1.JobComplete {
					logger.V(1).Info(fmt.Sprintf("Finalizer job='%s' is succeeded.", j.Name))
					meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
						Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobSucceeded),
						Status:  metav1.ConditionTrue,
						Reason:  "FinalizerJobSucceeded",
						Message: "Finalizer job is succeeded.",
					})
					if err := r.Status().Update(ctx, b); err != nil {
						return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
					}
					return ctrl.Result{}, nil
				}
			}
		}
		// Finalizer job is succeeded.
		logger.Info(fmt.Sprintf("Delete FluentPVCBinding='%s' because the finalizer job is succeeded.", b.Name))
		if err := r.Delete(ctx, b); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when deleting FluentPVCBinding caused by: %w", err)
			}
		}
		return ctrl.Result{}, nil
	case podFound && !pvcFound:
		switch pod.Status.Phase {
		case corev1.PodPending:
			return ctrl.Result{}, nil
		case corev1.PodRunning:
			err := xerrors.New("Pod is found and ready, but pvc is not found.")
			logger.Error(err, "Pod is found and ready, but pvc is not found.")
			meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
				Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionUnknown),
				Status:  metav1.ConditionTrue,
				Reason:  "Unknown",
				Message: "Pod is found, but PVC is not found regardless the pod is running.",
			})
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
			}
			return ctrl.Result{}, err
		case corev1.PodSucceeded, corev1.PodFailed:
			if !meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobSucceeded)) || !meta.IsStatusConditionTrue(b.Status.Conditions, string(fluentpvcv1alpha1.FluentPVCBindingConditionFinalizerJobFailed)) {
				err := xerrors.New("Pod is finished and pvc is not found, but FluentPVCBinding is not finalized.")
				meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
					Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionUnknown),
					Status:  metav1.ConditionTrue,
					Reason:  "Unknown",
					Message: "Pod is finished and pvc is not found, but FluentPVCBinding is not finalized.",
				})
				if err := r.Status().Update(ctx, b); err != nil {
					return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
				}
				return ctrl.Result{}, err
			}
			// Finalizer job is succeeded.
			// TODO: Why is the PVC deleted? this is unknown case?
			logger.Info(fmt.Sprintf("Delete FluentPVCBinding='%s' because the finalizer job is succeeded.", b.Name))
			if err := r.Delete(ctx, b); err != nil {
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when deleting FluentPVCBinding caused by: %w", err)
				}
			}
			return ctrl.Result{}, nil
		case corev1.PodUnknown:
			err := xerrors.New("Pod is unknown and pvc is not found.")
			meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
				Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionUnknown),
				Status:  metav1.ConditionTrue,
				Reason:  "Unknown",
				Message: "Pod is unknown and pvc is not found.",
			})
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
			}
			return ctrl.Result{}, err
		}
	case podFound && pvcFound:
		if owner := metav1.GetControllerOf(pvc); !isOwnerFluentPVCBinding(owner) {
			logger.Info(fmt.Sprintf("Set ownerReference to PVC='%s' from FluentPVCBinding='%s'", pvc.Name, b.Name))
			if err := ctrl.SetControllerReference(b, pvc, r.Scheme); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when setting owner reference caused by: %w", err)
			}
			if err := r.Update(ctx, pvc); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when setting owner reference caused by: %w", err)
			}
		}
		switch pod.Status.Phase {
		case corev1.PodPending:
			logger.Info(fmt.Sprintf("pod='%s' is pending.", pod.Name))
			return ctrl.Result{}, nil
		case corev1.PodRunning:
			// pod watch
			for i := range pod.Status.ContainerStatuses {
				// TODO: Sidecar should be 1.
				if pod.Status.ContainerStatuses[i].Name != fpvc.Spec.SidecarContainerTemplate.Name {
					continue
				}
				status := pod.Status.ContainerStatuses[i]
				if status.RestartCount == 0 {
					break
				}
				// TODO: deletePodIfSidecarContainerTerminationDetected option
				if status.LastTerminationState.Terminated.ExitCode != 0 {
					logger.Info(fmt.Sprintf(
						"SidecarContainer='%s' termination is detected. (message='%s')",
						fpvc.Spec.SidecarContainerTemplate.Name,
						status.LastTerminationState.Terminated.Message,
					))
					if err := r.Delete(ctx, pod, &client.DeleteOptions{
						Preconditions: &metav1.Preconditions{
							UID:             &pod.UID,
							ResourceVersion: &pod.ResourceVersion,
						},
						// TODO: minimum seconds?
						GracePeriodSeconds: pod.GetDeletionGracePeriodSeconds(),
					}); err != nil {
						return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when deleting a pod caused by: %w", err)
					}
				}
			}
			return ctrl.Result{}, nil
		case corev1.PodSucceeded, corev1.PodFailed:
			logger.Info(fmt.Sprintf(
				"FluentPVCBinding='%s' is out of use because PVC='%s' is found and Pod='%s' is %s.",
				b.Name,
				b.Spec.PVCName,
				b.Spec.PodName,
				pod.Status.Phase,
			))
			meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
				Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionOutOfUse),
				Status:  metav1.ConditionTrue,
				Reason:  "OutOfUse",
				Message: "FluentPVCBinding is ready, but Pod is finished.",
			})
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
			}
			return ctrl.Result{}, nil
		case corev1.PodUnknown:
			err := xerrors.New("Pod is unknown and pvc is found.")
			meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
				Type:    string(fluentpvcv1alpha1.FluentPVCBindingConditionUnknown),
				Status:  metav1.ConditionTrue,
				Reason:  "Unknown",
				Message: "Pod is unknown and pvc is found.",
			})
			if err := r.Status().Update(ctx, b); err != nil {
				return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred when updating the FluentPVCBinding status caused by: %w", err)
			}
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
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

func (r *FluentPVCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	// todo: is required?
	if err := mgr.GetFieldIndexer().IndexField(ctx,
		&corev1.PersistentVolumeClaim{},
		ownerControllerField,
		indexPVCByOwnerFluentPVCBinding,
	); err != nil {
		return xerrors.Errorf("Unexpected error occurred when indexing PVC by FluentPVCBinding caused by: %w", err)
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx,
		&batchv1.Job{},
		ownerControllerField,
		indexJobByOwnerFluentPVCBinding,
	); err != nil {
		return xerrors.Errorf("Unexpected error occurred when indexing Job by FluentPVCBinding caused by: %w", err)
	}
	// todo: is required?
	if err := mgr.GetFieldIndexer().IndexField(ctx,
		&fluentpvcv1alpha1.FluentPVCBinding{},
		ownerControllerField,
		indexFluentPVCBindingByOwnerFluentPVC,
	); err != nil {
		return xerrors.Errorf("Unexpected error occurred when indexing FluentPVCBinding by FluentPVC caused by: %w", err)
	}

	logger := log.FromContext(ctx)
	ch := make(chan event.GenericEvent)
	watcher := &fluentPVCBindingWatcher{
		client:    mgr.GetClient(),
		logger:    logger,
		ch:        ch,
		listLimit: fluentPVCBindingWatcherListLimit,
		tick:      fluentPVCBindingWatcherListLimit,
	}
	if err := mgr.Add(watcher); err != nil {
		return xerrors.Errorf("Unexpected error occurred when adding watcher to manager caused by: %w", err)
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
				return xerrors.Errorf("Unexpected error occurred when fire events caused by: %w", err)
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
			return xerrors.Errorf("Unexpected error occurred when listing FluentPVCBindings caused by: %w", err)
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

func (r *FluentPVCReconciler) getPodsByPVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) ([]corev1.Pod, error) {
	var pods corev1.PodList
	// query directly to API server to avoid latency for cache updates
	err := r.APIReader.List(ctx, &pods, client.InNamespace(pvc.Namespace))
	if err != nil {
		return nil, err
	}

	var result []corev1.Pod
OUTER:
	for _, pod := range pods.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			if volume.PersistentVolumeClaim.ClaimName == pvc.Name {
				result = append(result, pod)
				continue OUTER
			}
		}
	}

	return result, nil
}

func getFinishedStatus(j *batchv1.Job) (bool, batchv1.JobConditionType) {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true, c.Type
		}
	}
	return false, ""
}

// IsJobFinished returns whether or not a job has completed successfully or failed.
func IsJobFinished(j *batchv1.Job) bool {
	isFinished, _ := getFinishedStatus(j)
	return isFinished
}

func IsJobSucceeded(j *batchv1.Job) bool {
	isFinished, t := getFinishedStatus(j)
	return isFinished && t == batchv1.JobComplete
}

func IsJobFailed(j *batchv1.Job) bool {
	isFinished, t := getFinishedStatus(j)
	return isFinished && t == batchv1.JobFailed
}

// https://github.com/kubernetes/kubernetes/blob/c495744436fc94ebbef2fcbeb97699ca96fe02dd/pkg/api/pod/util.go#L242-L272
// IsPodReady returns true if a pod is ready; false otherwise.
func IsPodReady(pod *corev1.Pod) bool {
	return IsPodReadyConditionTrue(pod.Status)
}

// IsPodReadyConditionTrue returns true if a pod is ready; false otherwise.
func IsPodReadyConditionTrue(status corev1.PodStatus) bool {
	condition := GetPodReadyCondition(status)
	return condition != nil && condition.Status == corev1.ConditionTrue
}

// GetPodReadyCondition extracts the pod ready condition from the given status and returns that.
// Returns nil if the condition is not present.
func GetPodReadyCondition(status corev1.PodStatus) *corev1.PodCondition {
	_, condition := GetPodCondition(&status, corev1.PodReady)
	return condition
}

// GetPodCondition extracts the provided condition from the given status and returns that.
// Returns nil and -1 if the condition is not present, and the index of the located condition.
func GetPodCondition(status *corev1.PodStatus, conditionType corev1.PodConditionType) (int, *corev1.PodCondition) {
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
