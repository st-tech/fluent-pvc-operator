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

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
)

func generateFluentPVCForTest(
	testFluentPVCName string,
	testSidecarContainerName string,
	deletePodIfSidecarContainerTerminationDetected bool,
	sidecarContainerCommand []string,
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
			VolumeName:      "test-volume",
			CommonMountPath: "/mnt/test",
			CommonEnv:       []corev1.EnvVar{},
			SidecarContainerTemplate: corev1.Container{
				Name:    testSidecarContainerName,
				Command: sidecarContainerCommand,
				Image:   "alpine",
			},
			DeletePodIfSidecarContainerTerminationDetected: deletePodIfSidecarContainerTerminationDetected,
			PVCFinalizerJobSpecTemplate: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyOnFailure,
						Containers: []corev1.Container{
							{
								Name:    "test-finalizer-container",
								Command: []string{"echo", "test"},
								Image:   "alpine",
							},
						},
					},
				},
			},
		},
	}
}

func generateTestPodManifest(fluentPVCAnnotation string) *corev1.Pod {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Args:  []string{"sleep", "1000"},
					Image: "krallin/ubuntu-tini:trusty",
				},
			},
		},
	}
	if fluentPVCAnnotation != "" {
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: fluentPVCAnnotation,
		})
	}
	return pod
}

var _ = Describe("pod_controller", func() {
	const (
		testFluentPVCNameDefault        = "test-fluent-pvc"
		testFluentPVCNameDeletePodFalse = "test-fluent-pvc-delete-false"
		testFluentPVCNameSidecarFailed  = "test-fluent-pvc-sidecar-failed"
		testSidecarContainerName        = "test-sidecar-container"
		testStorageClassName            = "standard"
	)
	BeforeEach(func() {
		{
			err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameDefault, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameDeletePodFalse, testSidecarContainerName, false, []string{"sh", "-c", "sleep 5; exit 1"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Create(ctx, generateFluentPVCForTest(testFluentPVCNameSidecarFailed, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5; exit 1"}))
			Expect(err).NotTo(HaveOccurred())
		}
	})
	AfterEach(func() {
		// Clean up the Pod if created.
		pod := &corev1.Pod{}
		pod.SetNamespace("default")
		pod.SetName("test-pod")
		if err := k8sClient.Delete(ctx, pod, &client.DeleteOptions{
			GracePeriodSeconds: pointer.Int64Ptr(0),
		}); err != nil {
			if !apierrors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		}
		// Clean up the FluentPVC.
		{
			err := k8sClient.Delete(ctx, generateFluentPVCForTest(testFluentPVCNameDefault, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Delete(ctx, generateFluentPVCForTest(testFluentPVCNameDeletePodFalse, testSidecarContainerName, false, []string{"sh", "-c", "sleep 5; exit 1"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Delete(ctx, generateFluentPVCForTest(testFluentPVCNameSidecarFailed, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5; exit 1"}))
			Expect(err).NotTo(HaveOccurred())
		}
	})
	Context("An applied pod is not a target", func() {
		It("should not do anything, that means the pod continues to be running", func() {
			ctx := context.Background()
			pod := generateTestPodManifest("")
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}
			{
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
				Expect(err).Should(Succeed())

				Eventually(func() error {
					mutPod := &corev1.Pod{}
					err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
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
			}
		})
	})
	Context("An applied pod is a target & the sidecar container exits with code 0 and is not restarted", func() {
		It("should not do anything, that means the pod continues to be running", func() {
			ctx := context.Background()
			pod := generateTestPodManifest(testFluentPVCNameDefault)
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}
			{
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
				Expect(err).Should(Succeed())

				Eventually(func() error {
					mutPod := &corev1.Pod{}
					err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
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
					err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
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
			}
		})
	})
	Context("An applied pod is a target & the sidecar container exits with code != 0", func() {
		It("should not do anything if FluentPVC.DeletePodIfSidecarContainerTerminationDetected = false, that means the pod continues to be running", func() {
			ctx := context.Background()
			pod := generateTestPodManifest(testFluentPVCNameDeletePodFalse)
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}

			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
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
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
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
			pod := generateTestPodManifest(testFluentPVCNameSidecarFailed)
			pod.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}
			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
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
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
				if apierrors.IsNotFound(err) {
					return nil
				} else {
					return errors.New("Pod is still exist.")
				}
			}, 30).Should(Succeed())
		})
		It("should delete the pod which restartPolicy is 'Never'", func() {
			ctx := context.Background()
			pod := generateTestPodManifest(testFluentPVCNameSidecarFailed)
			pod.Spec.RestartPolicy = corev1.RestartPolicyNever
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).Should(Succeed())
			}
			Eventually(func() error {
				mutPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
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
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
				if apierrors.IsNotFound(err) {
					return nil
				} else {
					return errors.New("Pod is still exist.")
				}
			}, 30).Should(Succeed())
		})
	})
	Context("An applied pod is a target & the FluentPVC is deleted after the pod is applied", func() {
		It("should process correctly", func() {
			ctx := context.Background()
			pod := generateTestPodManifest("dummy-fluent-pvc")
			{
				err := k8sClient.Create(ctx, pod)
				Expect(err).To(HaveOccurred())
			}
		})
	})
})
