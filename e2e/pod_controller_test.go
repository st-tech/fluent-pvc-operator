package e2e

import (
	"reflect"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fluentpvcv1alpha1 "github.com/st-tech/fluent-pvc-operator/api/v1alpha1"
	"github.com/st-tech/fluent-pvc-operator/constants"
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
				fpvc := TestDefaultFluentPVC.DeepCopy()
				fpvc.SetName(id)
				tc.FindOrCreate(ctx, fpvc)

				pod := TestDefaultPod.DeepCopy()
				pod.SetName(id)
				pod.SetNamespace(id)
				tc.FindOrCreate(ctx, pod)

				By("expecting injecting nothing")
				pod = &corev1.Pod{}
				Expect(tc.Get(ctx, client.ObjectKey{Namespace: id, Name: id}, pod)).Should(Succeed())
				Expect(pod.Spec.Containers).Should(HaveLen(1))
				Expect(pod.Spec.Volumes).Should(HaveLen(1)) // NOTE: only default-token-xxxx exists.

				By("expecting the pod is running")
				EventuallyPodRunning(tc, ctx, id, id).Should(Succeed())
				ConsistentlyPodRunning(tc, ctx, id, id).Should(Succeed())
			})
		})
	})
	Context("A pod is a target", func() {
		When("the sidecar container exits", func() {
			type AssertBehaviorArg struct {
				fpvc                           *fluentpvcv1alpha1.FluentPVC
				pod                            *corev1.Pod
				deleteFluentPVCAfterPodApplied bool
				assertRestarting               bool
				assertConsistencyRunning       bool
				assertPodDeletion              bool
			}
			NewAssertBehaviorArg := func() *AssertBehaviorArg {
				return &AssertBehaviorArg{
					fpvc:                           TestDefaultFluentPVC.DeepCopy(),
					pod:                            TestDefaultPod.DeepCopy(),
					deleteFluentPVCAfterPodApplied: false,
					assertRestarting:               false,
					assertConsistencyRunning:       false,
					assertPodDeletion:              false,
				}
			}
			AssertBehavior := func(arg *AssertBehaviorArg) {
				By("preparing objects on k8s")
				arg.fpvc.SetName(id)
				tc.FindOrCreate(ctx, arg.fpvc)

				arg.pod.SetName(id)
				arg.pod.SetNamespace(id)
				arg.pod.SetAnnotations(map[string]string{constants.PodAnnotationFluentPVCName: id})
				tc.FindOrCreate(ctx, arg.pod)

				By("expecting injecting a sidecar container")
				pod := &corev1.Pod{}
				Expect(tc.Get(ctx, client.ObjectKey{Namespace: id, Name: id}, pod)).Should(Succeed())
				fpvc := &fluentpvcv1alpha1.FluentPVC{}
				Expect(tc.Get(ctx, client.ObjectKey{Name: id}, fpvc)).Should(Succeed())
				Expect(fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected).Should(BeEquivalentTo(arg.fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected))

				Expect(pod.Spec.Containers).Should(HaveLen(2))
				Expect(pod.Spec.Containers).Should(ContainElement(Satisfy(func(c corev1.Container) bool {
					return c.Name == fpvc.Spec.SidecarContainerTemplate.Name &&
						c.Image == fpvc.Spec.SidecarContainerTemplate.Image &&
						reflect.DeepEqual(c.Args, fpvc.Spec.SidecarContainerTemplate.Args)
				})))
				Expect(pod.Spec.Volumes).Should(HaveLen(2))
				Expect(pod.Spec.Volumes).Should(ContainElement(Satisfy(func(v corev1.Volume) bool {
					return v.Name == fpvc.Spec.PVCVolumeName &&
						v.PersistentVolumeClaim != nil
				})))

				By("expecting the pod is deleted")
				EventuallyPodRunning(tc, ctx, id, id).Should(Succeed())
				if arg.deleteFluentPVCAfterPodApplied {
					Eventually(func() error {
						return tc.Delete(ctx, fpvc, client.GracePeriodSeconds(0))
					}, 10).Should(Succeed())
				}
				if arg.assertRestarting {
					EventuallyPodContainerRestart(tc, ctx, id, id).Should(Succeed())
				}
				if arg.assertConsistencyRunning {
					ConsistentlyPodRunning(tc, ctx, id, id).Should(Succeed())
				}
				if arg.assertPodDeletion {
					EventuallyPodDeleted(tc, ctx, id, id).Should(Succeed())
				}
			}
			When("the exit code = 0", func() {
				It("should delete the pod", func() {
					arg := NewAssertBehaviorArg()
					arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerEcho.DeepCopy()
					arg.pod.Spec.RestartPolicy = corev1.RestartPolicyNever
					arg.assertPodDeletion = true
					AssertBehavior(arg)
				})
				It("should not delete the pod if DeletePodIfSidecarContainerTerminationDetected = false", func() {
					arg := NewAssertBehaviorArg()
					arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerEcho.DeepCopy()
					arg.fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected = false
					arg.pod.Spec.RestartPolicy = corev1.RestartPolicyNever
					arg.assertConsistencyRunning = true
					AssertBehavior(arg)
				})
			})
			When("the exit code != 0", func() {
				It("should delete the pod", func() {
					arg := NewAssertBehaviorArg()
					arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerExit1.DeepCopy()
					arg.pod.Spec.RestartPolicy = corev1.RestartPolicyNever
					arg.assertPodDeletion = true
					AssertBehavior(arg)
				})
				It("should not delete the pod if DeletePodIfSidecarContainerTerminationDetected = false", func() {
					arg := NewAssertBehaviorArg()
					arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerExit1.DeepCopy()
					arg.fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected = false
					arg.pod.Spec.RestartPolicy = corev1.RestartPolicyNever
					arg.assertConsistencyRunning = true
					AssertBehavior(arg)
				})
			})
			When("the sidecar container is restarted with exit code = 0", func() {
				It("should delete the pod", func() {
					arg := NewAssertBehaviorArg()
					arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerEcho.DeepCopy()
					arg.pod.Spec.RestartPolicy = corev1.RestartPolicyAlways
					arg.assertRestarting = true
					arg.assertPodDeletion = true
					AssertBehavior(arg)
				})
				It("should not delete the pod if DeletePodIfSidecarContainerTerminationDetected = false", func() {
					arg := NewAssertBehaviorArg()
					arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerEcho.DeepCopy()
					arg.fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected = false
					arg.pod.Spec.RestartPolicy = corev1.RestartPolicyAlways
					arg.assertRestarting = true
					arg.assertConsistencyRunning = true
					AssertBehavior(arg)
				})
			})
			When("the sidecar container is restarted with exit code != 0", func() {
				It("should delete the pod", func() {
					arg := NewAssertBehaviorArg()
					arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerExit1.DeepCopy()
					arg.pod.Spec.RestartPolicy = corev1.RestartPolicyAlways
					arg.assertRestarting = true
					arg.assertPodDeletion = true
					AssertBehavior(arg)
				})
				It("should not delete the pod if DeletePodIfSidecarContainerTerminationDetected = false", func() {
					arg := NewAssertBehaviorArg()
					arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerExit1.DeepCopy()
					arg.fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected = false
					arg.pod.Spec.RestartPolicy = corev1.RestartPolicyAlways
					arg.assertRestarting = true
					arg.assertConsistencyRunning = true
					AssertBehavior(arg)
				})
			})
			When("the FluentPVC is deleted after the pod is applied", func() {
				When("the exit code = 0", func() {
					It("should delete the pod", func() {
						arg := NewAssertBehaviorArg()
						arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerEcho.DeepCopy()
						arg.pod.Spec.RestartPolicy = corev1.RestartPolicyNever
						arg.assertPodDeletion = true
						arg.deleteFluentPVCAfterPodApplied = true
						AssertBehavior(arg)
					})
					It("should not delete the pod if DeletePodIfSidecarContainerTerminationDetected = false", func() {
						arg := NewAssertBehaviorArg()
						arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerEcho.DeepCopy()
						arg.fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected = false
						arg.pod.Spec.RestartPolicy = corev1.RestartPolicyNever
						arg.assertConsistencyRunning = true
						arg.deleteFluentPVCAfterPodApplied = true
						AssertBehavior(arg)
					})
				})
				When("the exit code != 0", func() {
					It("should delete the pod", func() {
						arg := NewAssertBehaviorArg()
						arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerExit1.DeepCopy()
						arg.pod.Spec.RestartPolicy = corev1.RestartPolicyNever
						arg.assertPodDeletion = true
						arg.deleteFluentPVCAfterPodApplied = true
						AssertBehavior(arg)
					})
					It("should not delete the pod if DeletePodIfSidecarContainerTerminationDetected = false", func() {
						arg := NewAssertBehaviorArg()
						arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerExit1.DeepCopy()
						arg.fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected = false
						arg.pod.Spec.RestartPolicy = corev1.RestartPolicyNever
						arg.assertConsistencyRunning = true
						arg.deleteFluentPVCAfterPodApplied = true
						AssertBehavior(arg)
					})
				})
				When("the sidecar container is restarted with exit code = 0", func() {
					It("should delete the pod", func() {
						arg := NewAssertBehaviorArg()
						arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerEcho.DeepCopy()
						arg.pod.Spec.RestartPolicy = corev1.RestartPolicyAlways
						arg.assertRestarting = true
						arg.assertPodDeletion = true
						arg.deleteFluentPVCAfterPodApplied = true
						AssertBehavior(arg)
					})
					It("should not delete the pod if DeletePodIfSidecarContainerTerminationDetected = false", func() {
						arg := NewAssertBehaviorArg()
						arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerEcho.DeepCopy()
						arg.fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected = false
						arg.pod.Spec.RestartPolicy = corev1.RestartPolicyAlways
						arg.assertRestarting = true
						arg.assertConsistencyRunning = true
						arg.deleteFluentPVCAfterPodApplied = true
						AssertBehavior(arg)
					})
				})
				When("the sidecar container is restarted with exit code != 0", func() {
					It("should delete the pod", func() {
						arg := NewAssertBehaviorArg()
						arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerExit1.DeepCopy()
						arg.pod.Spec.RestartPolicy = corev1.RestartPolicyAlways
						arg.assertRestarting = true
						arg.assertPodDeletion = true
						arg.deleteFluentPVCAfterPodApplied = true
						AssertBehavior(arg)
					})
					It("should not delete the pod if DeletePodIfSidecarContainerTerminationDetected = false", func() {
						arg := NewAssertBehaviorArg()
						arg.fpvc.Spec.SidecarContainerTemplate = *TestSidecarContainerExit1.DeepCopy()
						arg.fpvc.Spec.DeletePodIfSidecarContainerTerminationDetected = false
						arg.pod.Spec.RestartPolicy = corev1.RestartPolicyAlways
						arg.assertRestarting = true
						arg.assertConsistencyRunning = true
						arg.deleteFluentPVCAfterPodApplied = true
						AssertBehavior(arg)
					})
				})
			})
		})
	})
})
