package e2e

import (
	// "reflect"

	. "github.com/onsi/ginkgo"
	// . "github.com/onsi/gomega"

	// batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	// apierrors "k8s.io/apimachinery/pkg/api/errors"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "sigs.k8s.io/controller-runtime/pkg/client"
	// fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	// "github.com/st-tech/fluent-pvc-operator/constants"
	// "github.com/st-tech/fluent-pvc-operator/constants"
	// hashutils "github.com/st-tech/fluent-pvc-operator/utils/hash"
	// podutils "github.com/st-tech/fluent-pvc-operator/utils/pod"
)

var _ = Describe("pod_controller", func() {
	var tc *TestK8SClient
	var id string

	BeforeEach(func() {
		tc = NewTestK8SClient(k8sClient)
		id = RandomString()
		ns := &corev1.Namespace{}
		ns.SetName(id)
		tc.FindOrCreate(ctx, ns)
	})
	AfterEach(func() {
		tc.DeleteAllInNamespace(ctx, id, &corev1.Pod{})
		tc.DeleteFluentPVC(ctx, id)
		tc.DeleteNamespace(ctx, id)
	})
	// NOTE: The "essential" container means that the container fails or stops for any reason
	//       then all other containers that are part of the pod should be stopped.
	Context("the essential container exits", func() {
		When("the exit code = 0", func() {
			It("should create a finalizer job", func() {
				// the same name as the fpvc-b name

			})
			When("the finalizer job is succeeded", func() {
				It("should remove the pvc", func() {
					// delete finalizer

				})
			})
			When("the finalizer job is failed", func() {
				It("should not do anything until the job is deleted", func() {

				})
			})
		})
		When("the exit code != 0", func() {
			It("should create a finalizer job", func() {

			})
			When("the finalizer job is succeeded", func() {
				It("should remove the pvc", func() {
					// delete finalizer

				})
			})
			When("the finalizer job is failed", func() {
				It("should not do anything until the job is deleted", func() {

				})
			})
		})
	})
	Context("the essential container is running", func() {
		When("the fluentpvcbinding become Unknown condition", func() {
			It("should skip processing the pvc", func() {

			})
		})
	})
})
