# fluent-pvc-operator

fluent-pvc-operator is a [Kubernetes Operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) that aims to dynamically provision PVCs to Pods. This Operator make you possible to attach a disposable PVC to a Pod without issuing PVCs in advance.

## Motivation

The issues we want to solve with this Operator are as follows:

- Provide a way to protect the data in a Pod from sudden death of a Node by persisting the data to a PersistentVolume.
- Enable to offload filesystem-dependent Pod termination operations to other Pods.
- Make PVC available for Pods generated from Templates such as Deployment and DaemonSet.

## Features

fluent-pvc-operator has the following features:

- **Dynamic PVC Provisioning**: Creates a PVC and injects it into the Pod Manifest on Pods creation admission webhook.
- **Sidecar Container Injection**: Injects a container definition into the Pod Manifest on Pods creation admission webhook.
- **Unhealthy Pod Auto Deletion**: Detects anomalies in the Injected Sidecar Container and automatically deletes the Pod.
- **PVC Auto Finalization**: After the Pod is deleted, a Job is automatically issued to process the data in the PVC, and if the Job is successful, the PVC is deleted.

### Planned Features

- **Sidecar Container Auto Termination**: Terminates the Sidecar Container automatically when the specified Container in the Pod has been terminated. This feature is intended to be used in Job.
  - This feature will become not needed once [lifecycle for Sidecar Container](https://github.com/kubernetes/enhancements/tree/0e4d5df/keps/sig-node/753-sidecar-containers) is provided as a feature of Kubernetes.

## Custom Resource Definitions

There are two Custom Resource Definitions that fluent-pvc-operator installs:

- [`fluentpvcs.fluent-pvc-operator.tech.zozo.com`](./config/crd/bases/fluent-pvc-operator.tech.zozo.com_fluentpvcs.yaml)
  - These Custom Resources define the settings required to use fluent-pvc-operator, such as the template of the PVC to be provisioned and the definition of the Sidecar Container.
  - The detailed explanation is provided in the [Configurations](#configurations) section.
- [`fluentpvcbindings.fluent-pvc-operator.tech.zozo.com`](.config/crd/bases/fluent-pvc-operator.tech.zozo.com_fluentpvcbindings.yaml)
  - These Custom Resources are automatically generated by fluent-pvc-operator internally for the purpose of managing the state of FluentPVC, Pod, PVC and Job.
  - Users do not define this Custom Resource.

## Usage
Put `fluent-pvc-operator.tech.zozo.com/fluent-pvc-name: <YOUR_DEFINED_FLUENT_PVC>` in the annotations of your pod, then fluent-pvc-operator processes the pod as a target.

```
apiVersion: v1
kind: Pod
metadata:
  annotations:
    fluent-pvc-operator.tech.zozo.com/fluent-pvc-name: fluent-pvc-sample
  name: your-pod
spec:
  ...
```

### Behaviors

- On Pod Scheduling
  - Create a PVC for the Pod.
  - Inject the PVC to the Pod Manifest.
  - Inject the Sidecar Container Definition to the Pod Manifest.
- On Pod Running
  - Monitor the Sidecar Container status.
  - Delete the Pod when the Sidecar Container is terminated with exit code != 0.
- On Pod Terminated
  - Apply the finalizer Job for the PVC.
  - Delete the PVC when the finalizer Job is succeeded.

## Configurations

### [`fluentpvcs.fluent-pvc-operator.tech.zozo.com`](./config/crd/bases/fluent-pvc-operator.tech.zozo.com_fluentpvcs.yaml )

|name|type|required?|default|description|
|:---|:---|:--------|:------|:----------|
|pvcSpecTemplate|[PersistentVolumeClaimSpec](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/persistent-volume-claim-v1/#PersistentVolumeClaimSpec)|true||Template to provision PVCs|
|pvcFinalizerJobSpecTemplate|[JobSpec](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/job-v1/#JobSpec)|true||Template to apply Jobs for finalizing PVCs|
|pvcVolumeName|string|true||Name of [Volume](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/volume/#Volume) to use PVCs for Pods. Must be a [DNS_LABEL](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names) and unique within the Pod.|
|pvcVolumeMountPath|string|true||Path to mount containers as a [VolumeMount](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#volumes-1).Must not contain ':'.|
|sidecarContainerTemplate|[Container](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#Container)|true||Template for Sidecar Container injected into Pods.|
|commonEnvs|[][EnvVar](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#environment-variables)|false|`[]`|Common Environment Variables for all containers|
|commonVolumes|[][Volume](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/volume/#Volume)|false|`[]`|Common [Volume](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/volume/#Volume)s for all Pods|
|commonVolumeMounts|[][VolumeMount](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#volumes-1)|false|`[]`|Common [VolumeMount](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#volumes-1)s for all containers|
|deletePodIfSidecarContainerTerminationDetected|boolean|false|`true`|Flag to delete Pods when the injected sidecar container termination is detected.|

sample

```yaml
apiVersion: fluent-pvc-operator.tech.zozo.com/v1alpha1
kind: FluentPVC
metadata:
  name: fluent-pvc-sample
spec:
  pvcSpecTemplate:
    accessModes: [ "ReadWriteOnce" ]
    storageClassName: standard
    resources:
      requests:
        storage: 1Gi
  pvcFinalizerJobSpecTemplate:
    template:
      spec:
        restartPolicy: Never
        containers:
          - name: sidecar
            image: alpine:latest
            imagePullPolicy: Always
            command: [echo, finalizer]
            resources:
              limits:
                cpu: '1'
                memory: 1Gi
  pvcVolumeName: fluent-pvc
  pvcVolumeMountPath: /mnt/fluent-pvc
  sidecarContainerTemplate:
    name: sidecar
    image: alpine:latest
    imagePullPolicy: Always
    command: [echo sidecar]
    resources:
      limits:
        cpu: '1'
        memory: 1Gi
  deletePodIfSidecarContainerTerminationDetected: true
  commonEnvs:
    - name: FLUENT_PVC_MOUNT_DIR
      value: /mnt/fluent-pvc
  commonVolumes:
    - name: SOME_SECRET
      secret:
        secretName: some-secret
  commonVolumeMounts:
    - name: SOME_SECRET
      mountPath: /path/to/secret
```

## Installs

```
$ git clone https://github.com/st-tech/fluent-pvc-operator.git
$ cd fluent-pvc-operator
$ make deploy IMG=ghcr.io/st-tech/fluent-pvc-operator:0.0.1
```

## Requirements
- Kubernetes: 1.20, 1.19, 1.18
- [Cert Manager](https://cert-manager.io/docs/installation/kubernetes/)

## Designs

### Programs

- [fluentpvc_controller.go](./controllers/fluentpvc_controller.go)
  - Monitor the Finalizer of all FluentPVCBindings whose Owner Controller is the FluentPVC.
  - Remove the Finalizer from FluentPVC after the Finalizer is removed from all FluentPVCBindings.
- [fluentpvcbinding_controller.go](.controllers/fluentpvcbinding_controller.go)
  - Monitor the Pod, PVC and Job defined in FluentPVCBinding.
  - Update the condition of FluentPVCBinding according to each condition change.
  - Each controller decides what to do according to the condition of FluentPVCBinding.
  - Cannot delete FluentPVCBinding until the PVC Finalizer `fluent-pvc-operator.tech.zozo.com/pvc-protection` is deleted.
- [pod_controller.go](./controllers/pod_controller.go)
  - Monitor the Pod defined in FluentPVCBinding.
  - Delete the Pod if the Sidecar Container anomaly is detected.
- [pvc_controller.go](./controllers/pvc_controller.go)
  - Monitor the PVC defined in FluentPVCBinding.
  - Apply the Job to finalize the PVC that the Pod is no longer in use.
  - Delete the PVC when the Job is succeeded.
- [pod_webhook.go](./webhooks/pod_webhook.go)
  - Mutate Pods on Pods creation.
  - Creates PVCs and inject the PVC into Pods.
  - Inject the sidecar container definition into Pods.
  - Creates FluentPVCBindings with FluentPVC, Pod, and PVC identities.

### State Transition Diagrams

TBD.

## Development

Use [kind](https://kind.sigs.k8s.io/) to create local Kubernetes clusters.

### Create a Kubernetes Cluster for development

```sh
$ kind create cluster
$ make cert-manager
```

### Build fluent-pvc-operator

```sh
$ make docker-build
```

### Load the image into the kind cluster

```sh
$ make kind-load-image-fluent-pvc-operator
```

### Deploy fluent-pvc-operator

```sh
$ make fluent-pvc-operator
```

### Watch the behaviors

```sh
$ kubectl apply -f config/samples/fluent-pvc-operator_v1alpha1_fluentpvc.yaml
$ kubectl run --image=alpine:latest --annotations fluent-pvc-operator.tech.zozo.com/fluent-pvc-name=fluent-pvc-sample sample-pod -- sh -c 'for i in $(seq 1 60); do sleep 1; echo $i; done'

## You can watch the status changes by the following command.
$ watch -n1 "
echo '=======FluentPVC======='
kubectl get fluentpvc
echo '=======FluentPVCBinding======='
kubectl get fluentpvcbinding
echo '=======PVC======='
kubectl get pvc
echo '=======Job======='
kubectl get job
echo '=======Pod======='
kubectl get pod
echo '=============='
"
```

### Run unit tests

```sh
$ make test
```

These tests are runnable without kind clusters.

### Run e2e tests

```sh
## Run e2e tests with recreating the kind cluster.
$ make e2e/clean-test

## Run e2e tests on the existing kind cluster.
$ make e2e/test
```

## Examples

The [examples](./examples) directory contains several examples that can be used as a reference for using fluent-pvc-operator.

### For log-collection

This example assumes the usecase where the Pod logs are collected by fluentd and sent to [Cloud Pub/Sub](https://cloud.google.com/pubsub). The Cloud Pub/Sub used in this case is launched as an Emulator in the same cluster, so there is no need to prepare anything.

#### Build docker images

```sh
$ make examples/log-collection/build
```

#### Load docker images into Kubernetes cluster created by kind

```sh
$ make examples/log-collection/kind-load-image
```

#### Deploy the example manifests

```sh
$ make examples/log-collection/deploy
```

You can deploy manifests with recreating the Kubernetes cluster by kind.

```sh
$ make examples/log-collection/clean-deploy
```

## CHANGELOG

[CHANGELOG.md](./CHANGELOG.md)

## License

[MIT LICENSE](./LICENSE)
