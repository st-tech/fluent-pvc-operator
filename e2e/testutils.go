package e2e

import (
	"context"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/st-tech/fluent-pvc-operator/constants"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func deleteFluentPVC(ctx context.Context, c client.Client, n string) error {
	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	if err := c.Get(ctx, client.ObjectKey{Name: n}, fpvc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	bindings := &fluentpvcv1alpha1.FluentPVCBindingList{}
	if err := c.List(ctx, bindings); client.IgnoreNotFound(err) != nil {
		return err
	}
	for _, b := range bindings.Items {
		if !metav1.IsControlledBy(&b, fpvc) {
			continue
		}
		if err := deleteFluentPVCBinding(ctx, c, &b); err != nil {
			return err
		}
	}
	if controllerutil.ContainsFinalizer(fpvc, constants.FluentPVCFinalizerName) {
		controllerutil.RemoveFinalizer(fpvc, constants.FluentPVCFinalizerName)
		if err := c.Update(ctx, fpvc); err != nil {
			return err
		}
	}
	if err := c.Delete(ctx, fpvc); err != nil {
		return err
	}
	return nil
}

func deleteFluentPVCBinding(ctx context.Context, c client.Client, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	if err := deletePVC(ctx, c, b.Spec.PVC.Name, b.Namespace); err != nil {
		return err
	}
	if controllerutil.ContainsFinalizer(b, constants.FluentPVCBindingFinalizerName) {
		controllerutil.RemoveFinalizer(b, constants.FluentPVCBindingFinalizerName)
		if err := c.Update(ctx, b); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	if err := c.Delete(ctx, b, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

func deletePVC(ctx context.Context, c client.Client, name string, namespace string) error {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		} else {
			return err
		}
	}
	if controllerutil.ContainsFinalizer(pvc, constants.PVCFinalizerName) {
		controllerutil.RemoveFinalizer(pvc, constants.PVCFinalizerName)
		if err := c.Update(ctx, pvc); client.IgnoreNotFound(err) != nil {
			return err
		}
		// if pvc.Status.Phase == corev1.ClaimBound {
		if err := deletePV(ctx, c, pvc.Spec.VolumeName, pvc.Namespace); err != nil {
			return err
		}
		// }
	}
	if err := c.Delete(ctx, pvc, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

func deletePV(ctx context.Context, c client.Client, name string, namespace string) error {
	pv := &corev1.PersistentVolume{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pv); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		} else {
			return err
		}
	}
	if controllerutil.ContainsFinalizer(pv, "kubernetes.io/pv-protection") {
		controllerutil.RemoveFinalizer(pv, "kubernetes.io/pv-protection")
		if err := c.Update(ctx, pv); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	if err := c.Delete(ctx, pv, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}
