package e2e

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
	podutils "github.com/st-tech/fluent-pvc-operator/utils/pod"
)

const (
	testPodName                     = "test-pod"
	testContainerName               = "test-container"
	testNamespace                   = "default"
	testFluentPVCNameDefault        = "test-fluent-pvc"
	testFluentPVCNameDeletePodFalse = "test-fluent-pvc-delete-false"
	testFluentPVCNameSidecarFailed  = "test-fluent-pvc-sidecar-failed"
	testSidecarContainerName        = "test-sidecar-container"
	testStorageClassName            = "standard"
)

func generateFluentPVCForTest(
	testFluentPVCName string,
	testSidecarContainerName string,
	deletePodIfSidecarContainerTerminationDetected bool,
	sidecarContainerCommand []string,
	finalizerContainerCommand []string,
) *fluentpvcv1alpha1.FluentPVC {
	return &fluentpvcv1alpha1.FluentPVC{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fluentpvcv1alpha1.GroupVersion.String(),
			Kind:       "FluentPVC",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: testFluentPVCName,
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
			PVCVolumeName:      "test-volume",
			PVCVolumeMountPath: "/mnt/test",
			CommonEnvs:         []corev1.EnvVar{},
			SidecarContainerTemplate: corev1.Container{
				Name:    testSidecarContainerName,
				Command: sidecarContainerCommand,
				Image:   "alpine",
			},
			DeletePodIfSidecarContainerTerminationDetected: deletePodIfSidecarContainerTerminationDetected,
			PVCFinalizerJobSpecTemplate: batchv1.JobSpec{
				BackoffLimit: pointer.Int32Ptr(0),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							{
								Name:    "test-finalizer-container",
								Command: finalizerContainerCommand,
								Image:   "alpine",
							},
						},
					},
				},
			},
		},
	}
}

type testPodConfig struct {
	AddFluentPVCAnnotation bool
	FluentPVCName          string
	ContainerArgs          []string
	RestartPolicy          corev1.RestartPolicy
}

func generateTestPodManifest(testPodConfig testPodConfig) *corev1.Pod {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: testNamespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  testContainerName,
					Args:  []string{"sleep", "1000"},
					Image: "krallin/ubuntu-tini:trusty",
				},
			},
		},
	}
	if testPodConfig.AddFluentPVCAnnotation {
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: testPodConfig.FluentPVCName,
		})
	}
	if testPodConfig.ContainerArgs != nil {
		pod.Spec.Containers[0].Args = testPodConfig.ContainerArgs
	}
	if testPodConfig.RestartPolicy != "" {
		pod.Spec.RestartPolicy = testPodConfig.RestartPolicy
	}
	return pod
}

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
	pvc := &corev1.PersistentVolumeClaim{}
	pvcFound := true
	if err := c.Get(ctx, client.ObjectKey{Namespace: b.Namespace, Name: b.Spec.PVC.Name}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			pvcFound = false
		} else {
			return err
		}
	}
	if pvcFound && controllerutil.ContainsFinalizer(pvc, constants.PVCFinalizerName) {
		controllerutil.RemoveFinalizer(pvc, constants.PVCFinalizerName)
		if err := c.Update(ctx, pvc); client.IgnoreNotFound(err) != nil {
			return err
		}
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

var _ = Describe("pod_controller", func() {
	BeforeEach(func() {
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameDefault) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameDeletePodFalse) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameSidecarFailed) }, 10).Should(Succeed())
		{
			err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameDefault, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5"}, []string{"echo", "test"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameDeletePodFalse, testSidecarContainerName, false, []string{"sh", "-c", "sleep 5; exit 1"}, []string{"echo", "test"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameSidecarFailed, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5; exit 1"}, []string{"echo", "test"}))
			Expect(err).NotTo(HaveOccurred())
		}
	})
	AfterEach(func() {
		// Clean up the Pod if created.
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
		// Clean up the FluentPVC.
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameDefault) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameDeletePodFalse) }, 10).Should(Succeed())
		Eventually(func() error { return deleteFluentPVC(ctx, k8sClient, testFluentPVCNameSidecarFailed) }, 10).Should(Succeed())
	})
	Context("An applied pod is not a target", func() {
		BeforeEach(func() {
			ctx := context.Background()
			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: false,
			})
			podutils.InjectOrReplaceContainer(&pod.Spec, &corev1.Container{
				Name:    testSidecarContainerName,
				Command: []string{"sh", "-c", "sleep 5; exit 1"},
				Image:   "alpine",
			})
			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 30).Should(Succeed())
		})
		It("should not do anything, that means the pod continues to be running", func() {
			ctx := context.Background()
			mutPod := &corev1.Pod{}
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
			Expect(err).Should(Succeed())

			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if err != nil {
					return err
				}
				if mutPod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range mutPod.Status.ContainerStatuses {
					if !stat.Ready || stat.State.Running == nil {
						return errors.New("Pod ContainerStatuses are not ready.")
					}
				}
				return nil
			}, 30).Should(Succeed())

			Consistently(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if err != nil {
					return err
				}
				if mutPod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range mutPod.Status.ContainerStatuses {
					if stat.Name != testSidecarContainerName && (!stat.Ready || stat.State.Running == nil) {
						return errors.New("Main container is not ready.")
					}
				}
				return nil
			}, 10).Should(Succeed())
		})
	})
	Context("An applied pod is a target & the sidecar container exits with code 0 and is not restarted", func() {
		BeforeEach(func() {
			ctx := context.Background()
			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameDefault,
			})
			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 30).Should(Succeed())
		})
		It("should not do anything, that means the pod continues to be running", func() {
			mutPod := &corev1.Pod{}
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
			Expect(err).Should(Succeed())

			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if err != nil {
					return err
				}
				if mutPod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range mutPod.Status.ContainerStatuses {
					if stat.Name == testSidecarContainerName {
						if stat.State.Terminated == nil {
							return errors.New("Sidecar container is still running.")
						}
						if stat.State.Terminated != nil && stat.State.Terminated.ExitCode != 0 {
							return errors.New("Sidecar container terminated with exit code != 0.")
						}
					}
					if stat.Name != testSidecarContainerName && (!stat.Ready || stat.State.Running == nil) {
						return errors.New("Main container is not ready.")
					}
				}
				return nil
			}, 30).Should(Succeed())

			Consistently(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if err != nil {
					return err
				}
				if mutPod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range mutPod.Status.ContainerStatuses {
					if stat.Name != testSidecarContainerName && (!stat.Ready || stat.State.Running == nil) {
						return errors.New("Main container is not ready.")
					}
				}
				return nil
			}, 10).Should(Succeed())
		})
	})
	Context("An applied pod is a target & the sidecar container exits with code != 0", func() {
		It("should not do anything if FluentPVC.DeletePodIfSidecarContainerTerminationDetected = false, that means the pod continues to be running", func() {
			ctx := context.Background()
			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameDeletePodFalse,
			})
			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 30).Should(Succeed())

			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if err != nil {
					return err
				}
				if mutPod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range mutPod.Status.ContainerStatuses {
					if stat.Name == testSidecarContainerName {
						if stat.State.Terminated == nil {
							return errors.New("Sidecar container is still running.")
						}
						if stat.State.Terminated != nil && stat.State.Terminated.ExitCode == 0 {
							return errors.New("Sidecar container terminated with unexpected exit code.")
						}
					}
					if stat.Name != testSidecarContainerName && (!stat.Ready || stat.State.Running == nil) {
						return errors.New("Main container is not ready.")
					}
				}
				return nil
			}, 30).Should(Succeed())

			Consistently(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if err != nil {
					return err
				}
				if mutPod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range mutPod.Status.ContainerStatuses {
					if stat.Name != testSidecarContainerName && (!stat.Ready || stat.State.Running == nil) {
						return errors.New("Main container is not ready.")
					}
				}
				return nil
			}, 10).Should(Succeed())
		})
		It("should delete the pod which restartPolicy is 'OnFailure'", func() {
			ctx := context.Background()
			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameSidecarFailed,
				RestartPolicy:          corev1.RestartPolicyOnFailure,
			})
			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 30).Should(Succeed())
			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if err != nil {
					return err
				}
				if mutPod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range mutPod.Status.ContainerStatuses {
					if stat.Name == testSidecarContainerName {
						if stat.State.Terminated == nil {
							return errors.New("Sidecar container is still running.")
						}
						if stat.State.Terminated != nil && stat.State.Terminated.ExitCode == 0 {
							return errors.New("Sidecar container terminated with unexpected exit code.")
						}
					}
					if stat.Name != testSidecarContainerName && (!stat.Ready || stat.State.Running == nil) {
						return errors.New("Main container is not ready.")
					}
				}
				return nil
			}, 30).Should(Succeed())
			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if apierrors.IsNotFound(err) {
					return nil
				} else {
					return errors.New("Pod is still exist.")
				}
			}, 30).Should(Succeed())
		})
		It("should delete the pod which restartPolicy is 'Never'", func() {
			ctx := context.Background()
			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          testFluentPVCNameSidecarFailed,
			})
			pod.Spec.RestartPolicy = corev1.RestartPolicyNever
			Eventually(func() error {
				if err := k8sClient.Create(ctx, pod); err != nil {
					return err
				}
				return nil
			}, 30).Should(Succeed())
			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if err != nil {
					return err
				}
				if mutPod.Status.Phase != corev1.PodRunning {
					return errors.New("Pod is not running.")
				}
				for _, stat := range mutPod.Status.ContainerStatuses {
					if stat.Name == testSidecarContainerName {
						if stat.State.Terminated == nil {
							return errors.New("Sidecar container is still running.")
						}
						if stat.State.Terminated != nil && stat.State.Terminated.ExitCode == 0 {
							return errors.New("Sidecar container terminated with unexpected exit code.")
						}
					}
					if stat.Name != testSidecarContainerName && (!stat.Ready || stat.State.Running == nil) {
						return errors.New("Main container is not ready.")
					}
				}
				return nil
			}, 30).Should(Succeed())
			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testPodName}, mutPod)
				if apierrors.IsNotFound(err) {
					return nil
				} else {
					return errors.New("Pod is still exist.")
				}
			}, 30).Should(Succeed())
		})
	})
	Context("An applied pod is a target & the FluentPVC is deleted after the pod is applied", func() {
		// TODO: Fix test after merging https://github.com/st-tech/fluent-pvc-operator/pull/17
		It("should process correctly", func() {
			ctx := context.Background()
			pod := generateTestPodManifest(testPodConfig{
				AddFluentPVCAnnotation: true,
				FluentPVCName:          "dummy-fluent-pvc",
			})
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).To(HaveOccurred())
			}
		})
	})
})
