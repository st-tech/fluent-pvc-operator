package e2e

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ()

func getBindingFromTestPod() (*fluentpvcv1alpha1.FluentPVCBinding, error) {
	pod := &corev1.Pod{}
	{
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
			return nil, err
		}
	}
	var b *fluentpvcv1alpha1.FluentPVCBinding
	bList := &fluentpvcv1alpha1.FluentPVCBindingList{}
	if err := k8sClient.List(ctx, bList); err != nil {
		return nil, err
	}
	for _, b_ := range bList.Items {
		if b_.Spec.Pod.Name == pod.Name {
			b = b_.DeepCopy()
		}
	}
	if b == nil {
		return nil, errors.New("FluentPVCBinding not found.")
	}
	return b, nil
}

var _ = Describe("pvc_controller", func() {
	BeforeEach(func() {
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameSidecarSleepLong) }, 10).Should(Succeed())

		if err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameSidecarSleepLong, testSidecarContainerName, true, []string{"sh", "-c", "sleep 100"}, []string{"sh", "-c", "sleep 10"})); err != nil {
			Expect(err).ShouldNot(HaveOccurred())
		}
	})
	AfterEach(func() {
		pod := &corev1.Pod{}
		pod.SetNamespace(testNamespace)
		pod.SetName(testPodName)
		if err := k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
			Expect(err).ShouldNot(HaveOccurred())
		}

		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameSidecarSleepLong) }, 10).Should(Succeed())
	})
	Context("the pod is running and the pvc is bound", func() {
		BeforeEach(func() {
			Eventually(func() error {
				fpvc := &fluentpvcv1alpha1.FluentPVC{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameSidecarSleepLong}, fpvc); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())

			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameSidecarSleepLong,
				ContainerArgs:          []string{"sleep", "100"},
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})

			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the Unknown condition when the pvc become claimLost", func() {
			initBinding, err := getBindingFromTestPod()
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the pvc and the binding are ready.")
			Eventually(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, pvc); err != nil {
					return err
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}

				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionReady() {
					return errors.New("FluentPVCBinding does not have the Ready condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the persistent volume.")
			pvc := &corev1.PersistentVolumeClaim{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, pvc); err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}
			if err := deletePV(ctx, k8sClient, pvc.Spec.VolumeName, testNamespace); err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the Unknown condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionUnknown() {
					return errors.New("FluentPVCBinding does not have the Unknown condition.")
				}
				return nil
			}, 60).Should(Succeed())
		})

		It("should have the Unknown condition when the pvc and the pod are deleted", func() {
			initBinding, err := getBindingFromTestPod()
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the pvc and the binding are ready.")
			Eventually(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, pvc); err != nil {
					return err
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}

				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionReady() {
					return errors.New("FluentPVCBinding does not have the Ready condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the pvc.")
			if err := deletePVC(ctx, k8sClient, initBinding.Name, testNamespace); err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("deleting the pod.")
			pod := &corev1.Pod{}
			pod.SetName(testPodName)
			pod.SetNamespace(testNamespace)
			if err := k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the Unknown condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionUnknown() {
					return errors.New("FluentPVCBinding does not have the Unknown condition.")
				}
				return nil
			}, 60).Should(Succeed())
		})
	})
})
