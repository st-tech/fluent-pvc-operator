package e2e

import (
	"context"
	"fmt"
	"math"

	"github.com/imdario/mergo"
	ginkgoConfig "github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"golang.org/x/xerrors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
	hashutils "github.com/st-tech/fluent-pvc-operator/utils/hash"
)

const (
	testPodNamePrefix                = "test-pod-"
	testContainerNamePrefix          = "test-container-"
	testNamespaceNamePrefix          = "test-namespace-"
	testFluentPVCNamePrefix          = "test-fluent-pvc-"
	testSidecarContainerNamePrefix   = "test-sidecar-container-"
	testFinalizerContainerNamePrefix = "test-finalizer-container-"
	testStorageClassNamePrefix       = "test-storage-class-"
	testPVCNamePrefix                = "test-pvc-"
	testFluentPVCBindingNamePrefix   = "test-fluent-pvc-binding-"
)

var (
	testContainerSleepInf *corev1.Container = &corev1.Container{
		Name: testContainerNamePrefix + "sleep-inf",
		Args: []string{"sleep", "inf"},
		// NOTE: to reap zombie processes when receiving SIGTERM.
		//       ref. https://github.com/krallin/tini
		Image: "krallin/ubuntu-tini:trusty",
	}
	testSidecarContainerEcho *corev1.Container = &corev1.Container{
		Name:    testSidecarContainerNamePrefix + "echo",
		Command: []string{"echo", "sidecar"},
		Image:   "alpine",
	}
	testFinalizerContainerEcho *corev1.Container = &corev1.Container{
		Name:    testFinalizerContainerNamePrefix + "echo",
		Command: []string{"echo", "finalizer"},
		Image:   "alpine",
	}
	testDefaultPod *corev1.Pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{*testContainerSleepInf.DeepCopy()},
		},
	}
	testDefaultFluentPVC *fluentpvcv1alpha1.FluentPVC = &fluentpvcv1alpha1.FluentPVC{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fluentpvcv1alpha1.GroupVersion.String(),
			Kind:       "FluentPVC",
		},
		Spec: fluentpvcv1alpha1.FluentPVCSpec{
			PVCSpecTemplate: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: func(s string) *string { return &s }("standard"),
			},
			PVCVolumeName:            "test-volume",
			PVCVolumeMountPath:       "/mnt/test",
			CommonEnvs:               []corev1.EnvVar{},
			SidecarContainerTemplate: *testSidecarContainerEcho.DeepCopy(),
			DeletePodIfSidecarContainerTerminationDetected: true,
			PVCFinalizerJobSpecTemplate: batchv1.JobSpec{
				BackoffLimit: pointer.Int32Ptr(0),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							*testFinalizerContainerEcho.DeepCopy(),
						},
					},
				},
			},
		},
	}
)

func FillPodDefault(original *corev1.Pod) error {
	if err := mergo.Merge(original, *testDefaultPod.DeepCopy()); err != nil {
		return err
	}
	return nil
}

func FillFluentPVCDefault(org *fluentpvcv1alpha1.FluentPVC) error {
	if err := mergo.Merge(org, *testDefaultFluentPVC.DeepCopy()); err != nil {
		return err
	}
	return nil
}

func GinkgoNodeId() string {
	return fmt.Sprintf("%d/%d",
		ginkgoConfig.GinkgoConfig.ParallelNode,
		ginkgoConfig.GinkgoConfig.ParallelTotal,
	)
}

func RandomString() string {
	return hashutils.ComputeHash(GinkgoNodeId(), pointer.Int32Ptr(int32(rand.IntnRange(math.MinInt32, math.MaxInt32))))
}

type TestK8SClient struct {
	client.Client
}

func NewTestK8SClient(c client.Client) *TestK8SClient {
	return &TestK8SClient{c}
}

func (c *TestK8SClient) FindOrCreate(ctx context.Context, obj client.Object, opts ...client.CreateOption) {
	Eventually(func() error {
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err == nil {
			return nil
		}
		if err := c.Create(ctx, obj, opts...); err != nil {
			return err
		}
		return xerrors.New(fmt.Sprintf("Wait until namespace: %s, name: %s is found.", obj.GetNamespace(), obj.GetName()))
	}, 10).Should(Succeed())
}

func (c *TestK8SClient) DeleteFluentPVC(ctx context.Context, name string) {
	Eventually(func() error {
		return c.deleteFluentPVCByName(ctx, name)
	}, 30).Should(Succeed())
}

func (c *TestK8SClient) DeleteNamespace(ctx context.Context, name string) {
	Eventually(func() error {
		ns := &corev1.Namespace{}
		ns.SetName(name)
		if err := c.Delete(ctx, ns, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
			return err
		}
		return nil
	}, 30).Should(Succeed())
}

func (c *TestK8SClient) DeleteAllInNamespace(ctx context.Context, namespace string, obj client.Object) {
	Eventually(func() error {
		if err := c.DeleteAllOf(ctx, obj, client.InNamespace(namespace)); client.IgnoreNotFound(err) != nil {
			return err
		}
		return nil
	}, 30).Should(Succeed())
}

func (c *TestK8SClient) deleteFluentPVCByName(ctx context.Context, name string) error {
	fpvc := &fluentpvcv1alpha1.FluentPVC{}
	if err := c.Get(ctx, client.ObjectKey{Name: name}, fpvc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.deleteFluentPVC(ctx, fpvc)
}

func (c *TestK8SClient) deleteFluentPVC(ctx context.Context, fpvc *fluentpvcv1alpha1.FluentPVC) error {
	bindings := &fluentpvcv1alpha1.FluentPVCBindingList{}
	if err := c.List(ctx, bindings); client.IgnoreNotFound(err) != nil {
		return err
	}
	for _, b := range bindings.Items {
		if !metav1.IsControlledBy(&b, fpvc) {
			continue
		}
		if err := c.deleteFluentPVCBindingByName(ctx, b.Namespace, b.Name); err != nil {
			return err
		}
	}
	if err := c.removeFinalizer(ctx, fpvc, constants.FluentPVCFinalizerName); err != nil {
		return err
	}
	if err := c.Delete(ctx, fpvc, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

func (c *TestK8SClient) deleteFluentPVCBindingByName(ctx context.Context, namespace, name string) error {
	b := &fluentpvcv1alpha1.FluentPVCBinding{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, b); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.deleteFluentPVCBinding(ctx, b)
}

func (c *TestK8SClient) deleteFluentPVCBinding(ctx context.Context, b *fluentpvcv1alpha1.FluentPVCBinding) error {
	if err := c.deletePVCByName(ctx, b.Namespace, b.Spec.PVC.Name); err != nil {
		return err
	}
	if err := c.removeFinalizer(ctx, b, constants.FluentPVCBindingFinalizerName); err != nil {
		return err
	}
	if err := c.Delete(ctx, b, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

func (c *TestK8SClient) deletePVCByName(ctx context.Context, namespace, name string) error {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.deletePVC(ctx, pvc)
}

func (c *TestK8SClient) deletePVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) error {
	if err := c.deletePVByName(ctx, pvc.Namespace, pvc.Spec.VolumeName); err != nil {
		return err
	}
	if err := c.removeFinalizer(ctx, pvc, constants.PVCFinalizerName); err != nil {
		return err
	}
	if err := c.Delete(ctx, pvc, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

func (c *TestK8SClient) deletePVByName(ctx context.Context, namespace, name string) error {
	pv := &corev1.PersistentVolume{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pv); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.deletePV(ctx, pv)
}

func (c *TestK8SClient) deletePV(ctx context.Context, pv *corev1.PersistentVolume) error {
	if err := c.Delete(ctx, pv, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

func (c *TestK8SClient) removeFinalizer(ctx context.Context, obj client.Object, finalizer string) error {
	if !controllerutil.ContainsFinalizer(obj, finalizer) {
		return nil
	}
	controllerutil.RemoveFinalizer(obj, finalizer)
	if err := c.Update(ctx, obj); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}
