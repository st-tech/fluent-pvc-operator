package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/xerrors"

	// batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	// apierrors "k8s.io/apimachinery/pkg/api/errors"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
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
	Context("A pod is not a target", func() {
		When("a pod is applied", func() {
			It("should not do anything", func() {
				By("preparing objects on k8s")
				fpvc := &fluentpvcv1alpha1.FluentPVC{}
				fpvc.SetName(id)
				Expect(FillFluentPVCDefault(fpvc)).Should(Succeed())
				tc.FindOrCreate(ctx, fpvc)

				pod := &corev1.Pod{}
				pod.SetName(id)
				pod.SetNamespace(id)
				Expect(FillPodDefault(pod)).Should(Succeed())
				tc.FindOrCreate(ctx, pod)

				By("expecting injecting nothing")
				pod = &corev1.Pod{}
				Expect(tc.Get(ctx, client.ObjectKey{Namespace: id, Name: id}, pod)).Should(Succeed())
				Expect(pod.Spec.Containers).Should(HaveLen(1))
				Expect(pod.Spec.Volumes).Should(HaveLen(1)) // NOTE: only default-token-xxxx exists.

				By("expecting the pod is running")
				checkFn := func() error {
					pod := &corev1.Pod{}
					if err := tc.Get(ctx, client.ObjectKey{Namespace: id, Name: id}, pod); err != nil {
						return err
					}
					if pod.Status.Phase != corev1.PodRunning {
						return xerrors.New("Pod is not running.")
					}
					return nil
				}
				Eventually(checkFn, 10).Should(Succeed())
				Consistently(checkFn, 10).Should(Succeed())
			})
		})
	})
})
