package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PVC controller", func() {

	const (
		pvcName = "sample"
	)

	Context("when creating PVC resource", func() {
		It("Should create PVC", func() {
			ctx := context.Background()

			storageClassName := "standard"
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvcName,
					Namespace: corev1.NamespaceDefault,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
					StorageClassName: &storageClassName,
				},
			}
			err := k8sClient.Create(ctx, pvc)
			Expect(err).Should(Succeed())

			createdPVC := &corev1.PersistentVolumeClaim{}
			Eventually(func() error {
				err := k8sClient.Get(
					ctx,
					client.ObjectKey{Name: pvcName, Namespace: corev1.NamespaceDefault}, createdPVC)
				if err != nil {
					return err
				}
				return nil
			}).Should(Succeed())

			nsList := &corev1.NamespaceList{}
			err = k8sClient.List(ctx, nsList)
			Expect(err).Should(Succeed())
		})
	})
})
