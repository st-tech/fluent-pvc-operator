package e2e

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("fluentpvcbinding_controller", func() {
	BeforeEach(func() {
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameDefault) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameSidecarSleepLong) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameFinalizerJobFailed) }, 10).Should(Succeed())

		if err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameDefault, testSidecarContainerName, true, []string{"sh", "-c", "sleep 10"}, []string{"sh", "-c", "sleep 10"})); err != nil {
			Expect(err).NotTo(HaveOccurred())
		}
		if err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameSidecarSleepLong, testSidecarContainerName, true, []string{"sh", "-c", "sleep 100"}, []string{"sh", "-c", "sleep 10"})); err != nil {
			Expect(err).ShouldNot(HaveOccurred())
		}
		if err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameFinalizerJobFailed, testSidecarContainerName, true, []string{"sh", "-c", "sleep 10"}, []string{"sh", "-c", "sleep 10; exit 1"})); err != nil {
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

		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameDefault) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameSidecarSleepLong) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameFinalizerJobFailed) }, 10).Should(Succeed())
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
		It("should have the Unknown condition when the pvc becomes claimLost", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the pvc/pv and the binding are ready.")
			Eventually(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, pvc); err != nil {
					return err
				}
				if pvc.Status.Phase != corev1.ClaimBound {
					return errors.New("PVC is not bound.")
				}

				pv := &corev1.PersistentVolume{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: pvc.Spec.VolumeName}, pv); err != nil {
					return err
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
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
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
		It("should have the OutOfUse condition when the pod is deleted", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the pvc and the binding are ready.")
			Eventually(func() error {
				pod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
					return err
				}
				if pod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range pod.Status.ContainerStatuses {
					if !stat.Ready || stat.State.Running == nil {
						return errors.New("Pod containers are not ready.")
					}
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

			By("deleting the pod.")
			pod := &corev1.Pod{}
			pod.SetName(testPodName)
			pod.SetNamespace(testNamespace)
			if err := k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the OutOfUse condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionOutOfUse() {
					return errors.New("FluentPVCBinding does not have the OutOfUse condition.")
				}
				return nil
			}, 60).Should(Succeed())
		})
	})
	Context("the pod is succeeded and the pvc is bound", func() {
		BeforeEach(func() {
			Eventually(func() error {
				fpvc := &fluentpvcv1alpha1.FluentPVC{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameDefault}, fpvc); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())

			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameDefault,
				ContainerArgs:          []string{"sleep", "10"},
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})

			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the OutOfUse condition when the pod is succeeded", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the pvc and the binding are ready.")
			Eventually(func() error {
				pod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
					return err
				}
				if pod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range pod.Status.ContainerStatuses {
					if !stat.Ready || stat.State.Running == nil {
						return errors.New("Pod containers are not ready.")
					}
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

			By("checking that the binding has the OutOfUse condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionOutOfUse() {
					return errors.New("FluentPVCBinding does not have the OutOfUse condition.")
				}
				return nil
			}, 60).Should(Succeed())
		})
	})
	Context("the pod is failed and the pvc is bound", func() {
		BeforeEach(func() {
			Eventually(func() error {
				fpvc := &fluentpvcv1alpha1.FluentPVC{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameDefault}, fpvc); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())

			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameDefault,
				ContainerArgs:          []string{"sh", "-c", "sleep 10; exit 1"},
				RestartPolicy:          corev1.RestartPolicyNever,
			})

			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the OutOfUse condition when the pod is failed and is not restarted", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the pvc and the binding are ready.")
			Eventually(func() error {
				pod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, pod); err != nil {
					return err
				}
				if pod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range pod.Status.ContainerStatuses {
					if !stat.Ready || stat.State.Running == nil {
						return errors.New("Pod containers are not ready.")
					}
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

			By("checking that the binding has the OutOfUse condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionOutOfUse() {
					return errors.New("FluentPVCBinding does not have the OutOfUse condition.")
				}
				return nil
			}, 60).Should(Succeed())
		})
	})
	Context("the binding is not ready", func() {
		It("should continue to be not Ready when the pod is deleted", func() {
			By("creating the pod.")
			Eventually(func() error {
				fpvc := &fluentpvcv1alpha1.FluentPVC{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameDefault}, fpvc); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())
			{
				pod := generateTestPodManifest(testPodConfig{
					AddFluentPVCAnnotation: true,
					FluentPVCName:          testFluentPVCNameDefault,
					ContainerArgs:          []string{"sleep", "10"},
					RestartPolicy:          corev1.RestartPolicyOnFailure,
				})
				Eventually(func() error {
					if err := k8sClient.Create(ctx, pod); err != nil {
						return err
					}
					return nil
				}, 60).Should(Succeed())
			}

			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding is not ready.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if b.IsConditionReady() {
					return errors.New("FluentPVCBinding already have the Ready condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the pod.")
			pod := &corev1.Pod{}
			pod.SetName(testPodName)
			pod.SetNamespace(testNamespace)
			if err := k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0)); err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding continues to be not Ready.")
			Consistently(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if b.IsConditionReady() {
					return errors.New("FluentPVCBinding has the Ready condition.")
				}
				return nil
			}, 20).Should(Succeed())
		})
		It("should have the Ready condition when the pod is created", func() {
			By("creating the pod.")
			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameDefault,
				ContainerArgs:          []string{"sleep", "10"},
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})
			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())

			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding is Ready.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionReady() {
					return errors.New("FluentPVCBinding does not have the Ready condition.")
				}
				return nil
			}, 60).Should(Succeed())
		})
	})
	Context("the binding is OutOfUse", func() {
		BeforeEach(func() {
			Eventually(func() error {
				fpvc := &fluentpvcv1alpha1.FluentPVC{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameDefault}, fpvc); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())

			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameDefault,
				ContainerArgs:          []string{"sleep", "10"},
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})

			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the FinalizerJobApplied condition when the finalizer job is applied", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the OutOfUse condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionOutOfUse() {
					return errors.New("FluentPVCBinding does not have the OutOfUse condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("checking that the binding has the FinalizerJobApplied condition and the job is applied.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobApplied condition.")
				}

				jobs := &batchv1.JobList{}
				if err := k8sClient.List(ctx, jobs); err != nil {
					return err
				}
				if len(jobs.Items) != 1 {
					return errors.New("Job not found or multiple job found.")
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the Unknown condition when the pvc is deleted", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the OutOfUse condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionOutOfUse() {
					return errors.New("FluentPVCBinding does not have the OutOfUse condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the pvc.")
			if err := deletePVC(ctx, k8sClient, initBinding.Name, testNamespace); err != nil {
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
		It("should have the FinalizerJobSucceeded condition when the finalizer job is completed", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the FinalizerJobApplied condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobApplied condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("checking that the binding has the FinalizerJobSucceeded condition and the job is completed.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobSucceeded() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobSucceeded condition.")
				}

				jobs := &batchv1.JobList{}
				if err := k8sClient.List(ctx, jobs); err != nil {
					return err
				}
				if len(jobs.Items) != 1 {
					return errors.New("Job not found or multiple job found.")
				}
				for _, c := range jobs.Items[0].Status.Conditions {
					if c.Type != batchv1.JobComplete || c.Status == corev1.ConditionFalse {
						return errors.New("Job is not completed.")
					}
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the FinalizerJobSucceeded condition even if the binding is deleted after the finalizer job applied", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the FinalizerJobApplied condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobApplied condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the binding.")
			if err := k8sClient.Delete(ctx, initBinding); err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the FinalizerJobSucceeded condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobSucceeded() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobSucceeded condition.")
				}
				return nil
			}, 60).Should(Succeed())

		})
		It("should clean up the pvc and the binding itseld after FinalizerJobSucceeded condition", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the FinalizerJobSucceeded condition and the job is completed.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobSucceeded() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobSucceeded condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the pod.")
			pod := &corev1.Pod{}
			pod.SetName(testPodName)
			pod.SetNamespace(testNamespace)
			if err := k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0)); client.IgnoreNotFound(err) != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding and the pvc are removed.")
			Eventually(func() error {
				pvc := &corev1.PersistentVolumeClaim{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, pvc)
				if err == nil {
					return errors.New("PVC is still exist.")
				}
				if !apierrors.IsNotFound(err) {
					return err
				}

				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				err = k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b)
				if err == nil {
					return errors.New("Binding is still exist.")
				}
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the OutOfUse condition when the finalizer job is deleted", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the FinalizerJobApplied condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobApplied condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the job")
			Eventually(func() error {
				jobs := &batchv1.JobList{}
				if err := k8sClient.List(ctx, jobs); err != nil {
					return err
				}
				if len(jobs.Items) != 1 {
					return errors.New("Job not found or multiple job found.")
				}
				if jobs.Items[0].Status.Succeeded == 0 || len(jobs.Items[0].Status.Conditions) == 0 {
					return errors.New("Job is not completed.")
				}
				for _, c := range jobs.Items[0].Status.Conditions {
					if c.Type != batchv1.JobComplete || c.Status == corev1.ConditionFalse {
						return errors.New("Job is not completed.")
					}
				}
				if err := k8sClient.Delete(ctx, &jobs.Items[0]); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the finalizer pod")
			pods := &corev1.PodList{}
			if err := k8sClient.List(ctx, pods); err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}
			for _, p := range pods.Items {
				if p.Labels["job-name"] == initBinding.Name {
					if err := k8sClient.Delete(ctx, &p); err != nil {
						Expect(err).ShouldNot(HaveOccurred())
					}
				}
			}

			By("checking that the binding has the OutOfUse condition and the job is deleted.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionOutOfUse() {
					return errors.New("FluentPVCBinding does not have the OutOfUse condition.")
				}
				if b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding still has the FinalizerJobApplied condition.")
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the Unknown condition when the multiple finalizer job are found", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the FinalizerJobApplied condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobApplied condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("applying the unnecessary finalizer job")
			j := &batchv1.Job{}
			j.SetName("test-finalizer-job")
			j.SetNamespace(initBinding.Namespace)
			j.Spec = batchv1.JobSpec{
				BackoffLimit: pointer.Int32Ptr(0),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							{
								Name:    "test-finalizer-container",
								Command: []string{"sh", "-c", "sleep 10"},
								Image:   "alpine",
							},
						},
					},
				},
			}
			ctrl.SetControllerReference(initBinding, j, k8sClient.Scheme())
			if err := k8sClient.Create(ctx, j); err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the Unknown condition and the multiple finalizer job are found.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionUnknown() {
					return errors.New("FluentPVCBinding does not have the Unknown condition.")
				}

				jobs := &batchv1.JobList{}
				if err := k8sClient.List(ctx, jobs); err != nil {
					return err
				}
				if len(jobs.Items) <= 1 {
					return errors.New("Job not found or single job found.")
				}
				return nil
			}, 60).Should(Succeed())
		})
	})
	Context("the finalizer job is failed", func() {
		BeforeEach(func() {
			Eventually(func() error {
				fpvc := &fluentpvcv1alpha1.FluentPVC{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCNameFinalizerJobFailed}, fpvc); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())

			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameFinalizerJobFailed,
				ContainerArgs:          []string{"sleep", "10"},
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})

			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the FinalizerJobFailed condition", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the FinalizerJobApplied condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobApplied condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("checking that the binding has the FinalizerJobFailed condition and the job is failed.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobFailed() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobFailed condition.")
				}

				jobs := &batchv1.JobList{}
				if err := k8sClient.List(ctx, jobs); err != nil {
					return err
				}
				if len(jobs.Items) != 1 {
					return errors.New("Job not found or multiple job found.")
				}
				for _, c := range jobs.Items[0].Status.Conditions {
					if c.Type != batchv1.JobFailed || c.Status == corev1.ConditionFalse {
						return errors.New("Job is not failed.")
					}
				}
				return nil
			}, 60).Should(Succeed())
		})
		It("should have the OutOfUse condition when the finalizer job is deleted after the job failed", func() {
			initBinding, err := getFluentPVCBindingFromPod(ctx, k8sClient, testNamespace, testPodName)
			if err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("checking that the binding has the FinalizerJobFailed condition.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionFinalizerJobFailed() {
					return errors.New("FluentPVCBinding does not have the FinalizerJobFailed condition.")
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the job that has failed")
			Eventually(func() error {
				jobs := &batchv1.JobList{}
				if err := k8sClient.List(ctx, jobs); err != nil {
					return err
				}
				if len(jobs.Items) != 1 {
					return errors.New("Job not found or multiple job found.")
				}
				for _, c := range jobs.Items[0].Status.Conditions {
					if c.Type != batchv1.JobFailed || c.Status == corev1.ConditionFalse {
						return errors.New("Job is not failed.")
					}
				}
				if err := k8sClient.Delete(ctx, &jobs.Items[0]); err != nil {
					return err
				}
				return nil
			}, 60).Should(Succeed())

			By("deleting the finalizer pod")
			pods := &corev1.PodList{}
			if err := k8sClient.List(ctx, pods); err != nil {
				Expect(err).ShouldNot(HaveOccurred())
			}
			for _, p := range pods.Items {
				if p.Labels["job-name"] == initBinding.Name {
					if err := k8sClient.Delete(ctx, &p); err != nil {
						Expect(err).ShouldNot(HaveOccurred())
					}
				}
			}

			By("checking that the binding has the OutOfUse condition and the job is deleted.")
			Eventually(func() error {
				b := &fluentpvcv1alpha1.FluentPVCBinding{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: initBinding.Name}, b); err != nil {
					return err
				}
				if !b.IsConditionOutOfUse() {
					return errors.New("FluentPVCBinding does not have the OutOfUse condition.")
				}
				if b.IsConditionFinalizerJobApplied() {
					return errors.New("FluentPVCBinding still has the FinalizerJobApplied condition.")
				}
				return nil
			}, 60).Should(Succeed())
		})
	})
})
