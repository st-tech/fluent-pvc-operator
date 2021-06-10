package controllers

import (
	"context"
	"fmt"

	"golang.org/x/xerrors"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
)

//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fluent-pvc-operator.tech.zozo.com,resources=fluentpvcs/finalizers,verbs=update

type fluentPVCReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func NewFluentPVCReconciler(mgr ctrl.Manager) *fluentPVCReconciler {
	return &fluentPVCReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
}

func (r *fluentPVCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("fluentPVCReconciler").WithName("Reconcile")
	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	if err := r.Get(ctx, req.NamespacedName, fpvc); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, xerrors.Errorf("Unexpected error occurred.: %w", err)
	}
	logger.Info(fmt.Sprintf("CreateOrUpdate fluentpvc='%s': %+v", fpvc.Name, fpvc))
	return ctrl.Result{}, nil
}

func (r *fluentPVCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&fluentpvcv1alpha1.FluentPVC{}).
		WithEventFilter(pred).
		Complete(r)
}
