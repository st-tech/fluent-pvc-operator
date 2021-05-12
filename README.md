# fluent-pvc-operator

fluent-pvc-operator is a [Kubernetes Operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) designed to perfectly collect your event logs on Kubernetes. fluent-pvc-operator includes the following features.

- Deploy [Admission Webhook](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)s
  - PersistentVolumeClaim Dynamic Provisioner
    - Create a pvc when a pod scheduling
    - Attach the pvc the pod by injecting the settings to the manifest
    - (todo) Destroy the pvc when the pod destroying Or attach a label to the pvc when the pod destroying
  - Fluentd Sidecar Injector
    - Inject the fluentd settings into the manifest to collect all event logs
- Deploy PVC Teardown Controller
  - Search PVCs labeled deleted flags
  - Create a log-collection pod for each of the pvc
  - Destroy the pvc with the pv completely

