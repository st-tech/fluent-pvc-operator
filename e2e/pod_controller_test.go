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

var _ = Describe("PVC controller", func() {
	const (
		testPodName                = "test-pod"
		testContainerName          = "test-container"
		testNamespace              = "default"
		testFluentPVCName          = "test-fluent-pvc"
		testSidecarContainerName   = "test-sidecar-container"
		testVolumeName             = "test-volume"
		testMountPath              = "/mnt/test"
		testFinalizerContainerName = "test-finalizer-container"
		testStorageClassName       = "standard"
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
				// ShareProcessNamespace: pointer.BoolPtr(true),
				Containers: []corev1.Container{
					{
						Name:  testContainerName,
						Args:  []string{"sleep", "1000"},
						Image: "krallin/ubuntu-tini:trusty",
					},
				},
			},
		}
		testFluentPVC = &fluentpvcv1alpha1.FluentPVC{
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
					StorageClassName: func(s string) *string { return &s }(testStorageClassName),
				},
				VolumeName:      testVolumeName,
				CommonMountPath: testMountPath,
				CommonEnv:       []corev1.EnvVar{},
				SidecarContainerTemplate: corev1.Container{
					Name:    testSidecarContainerName,
					Command: []string{"sleep", "5"},
					Image:   "alpine",
				},
				DeletePodIfSidecarContainerTerminationDetected: true,
				PVCFinalizerJobSpecTemplate: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								{
									Name:    testFinalizerContainerName,
									Command: []string{"echo", "test"},
									Image:   "alpine",
								},
							},
						},
					},
				},
			},
		}
	)
	BeforeEach(func() {
		err := k8sClient.Create(ctx, testFluentPVC.DeepCopy())
		Expect(err).NotTo(HaveOccurred())
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
		err := k8sClient.Delete(ctx, testFluentPVC)
		Expect(err).NotTo(HaveOccurred())
	})
	It("should pod ready when pod is fluent-pvc target", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: testFluentPVCName,
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
					if !stat.Ready || stat.State.Running == nil {
						return errors.New("Pod ContainerStatuses are not ready.")
					}
				}
				return nil
			}, 30).Should(Succeed())
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
			constants.PodAnnotationFluentPVCName: testFluentPVCName,
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
		mutFpvc := &fluentpvcv1alpha1.FluentPVC{}
		{
			err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCName}, mutFpvc)
			Expect(err).Should(Succeed())
		}
		mutFpvc.Spec.DeletePodIfSidecarContainerTerminationDetected = false
		mutFpvc.Spec.SidecarContainerTemplate.Command = []string{"sleep", "5;", "exit", "1"}
		{
			err := k8sClient.Update(ctx, mutFpvc)
			Expect(err).Should(Succeed())
		}
		pod := testPod.DeepCopy()
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: testFluentPVCName,
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
		mutFpvc := &fluentpvcv1alpha1.FluentPVC{}
		{
			err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCName}, mutFpvc)
			Expect(err).Should(Succeed())
		}
		mutFpvc.Spec.SidecarContainerTemplate.Command = []string{"sh", "-c", "sleep 5; exit 1"}
		{
			err := k8sClient.Update(ctx, mutFpvc)
			Expect(err).Should(Succeed())
		}
		pod := testPod.DeepCopy()
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: testFluentPVCName,
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
		mutFpvc := &fluentpvcv1alpha1.FluentPVC{}
		{
			err := k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCName}, mutFpvc)
			Expect(err).Should(Succeed())
		}
		mutFpvc.Spec.SidecarContainerTemplate.Command = []string{"sh", "-c", "sleep 5; exit 1"}
		{
			err := k8sClient.Update(ctx, mutFpvc)
			Expect(err).Should(Succeed())
		}
		pod := testPod.DeepCopy()
		pod.SetAnnotations(map[string]string{
			constants.PodAnnotationFluentPVCName: testFluentPVCName,
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
