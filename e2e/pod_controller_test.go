package controllers

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

func GenerateFluentPVCForTest(
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

var _ = Describe("PVC controller", func() {
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
	var (
		testPod = &corev1.Pod{
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
	)
	BeforeEach(func() {
		{
			err := k8sClient.Create(ctx, GenerateFluentPVCForTest(testFluentPVCNameDefault, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Create(ctx, GenerateFluentPVCForTest(testFluentPVCNameDeletePodFalse, testSidecarContainerName, false, []string{"sh", "-c", "sleep 5; exit 1"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Create(ctx, GenerateFluentPVCForTest(testFluentPVCNameSidecarFailed, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5; exit 1"}))
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
		{
			err := k8sClient.Delete(ctx, GenerateFluentPVCForTest(testFluentPVCNameDefault, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Delete(ctx, GenerateFluentPVCForTest(testFluentPVCNameDeletePodFalse, testSidecarContainerName, false, []string{"sh", "-c", "sleep 5; exit 1"}))
			Expect(err).NotTo(HaveOccurred())
		}
		{
			err := k8sClient.Delete(ctx, GenerateFluentPVCForTest(testFluentPVCNameSidecarFailed, testSidecarContainerName, true, []string{"sh", "-c", "sleep 5; exit 1"}))
			Expect(err).NotTo(HaveOccurred())
		}
	})
	It("should pod ready when pod is not target", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
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
	It("should pod ready when sidecar exited with code 0", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: testFluentPVCNameDefault,
		})
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
	It("should pod ready when sidecar failed with code != 0 and DeletePodIfSidecarContainerTerminationDetected = false", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: testFluentPVCNameDeletePodFalse,
		})
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
	It("should pod with restartPolicy = OnFailure deleted when sidecar failed with code != 0", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: testFluentPVCNameSidecarFailed,
		})
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
	It("should pod with restartPolicy = Never deleted when sidecar failed with code != 0", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: testFluentPVCNameSidecarFailed,
		})
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
	It("should return error when fluent-pvc resouce is not found", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: "dummy-fluent-pvc",
		})
		{
			err := k8sClient.Create(ctx, pod)
			Expect(err).To(HaveOccurred())
		}
	})
})
