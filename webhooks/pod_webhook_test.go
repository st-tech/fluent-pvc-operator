package webhooks

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
)

var _ = Describe("Pod Mutation Webhook", func() {
	const (
		testPodName                = "test-pod"
		testContainerName          = "test-container"
		testNamespace              = "default"
		testFluentPVCName          = "test-fluent-pvc"
		testSidecarContainerName   = "test-sidecar-container"
		testVolumeName             = "test-volume"
		testMountPath              = "/mnt/test"
		testFinalizerContainerName = "test-finalizer-container"
		testStorageClassName       = "test-storage-class"
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
						Name:    testContainerName,
						Command: []string{"echo", "test"},
						Image:   "alpine",
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
				PVCVolumeName:      testVolumeName,
				PVCVolumeMountPath: testMountPath,
				CommonEnvs:         []corev1.EnvVar{},
				SidecarContainerTemplate: corev1.Container{
					Name:    testSidecarContainerName,
					Command: []string{"echo", "test"},
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
		testStorageClass = &storagev1.StorageClass{
			TypeMeta: metav1.TypeMeta{
				APIVersion: storagev1.SchemeGroupVersion.String(),
				Kind:       "StorageClass",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: testStorageClassName,
			},
			Provisioner:       "kubernetes.io/no-provisioner",
			VolumeBindingMode: func(m storagev1.VolumeBindingMode) *storagev1.VolumeBindingMode { return &m }(storagev1.VolumeBindingWaitForFirstConsumer),
		}
	)
	BeforeEach(func() {
		err := k8sClient.Create(ctx, testStorageClass.DeepCopy())
		Expect(err).NotTo(HaveOccurred())
		err = k8sClient.Create(ctx, testFluentPVC.DeepCopy())
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func() {
		// Clean up the Pod if created.
		pod := &corev1.Pod{}
		pod.SetNamespace(testNamespace)
		pod.SetName(testPodName)
		if err := k8sClient.Delete(ctx, pod); err != nil {
			if !apierrors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		}
		// Clean up the StorageClass and the FluentPVC.
		err := k8sClient.Delete(ctx, testStorageClass)
		Expect(err).NotTo(HaveOccurred())
		err = k8sClient.Delete(ctx, testFluentPVC)
		Expect(err).NotTo(HaveOccurred())
	})
	It("should create a PVC and a FluentPVCBinding", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
		pod.SetLabels(map[string]string{
			constants.PodLabelFluentPVCName: testFluentPVCName,
		})
		{
			err := k8sClient.Create(ctx, pod)
			Expect(err).Should(Succeed())
		}
		{
			mutPod := &corev1.Pod{}
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
			Expect(err).Should(Succeed())

			var volumeForPVC *corev1.Volume
			for _, v := range mutPod.Spec.Volumes {
				if v.PersistentVolumeClaim != nil && v.Name == testVolumeName {
					volumeForPVC = v.DeepCopy()
				}
			}
			Expect(volumeForPVC).NotTo(BeNil())

			var sidecarContainer *corev1.Container
			for _, c := range mutPod.Spec.Containers {
				if c.Name == testSidecarContainerName {
					sidecarContainer = c.DeepCopy()
				}
				var volumeMount *corev1.VolumeMount
				for _, vm := range c.VolumeMounts {
					if vm.Name == testVolumeName {
						volumeMount = vm.DeepCopy()
					}
				}
				Expect(volumeMount).NotTo(BeNil())
				Expect(volumeMount.MountPath).Should(BeEquivalentTo(testMountPath))
			}
			Expect(sidecarContainer).NotTo(BeNil())

			pvc := &corev1.PersistentVolumeClaim{}
			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: volumeForPVC.PersistentVolumeClaim.ClaimName}, pvc)
			Expect(err).Should(Succeed())
			Expect(pvc.Finalizers).Should(ContainElement(constants.PVCFinalizerName))

			b := &fluentpvcv1alpha1.FluentPVCBinding{}
			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pvc.Name}, b)
			Expect(err).Should(Succeed())
			Expect(b.Finalizers).Should(ContainElement(constants.FluentPVCBindingFinalizerName))

			Expect(mutPod.Labels).ShouldNot(BeEmpty())
			Expect(mutPod.Labels[constants.PodLabelFluentPVCBindingName]).Should(BeEquivalentTo(b.Name))

			bOwner := metav1.GetControllerOf(b)
			Expect(bOwner.APIVersion).Should(BeEquivalentTo(fluentpvcv1alpha1.GroupVersion.String()))
			Expect(bOwner.Kind).Should(BeEquivalentTo("FluentPVC"))
			Expect(bOwner.Name).Should(BeEquivalentTo(testFluentPVCName))
		}

	})
	It("should return a error when FluentPVC is not found.", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
		pod.SetLabels(map[string]string{
			constants.PodLabelFluentPVCName: "IS_NOT_FOUND",
		})
		err := k8sClient.Create(ctx, pod)
		Expect(err).ShouldNot(Succeed())
		Expect(err.Error()).Should(BeEquivalentTo(
			"admission webhook \"pod-mutation-webhook.fluent-pvc-operator.tech.zozo.com\" denied" +
				" the request: FluentPVC.fluent-pvc-operator.tech.zozo.com \"IS_NOT_FOUND\" not found",
		))
	})
	It("should not patch the Pod when the Pod is not a target of FluentPVC.", func() {
		ctx := context.Background()
		pod := testPod.DeepCopy()
		err := k8sClient.Create(ctx, pod)
		Expect(err).Should(Succeed())
		mutPod := &corev1.Pod{}
		err = k8sClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, mutPod)
		Expect(err).Should(Succeed())

		var volumeForPVC *corev1.Volume
		for _, v := range mutPod.Spec.Volumes {
			if v.PersistentVolumeClaim != nil {
				volumeForPVC = &v
			}
		}
		Expect(volumeForPVC).To(BeNil())
	})
})
