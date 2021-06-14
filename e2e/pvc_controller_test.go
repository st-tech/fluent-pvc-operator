package e2e

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	testFluentPVCNameSidecarSleepLong   = "test-fluent-pvc-sidecar-sleep-long"
	testFluentPVCNameFinalizerJobFailed = "test-fluent-pvc-finalizer-job-failed"
	testPVCName                         = "test-pvc"
	testFluentPVCBindingName            = "test-fluent-pvc-binding"
)

var _ = Describe("pvc_controller", func() {
	BeforeEach(func() {
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameSidecarSleepLong) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameFinalizerJobFailed) }, 10).Should(Succeed())

		if err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameSidecarSleepLong, testSidecarContainerName, true, []string{"sh", "-c", "sleep 100"}, []string{"sh", "-c", "sleep 10"})); err != nil {
			Expect(err).NotTo(HaveOccurred())
		}
		if err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameFinalizerJobFailed, testSidecarContainerName, true, []string{"sh", "-c", "sleep 100"}, []string{"sh", "-c", "sleep 10; exit 1"})); err != nil {
			Expect(err).NotTo(HaveOccurred())
		}
	})
	AfterEach(func() {
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

		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameSidecarSleepLong) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameFinalizerJobFailed) }, 10).Should(Succeed())
	})
	Context("pod main container is succeeded or running", func() {
		BeforeEach(func() {
			ctx := context.Background()
			fpvc := &fluentpvcv1alpha1.FluentPVC{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameSidecarSleepLong}, fpvc); err != nil {
				Expect(err).Should(Succeed())
			}

			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameSidecarSleepLong,
				ContainerArgs:          []string{"sleep", "100"},
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}
		})
		It("should finalize and delete pvc", func() {
			ctx := context.Background()
			pod := &corev1.Pod{}
			{
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod)
				Expect(err).Should(Succeed())
			}

			var bList *fluentpvcv1alpha1.FluentPVCBindingList
			var b *fluentpvcv1alpha1.FluentPVCBinding
			if err := k8sClient.List(ctx, bList); err != nil {
				Expect(err).Should(Succeed())
			}
			for _, b_ := range bList.Items {
				if b.Spec.Pod.Name == pod.Name {
					b = b_.DeepCopy()
				}
			}
			Expect(b).ShouldNot(BeNil())
			bindingAndPVCName := b.Name

			Eventually(func() error {
				{
					pod = &corev1.Pod{}
					if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
						return err
					}
					if pod.Status.Phase != corev1.PodRunning {
						return errors.New("Pod is not running.")
					}
					for _, stat := range pod.Status.ContainerStatuses {
						if !stat.Ready || stat.State.Running == nil {
							return errors.New("Pod ContainerStatuses are not ready.")
						}
					}
				}
				{
					pvc := &corev1.PersistentVolumeClaim{}
					if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
						return err
					}
					if pvc.Status.Phase != corev1.ClaimBound {
						return errors.New("PVC is not bound.")
					}
				}
				return nil
			}, 30).Should(Succeed())

			if err := k8sClient.Delete(ctx, pod, &client.DeleteOptions{
				GracePeriodSeconds: pointer.Int64Ptr(0),
			}); err != nil {
				if !apierrors.IsNotFound(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			}

			Eventually(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc)
				if err != nil {
					if !apierrors.IsNotFound(err) {
						return err
					}
				}
				if pvc.Status.Phase == corev1.ClaimBound {
					return errors.New("PVC is still bound.")
				}
				return nil
			}, 30).Should(Succeed())
		})
		It("should have same name with binding and job, should wait for finalizer job completion before finalize pvc", func() {
			ctx := context.Background()
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
				Expect(err).Should(Succeed())
			}

			var bList *fluentpvcv1alpha1.FluentPVCBindingList
			var b *fluentpvcv1alpha1.FluentPVCBinding
			if err := k8sClient.List(ctx, bList); err != nil {
				Expect(err).Should(Succeed())
			}
			for _, b_ := range bList.Items {
				if b.Spec.Pod.Name == pod.Name {
					b = b_.DeepCopy()
				}
			}
			Expect(b).ShouldNot(BeNil())
			bindingAndPVCName := b.Name

			Eventually(func() error {
				pod = &corev1.Pod{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
					return err
				}

				if pod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range pod.Status.ContainerStatuses {
					if !stat.Ready || stat.State.Running == nil {
						return errors.New("Pod ContainerStatuses are not ready.")
					}
				}

				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
					return err
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}
				return nil
			}, 30).Should(Succeed())

			if err := k8sClient.Delete(ctx, pod, &client.DeleteOptions{
				GracePeriodSeconds: pointer.Int64Ptr(0),
			}); err != nil {
				if !apierrors.IsNotFound(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			}

			Eventually(func() error {
				pod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod)
				if err == nil || !apierrors.IsNotFound(err) {
					return errors.New("Pod is still exist.")
				}
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, b); err != nil {
					return err
				}
				if !b.IsConditionOutOfUse() {
					return errors.New("FluentPVCBinding is not OutOfUse condition.")
				}
				if b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding is already FinalizerJobApplied condition.")
				}
				return nil
			}, 30).Should(Succeed())

			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding is not FinalizerJobApplied condition.")
				}
				if b.Name != b.Spec.PVC.Name {
					return errors.New("FluentPVCBinding has different name with PVC.")
				}

				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: b.Spec.PVC.Name}, pvc); err != nil {
					return err
				}
				if !controllerutil.ContainsFinalizer(pvc, constants.PVCFinalizerName) {
					return errors.New("PVC does not have finalizer.")
				}

				jobs := &batchv1.JobList{}
				if err := k8sClient.List(ctx, jobs); err != nil {
					return err
				}
				if len(jobs.Items) != 1 {
					return errors.New("Job not found or Multiple Job found.")
				}
				if b.Name != jobs.Items[0].Name {
					return errors.New("FluentPVCBinding has different name with Finalizer Job.")
				}
				return nil
			}, 30).Should(Succeed())

			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobSucceeded() {
					return errors.New("FluentPVCBinding is not FinalizerJobSucceeded condition.")
				}

				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
					return err
				}
				return nil
			}, 30).Should(Succeed())

			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				{
					err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, b)
					if err == nil || !apierrors.IsNotFound(err) {
						return errors.New("FluentPVCBinding is still exist.")
					}
				}

				pvc := &corev1.PersistentVolumeClaim{}
				{
					err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc)
					if err == nil || !apierrors.IsNotFound(err) {
						return errors.New("PVC is still exist.")
					}
				}
				return nil
			}, 30).Should(Succeed())
		})
		It("should not do anything, that means pvc is continue to be exist", func() {
			ctx := context.Background()
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
				Expect(err).Should(Succeed())
			}

			var bList *fluentpvcv1alpha1.FluentPVCBindingList
			var b *fluentpvcv1alpha1.FluentPVCBinding
			if err := k8sClient.List(ctx, bList); err != nil {
				Expect(err).Should(Succeed())
			}
			for _, b_ := range bList.Items {
				if b.Spec.Pod.Name == pod.Name {
					b = b_.DeepCopy()
				}
			}
			Expect(b).ShouldNot(BeNil())
			bindingAndPVCName := b.Name

			Eventually(func() error {
				pod = &corev1.Pod{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
					return err
				}
				if pod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range pod.Status.ContainerStatuses {
					if !stat.Ready || stat.State.Running == nil {
						return errors.New("Pod ContainerStatuses are not ready.")
					}
				}

				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
					return err
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}
				return nil
			}, 30).Should(Succeed())

			Consistently(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
					return err
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}
				return nil
			}, 20).Should(Succeed())
		})
	})
	Context("pvc has claimLost phase and binding has Unknown condition", func() {
		BeforeEach(func() {
			ctx := context.Background()
			fpvc := &fluentpvcv1alpha1.FluentPVC{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameSidecarSleepLong}, fpvc); err != nil {
				Expect(err).Should(Succeed())
			}

			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameSidecarSleepLong,
				ContainerArgs:          []string{"sleep", "100"},
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}
		})
		It("should not do anything, that means pvc is continue to be exist", func() {
			ctx := context.Background()
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
				Expect(err).Should(Succeed())
			}

			var bList *fluentpvcv1alpha1.FluentPVCBindingList
			var b *fluentpvcv1alpha1.FluentPVCBinding
			if err := k8sClient.List(ctx, bList); err != nil {
				Expect(err).Should(Succeed())
			}
			for _, b_ := range bList.Items {
				if b.Spec.Pod.Name == pod.Name {
					b = b_.DeepCopy()
				}
			}
			Expect(b).ShouldNot(BeNil())
			bindingAndPVCName := b.Name

			Eventually(func() error {
				{
					pod := &corev1.Pod{}
					if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
						return err
					}
					if pod.Status.Phase != corev1.PodRunning {
						return errors.New("Pod is not running.")
					}
					for _, stat := range pod.Status.ContainerStatuses {
						if !stat.Ready || stat.State.Running == nil {
							return errors.New("Pod ContainerStatuses are not ready.")
						}
					}
				}
				{
					pvc := &corev1.PersistentVolumeClaim{}
					if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
						return err
					}
					if pvc.Status.Phase != corev1.ClaimBound {
						return errors.New("PVC is not bound.")
					}
					pvc.Status.Phase = corev1.ClaimLost
					if err := k8sClient.Update(ctx, pvc); err != nil {
						return err
					}
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
				pod := &corev1.Pod{}
				pod.SetName(testPodName)
				pod.SetNamespace(testNamespace)
				if err := k8sClient.Delete(ctx, pod, &client.DeleteOptions{
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
					return err
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
						return err
					}
				}
				return nil
			}, 20).Should(Succeed())
		})
	})
	Context("pod has succeeded and finalizer job has failed", func() {
		BeforeEach(func() {
			ctx := context.Background()
			fpvc := &fluentpvcv1alpha1.FluentPVC{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameFinalizerJobFailed}, fpvc); err != nil {
				Expect(err).Should(Succeed())
			}

			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameFinalizerJobFailed,
				ContainerArgs:          []string{"sleep", "100"},
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}
		})
		It("should not finalize pvc", func() {
			ctx := context.Background()
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
				Expect(err).Should(Succeed())
			}

			var bList *fluentpvcv1alpha1.FluentPVCBindingList
			var b *fluentpvcv1alpha1.FluentPVCBinding
			if err := k8sClient.List(ctx, bList); err != nil {
				Expect(err).Should(Succeed())
			}
			for _, b_ := range bList.Items {
				if b.Spec.Pod.Name == pod.Name {
					b = b_.DeepCopy()
				}
			}
			Expect(b).ShouldNot(BeNil())
			bindingAndPVCName := b.Name

			Eventually(func() error {
				pod = &corev1.Pod{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
					return err
				}
				if pod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range pod.Status.ContainerStatuses {
					if !stat.Ready || stat.State.Running == nil {
						return errors.New("Pod ContainerStatuses are not ready.")
					}
				}

				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
					return err
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}
				return nil
			}, 30).Should(Succeed())

			if err := k8sClient.Delete(ctx, pod, &client.DeleteOptions{
				GracePeriodSeconds: pointer.Int64Ptr(0),
			}); err != nil {
				if !apierrors.IsNotFound(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			}

			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding is not FinalizerJobApplied condition.")
				}

				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: b.Spec.PVC.Name}, pvc); err != nil {
					return err
				}
				return nil
			}, 30).Should(Succeed())

			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobFailed() {
					return errors.New("FluentPVCBinding is not FinalizerJobFailed condition.")
				}

				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: b.Spec.PVC.Name}, pvc); err != nil {
					return err
				}

				jobs := &batchv1.JobList{}
				if err := k8sClient.List(ctx, jobs); err != nil {
					return err
				}
				if len(jobs.Items) != 1 {
					return errors.New("Job not found or Multiple Job found.")
				}
				for _, c := range jobs.Items[0].Status.Conditions {
					if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionFalse {
						return errors.New("Job have not failed.")
					}
				}
				return nil
			}, 30).Should(Succeed())

			Consistently(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: bindingAndPVCName}, pvc); err != nil {
					return err
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}
				return nil
			}, 20).Should(Succeed())
		})
	})
})
