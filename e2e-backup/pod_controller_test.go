package e2e

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
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
