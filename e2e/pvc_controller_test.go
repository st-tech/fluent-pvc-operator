package e2e

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	testFluentPVCNameDefaultSucceeded = "test-fluent-pvc-succeeded"
	testPVCName                       = "test-pvc"
	testFluentPVCBindingName          = "test-fluent-pvc-binding"
)

var _ = Describe("pvc_controller", func() {
	BeforeEach(func() {
		{
			err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameDefaultSucceeded, testSidecarContainerName, true, []string{"sh", "-c", "sleep 100"}))
			Expect(err).NotTo(HaveOccurred())
		}
	})
	AfterEach(func() {
		// Clean up resources if created.
		pod := &corev1.Pod{}
		pod.SetNamespace(testNamespace)
		pod.SetName(testPodName)
		if err := k8sClient.Delete(ctx, pod, &client.DeleteOptions{
			GracePeriodSeconds: pointer.Int64Ptr(0),
		}); err != nil {
			if !apierrors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		}

		pvcList := &corev1.PersistentVolumeClaimList{}
		{
			err := k8sClient.List(ctx, pvcList)
			Expect(err).Should(Succeed())
		}
		for _, pvc := range pvcList.Items {
			controllerutil.RemoveFinalizer(&pvc, constants.PVCFinalizerName)
			k8sClient.Update(ctx, &pvc)
		}

		// Clean up the FluentPVC.
		{
			err := k8sClient.Delete(ctx, generateFluentPVCForTest(testFluentPVCNameDefaultSucceeded, testSidecarContainerName, true, []string{"sh", "-c", "sleep 100"}))
			Expect(err).NotTo(HaveOccurred())
		}
	})
	Context("", func() {
		BeforeEach(func() {
			ctx := context.Background()
			fpvc := &fluentpvcv1alpha1.FluentPVC{}
			{
				err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameDefaultSucceeded}, fpvc)
				Expect(err).Should(Succeed())
			}

			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameDefaultSucceeded,
				ContainerArgs:          []string{"sleep", "100"},
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}
		})
		It("pod ready -> pod deletion (not found) -> pvc finalized", func() {
			ctx := context.Background()
			mutPod := &corev1.Pod{}
			{
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				Expect(err).Should(Succeed())
			}
			b := &fluentpvcv1alpha1.FluentPVCBindingList{}
			{
				err := k8sClient.List(ctx, b)
				Expect(err).Should(Succeed())
			}

			Eventually(func() error {
				{
					mutPod = &corev1.Pod{}
					err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
					Expect(err).Should(Succeed())
					if mutPod.Status.Phase != corev1.PodRunning {
						return errors.New("Pod is not running.")
					}
					for _, stat := range mutPod.Status.ContainerStatuses {
						if !stat.Ready || stat.State.Running == nil {
							return errors.New("Pod ContainerStatuses are not ready.")
						}
					}
				}
				{
					pvc := &corev1.PersistentVolumeClaim{}
					err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: b.Items[0].Name}, pvc)
					Expect(err).Should(Succeed())
					if pvc.Status.Phase != corev1.ClaimBound {
						return errors.New("PVC is not bound.")
					}
				}
				return nil
			}, 30).Should(Succeed())

			if err := k8sClient.Delete(ctx, mutPod, &client.DeleteOptions{
				GracePeriodSeconds: pointer.Int64Ptr(0),
			}); err != nil {
				if !apierrors.IsNotFound(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			}

			Eventually(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: b.Items[0].Name}, pvc)
				if err != nil {
					if !apierrors.IsNotFound(err) {
						Expect(err).NotTo(HaveOccurred())
					}
				}
				if pvc.Status.Phase == corev1.ClaimBound {
					return errors.New("PVC is still bound.")
				}
				return nil
			}, 30).Should(Succeed())
		})
		It("should not do anything, that means pvc is continue to be exist when binding is not OutOfUse condition", func() {
			ctx := context.Background()
			mutPod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod); err != nil {
				Expect(err).Should(Succeed())
			}

			b := &fluentpvcv1alpha1.FluentPVCBindingList{}
			if err := k8sClient.List(ctx, b); err != nil {
				Expect(err).Should(Succeed())
			}

			Eventually(func() error {
				mutPod = &corev1.Pod{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod); err != nil {
					Expect(err).Should(Succeed())
				}
				if mutPod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range mutPod.Status.ContainerStatuses {
					if !stat.Ready || stat.State.Running == nil {
						return errors.New("Pod ContainerStatuses are not ready.")
					}
				}

				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: b.Items[0].Name}, pvc); err != nil {
					Expect(err).Should(Succeed())
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}
				return nil
			}, 30).Should(Succeed())

			Consistently(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: b.Items[0].Name}, pvc); err != nil {
					Expect(err).Should(Succeed())
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}
				return nil
			}, 20).Should(Succeed())
		})
		It("should not do anything, that means pvc is continue to be exist when binding is Unknown condition", func() {
			ctx := context.Background()
			bList := &fluentpvcv1alpha1.FluentPVCBindingList{}
			{
				err := k8sClient.List(ctx, bList)
				Expect(err).Should(Succeed())
			}
			bindingAndPVCName := bList.Items[0].Name

			Eventually(func() error {
				{
					mutPod := &corev1.Pod{}
					err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
					Expect(err).Should(Succeed())
					if mutPod.Status.Phase != corev1.PodRunning {
						return errors.New("Pod is not running.")
					}
					for _, stat := range mutPod.Status.ContainerStatuses {
						if !stat.Ready || stat.State.Running == nil {
							return errors.New("Pod ContainerStatuses are not ready.")
						}
					}
				}
				{
					pvc := &corev1.PersistentVolumeClaim{}
					err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc)
					Expect(err).Should(Succeed())
					if pvc.Status.Phase != corev1.ClaimBound {
						return errors.New("PVC is not bound.")
					}
					pvc.Status.Phase = corev1.ClaimLost
					err = k8sClient.Update(ctx, pvc)
					Expect(err).Should(Succeed())
				}
				return nil
			}, 30).Should(Succeed())

			{
				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
					Expect(err).Should(Succeed())
				}
				pv := &corev1.PersistentVolume{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: pvc.Spec.VolumeName}, pv); err != nil {
					Expect(err).Should(Succeed())
				}
				controllerutil.RemoveFinalizer(pv, "kubernetes.io/pv-protection")
				if err := k8sClient.Update(ctx, pv); err != nil {
					Expect(err).Should(Succeed())
				}
				if err := k8sClient.Delete(ctx, pv); err != nil {
					Expect(err).Should(Succeed())
				}
			}
			{
				mutPod := &corev1.Pod{}
				mutPod.SetName(testPodName)
				mutPod.SetNamespace(testNamespace)
				if err := k8sClient.Delete(ctx, mutPod, &client.DeleteOptions{
					GracePeriodSeconds: pointer.Int64Ptr(0),
				}); err != nil {
					if !apierrors.IsNotFound(err) {
						Expect(err).NotTo(HaveOccurred())
					}
				}
			}

			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, b); err != nil {
					Expect(err).Should(Succeed())
				}
				if !b.IsConditionUnknown() {
					return errors.New("FluentPVCBinding is not Unknown condition.")
				}
				return nil
			}, 30).Should(Succeed())

			Consistently(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
					if !apierrors.IsNotFound(err) {
						Expect(err).NotTo(HaveOccurred())
					}
				}
				return nil
			}, 20).Should(Succeed())
		})
	})
})
