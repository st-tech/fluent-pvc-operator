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

// func generateTestBindingManifest() *fluentpvcv1alpha1.FluentPVCBinding {
// 	b := &fluentpvcv1alpha1.FluentPVCBinding{}
// 	b.SetName(testFluentPVCBindingName)
// 	b.SetNamespace(testNamespace)
// 	// b.setConditionTrue(FluentPVCBindingConditionFinalizerJobApplied, "reason", "message")
// 	b.Spec.FluentPVCName = testFluentPVCNameDefaultSucceeded
// 	b.Spec.PodName = testPodName
// 	b.Spec.PVCName = testFluentPVCBindingName
// 	return b
// }

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

		// b := &fluentpvcv1alpha1.FluentPVCBinding{}
		// b.SetNamespace(testNamespace)
		// b.SetName(testFluentPVCBindingName)
		// if err := k8sClient.Delete(ctx, b); err != nil {
		// 	if !apierrors.IsNotFound(err) {
		// 		Expect(err).NotTo(HaveOccurred())
		// 	}
		// }
		// pvc := &corev1.PersistentVolumeClaim{}
		// pvc.SetNamespace(testNamespace)
		// pvc.SetName(testFluentPVCBindingName)
		// if err := k8sClient.Delete(ctx, pvc); err != nil {
		// 	if !apierrors.IsNotFound(err) {
		// 		Expect(err).NotTo(HaveOccurred())
		// 	}
		// }
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
			// podPatched := pod.DeepCopy()
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}

			// b := generateTestBindingManifest()
			// {
			// 	ctrl.SetControllerReference(fpvc, b, k8sClient.Scheme())
			// 	err := k8sClient.Create(ctx, b)
			// 	Expect(err).Should(Succeed())
			// }

			// pvc := &corev1.PersistentVolumeClaim{}
			// {
			// 	pvc.SetName(testFluentPVCBindingName)
			// 	pvc.SetNamespace(testNamespace)
			// 	pvc.Spec = *fpvc.Spec.PVCSpecTemplate.DeepCopy()
			// 	// Skip AddFinalizer step for test
			// 	// controllerutil.AddFinalizer(pvc, constants.PVCFinalizerName)
			// 	ctrl.SetControllerReference(b, pvc, k8sClient.Scheme())
			// 	err := k8sClient.Create(ctx, pvc)
			// 	Expect(err).Should(Succeed())
			// }
			// {
			// 	podutils.InjectOrReplaceVolume(&podPatched.Spec, &corev1.Volume{
			// 		Name: fpvc.Spec.VolumeName,
			// 		VolumeSource: corev1.VolumeSource{
			// 			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			// 				ClaimName: testFluentPVCBindingName,
			// 			},
			// 		},
			// 	})
			// 	podutils.InjectOrReplaceContainer(&podPatched.Spec, fpvc.Spec.SidecarContainerTemplate.DeepCopy())
			// 	podutils.InjectOrReplaceVolumeMount(&podPatched.Spec, &corev1.VolumeMount{
			// 		Name:      fpvc.Spec.VolumeName,
			// 		MountPath: fpvc.Spec.CommonMountPath,
			// 	})
			// 	_, err := ctrl.CreateOrUpdate(ctx, k8sClient, podPatched, func() error { return nil })
			// 	// err := k8sClient.Update(ctx, podPatched)
			// 	Expect(err).Should(Succeed())
			// }
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
		It("should not do anything, that means the pvc continues to be exist when pvc owner != binding", func() {
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
	})
})
