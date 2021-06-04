package pod

import (
	corev1 "k8s.io/api/core/v1"
)

func InjectOrReplaceContainer(podSpec *corev1.PodSpec, container *corev1.Container) {
	containers := []corev1.Container{}
	containerFound := false
	for _, c := range podSpec.Containers {
		if c.Name == container.Name {
			containerFound = true
			containers = append(containers, *container.DeepCopy())
			continue
		}
		containers = append(containers, *c.DeepCopy())
	}
	if !containerFound {
		containers = append(containers, *container.DeepCopy())
	}
	podSpec.Containers = containers
}

func InjectOrReplaceVolume(podSpec *corev1.PodSpec, volume *corev1.Volume) {
	volumes := []corev1.Volume{}
	volumeFound := false
	for _, v := range podSpec.Volumes {
		if v.Name == volume.Name {
			volumeFound = true
			volumes = append(volumes, *volume.DeepCopy())
			continue
		}
		volumes = append(volumes, *v.DeepCopy())
	}
	if !volumeFound {
		volumes = append(volumes, *volume.DeepCopy())
	}
	podSpec.Volumes = volumes
}

func InjectOrReplaceVolumeMount(podSpec *corev1.PodSpec, volumeMount *corev1.VolumeMount) {
	containers := []corev1.Container{}
	for _, c := range podSpec.Containers {
		c := *c.DeepCopy()
		volumeMounts := []corev1.VolumeMount{}
		volumeMountFound := false
		for _, vm := range c.VolumeMounts {
			if vm.Name == volumeMount.Name {
				volumeMountFound = true
				volumeMounts = append(volumeMounts, *volumeMount.DeepCopy())
				continue
			}
			volumeMounts = append(volumeMounts, *vm.DeepCopy())
		}
		if !volumeMountFound {
			volumeMounts = append(volumeMounts, *volumeMount.DeepCopy())
		}
		c.VolumeMounts = volumeMounts
		containers = append(containers, c)
	}
	podSpec.Containers = containers
}

func InjectOrReplaceEnv(podSpec *corev1.PodSpec, env *corev1.EnvVar) {
	containers := []corev1.Container{}
	for _, c := range podSpec.Containers {
		c := *c.DeepCopy()
		envs := []corev1.EnvVar{}
		envFound := false
		for _, e := range c.Env {
			if e.Name == env.Name {
				envFound = true
				envs = append(envs, *env.DeepCopy())
			}
			envs = append(envs, e)
		}
		if !envFound {
			envs = append(envs, *env.DeepCopy())
		}
		c.Env = envs
		containers = append(containers, c)
	}
	podSpec.Containers = containers
}
