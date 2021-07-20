package e2e

import (
	"context"

	. "github.com/onsi/gomega"
	"golang.org/x/xerrors"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	defaultEventuallyTimeoutSeconds    int = 300
	defaultConsistentlyDurationSeconds int = 30
)

func checkPodRunning(c client.Client, ctx context.Context, namespace, name string) error {
	pod := &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pod); err != nil {
		return err
	}
	if pod.Status.Phase != corev1.PodRunning {
		return xerrors.New("Pod is not running.: " + string(pod.Status.Phase))
	}
	return nil
}

func EventuallyPodRunning(c client.Client, ctx context.Context, namespace, name string) AsyncAssertion {
	return Eventually(func() error {
		return checkPodRunning(c, ctx, namespace, name)
	}, defaultEventuallyTimeoutSeconds)
}

func EventuallyPodContainerRestart(c client.Client, ctx context.Context, namespace, name string) AsyncAssertion {
	return Eventually(func() error {
		pod := &corev1.Pod{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pod); err != nil {
			return err
		}
		for _, c := range pod.Status.ContainerStatuses {
			if c.RestartCount > 0 {
				return nil
			}
		}
		return xerrors.New("No container restarts")
	}, defaultEventuallyTimeoutSeconds)
}

func ConsistentlyPodRunning(c client.Client, ctx context.Context, namespace, name string) AsyncAssertion {
	return Consistently(func() error {
		return checkPodRunning(c, ctx, namespace, name)
	}, defaultConsistentlyDurationSeconds)
}

func EventuallyPodDeleted(c client.Client, ctx context.Context, namespace, name string) AsyncAssertion {
	return Eventually(func() error {
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &corev1.Pod{}); err != nil {
			return client.IgnoreNotFound(err)
		}
		return xerrors.New("Pod is running.")
	}, defaultEventuallyTimeoutSeconds)
}
