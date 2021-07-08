package webhooks

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
)

var _ = Describe("FluentPVC Validation Webhook", func() {
	const (
		testFluentPVCName          = "test-fluent-pvc"
		testSidecarContainerName   = "test-sidecar-container"
		testVolumeName             = "test-volume"
		testMountPath              = "/mnt/test"
		testFinalizerContainerName = "test-finalizer-container"
		testStorageClassName       = "test-storage-class"
	)
	var (
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
	})
	AfterEach(func() {
		err := k8sClient.Delete(ctx, testStorageClass)
		Expect(err).NotTo(HaveOccurred())
		err = k8sClient.Delete(ctx, testFluentPVC)
		Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
	})
	It("should create a FluentPVC when the Spec is valid.", func() {
		ctx := context.Background()
		fpvc := testFluentPVC.DeepCopy()
		err := k8sClient.Create(ctx, fpvc)
		Expect(err).Should(Succeed())

		fpvc = &fluentpvcv1alpha1.FluentPVC{}
		err = k8sClient.Get(ctx, client.ObjectKey{Name: testFluentPVCName}, fpvc)
		Expect(err).Should(Succeed())

	})
	It("should return a error when PVCSpecTemplate.AccessModes is invalid.", func() {
		ctx := context.Background()
		fpvc := testFluentPVC.DeepCopy()

		fpvc.Spec.PVCSpecTemplate.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadOnlyMany}
		err := k8sClient.Create(ctx, fpvc)

		Expect(err).ShouldNot(Succeed())
		Expect(err.Error()).Should(BeEquivalentTo(
			"admission webhook \"fluent-pvc-validation-webhook.fluent-pvc-operator.tech.zozo.com\" denied" +
				" the request: Only 'ReadWriteOnce' is acceptable for FluentPVC.spec.pvcSpecTemplate.accessModes, but '[ReadOnlyMany]' is specified.",
		))
	})
	It("should return a error when PVCSpecTemplate.StorageClassName is invalid.", func() {
		ctx := context.Background()
		fpvc := testFluentPVC.DeepCopy()

		fpvc.Spec.PVCSpecTemplate.StorageClassName = func(s string) *string { return &s }("dummy-storage-class")
		err := k8sClient.Create(ctx, fpvc)

		Expect(err).ShouldNot(Succeed())
		Expect(err.Error()).Should(BeEquivalentTo(
			"admission webhook \"fluent-pvc-validation-webhook.fluent-pvc-operator.tech.zozo.com\" denied" +
				" the request: StorageClass.storage.k8s.io \"dummy-storage-class\" not found",
		))
	})
	It("should return a error when JobSpec is invalid.", func() {
		ctx := context.Background()
		fpvc := testFluentPVC.DeepCopy()

		fpvc.Spec.PVCFinalizerJobSpecTemplate.Template.Spec.RestartPolicy = corev1.RestartPolicyAlways
		err := k8sClient.Create(ctx, fpvc)

		Expect(err).ShouldNot(Succeed())
		Expect(err.Error()).Should(BeEquivalentTo(
			"admission webhook \"fluent-pvc-validation-webhook.fluent-pvc-operator.tech.zozo.com\" denied" +
				" the request: Job.batch \"test-fluent-pvc\" is invalid: spec.template.spec.restartPolicy: Unsupported value: \"Always\": supported values: \"OnFailure\", \"Never\"",
		))
	})
	It("should return a error when PVCSpec is invalid.", func() {
		ctx := context.Background()
		fpvc := testFluentPVC.DeepCopy()

		var PersistentVolumeModeDummy corev1.PersistentVolumeMode = "Dummy"
		fpvc.Spec.PVCSpecTemplate.VolumeMode = &PersistentVolumeModeDummy
		err := k8sClient.Create(ctx, fpvc)

		Expect(err).ShouldNot(Succeed())
		Expect(err.Error()).Should(BeEquivalentTo(
			"admission webhook \"fluent-pvc-validation-webhook.fluent-pvc-operator.tech.zozo.com\" denied" +
				" the request: PersistentVolumeClaim \"test-fluent-pvc\" is invalid: spec.volumeMode: Unsupported value: \"Dummy\": supported values: \"Block\", \"Filesystem\"",
		))
	})
	It("should return a error when SidecarContainerSpec is invalid.", func() {
		ctx := context.Background()
		fpvc := testFluentPVC.DeepCopy()

		fpvc.Spec.SidecarContainerTemplate.Name = ""
		err := k8sClient.Create(ctx, fpvc)

		Expect(err).ShouldNot(Succeed())
		Expect(err.Error()).Should(BeEquivalentTo(
			"admission webhook \"fluent-pvc-validation-webhook.fluent-pvc-operator.tech.zozo.com\" denied" +
				" the request: Pod \"test-fluent-pvc\" is invalid: spec.containers[0].name: Required value",
		))
	})
})
