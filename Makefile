
# Image URL to use all building/pushing image targets
IMG ?= fluent-pvc-operator:development
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false,maxDescLen=0"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-50s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: manifests generate fmt vet ## Run tests.
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.2/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test ./webhooks -coverprofile cover.out

##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

docker-build: test ## Build docker image with the manager.
	docker build -t ${IMG} .

docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -


CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

#======= END kubebuilder generated =======

##@ Development (User Defined Commands)

# parameters
TEST_KUBERNETES_TARGET ?= current

# dependency versions
KIND_VERSION := 0.10.0
CERT_MANAGER_VERSION := 1.3.1
BINDIR := $(shell pwd)/bin
KIND_CLUSTER_NAME := fluent-pvc-operator
KUSTOMIZE_DIR := $(shell pwd)/config/default
FLUENT_PVC_NAMESPACE := fluent-pvc-operator-system

ifeq ($(TEST_KUBERNETES_TARGET),current)
TEST_KUBERNETES_VERSION := 1.20
else ifeq ($(TEST_KUBERNETES_TARGET),prev1)
TEST_KUBERNETES_VERSION := 1.19
else ifeq ($(TEST_KUBERNETES_TARGET),prev2)
TEST_KUBERNETES_VERSION := 1.18
endif
export TEST_KUBERNETES_VERSION

ifeq ($(TEST_KUBERNETES_VERSION),1.20)
KUBERNETES_VERSION := 1.20.7
else ifeq ($(TEST_KUBERNETES_VERSION),1.19)
KUBERNETES_VERSION := 1.19.11
else ifeq ($(TEST_KUBERNETES_VERSION),1.18)
KUBERNETES_VERSION := 1.18.19
endif

.PHONY: launch-kind
launch-kind: kind kubectl shutdown-kind ## Launch a K8s cluster by kind.
	$(BINDIR)/kind create cluster --name=$(KIND_CLUSTER_NAME) --image kindest/node:v$(KUBERNETES_VERSION)
	$(BINDIR)/kubectl config use-context kind-$(KIND_CLUSTER_NAME)

.PHONY: cert-manager
cert-manager: kubectl ## Apply cert-manager into the K8s cluster specified in ~/.kube/config.
	$(BINDIR)/kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v$(CERT_MANAGER_VERSION)/cert-manager.yaml
	$(BINDIR)/kubectl wait -n cert-manager --for=condition=Available deployments --all --timeout=300s

.PHONY: kind-load-image-fluent-pvc-operator
kind-load-image-fluent-pvc-operator: docker-build kind ## Load the docker image into the K8s cluster launched by kind.
	$(BINDIR)/kind load docker-image --name $(KIND_CLUSTER_NAME) $(IMG)

.PHONY: shutdown-kind
shutdown-kind: kind ## Shutdown a K8s cluster by kind.
	$(BINDIR)/kind delete cluster --name=$(KIND_CLUSTER_NAME) || true

kind: ## Download kind locally if necessary.
ifeq (,$(wildcard $(BINDIR)/kind))
ifeq ($(shell uname),Darwin)
	curl --create-dirs -o $(BINDIR)/kind -sfL https://kind.sigs.k8s.io/dl/v$(KIND_VERSION)/kind-darwin-amd64
else
	curl --create-dirs -o $(BINDIR)/kind -sfL https://kind.sigs.k8s.io/dl/v$(KIND_VERSION)/kind-linux-amd64
endif
	chmod a+x $(BINDIR)/kind
endif

kubectl: ## Download kubectl locally if necessary.
ifeq (,$(wildcard $(BINDIR)/kubectl))
ifeq ($(shell uname),Darwin)
	curl --create-dirs -o $(BINDIR)/kubectl -sfL https://storage.googleapis.com/kubernetes-release/release/v$(KUBERNETES_VERSION)/bin/darwin/amd64/kubectl
else
	curl --create-dirs -o $(BINDIR)/kubectl -sfL https://storage.googleapis.com/kubernetes-release/release/v$(KUBERNETES_VERSION)/bin/linux/amd64/kubectl
endif
	chmod a+x $(BINDIR)/kubectl
endif

.PHONY: fluent-pvc-operator
fluent-pvc-operator: deploy ## Apply fluent-pvc-operator into the K8s cluster specified in ~/.kube/config.
	$(BINDIR)/kubectl wait -n $(FLUENT_PVC_NAMESPACE) --for=condition=Available deployments --all --timeout=300s

.PHONY: setup-e2e-test
setup-e2e-test: launch-kind cert-manager kind-load-image-fluent-pvc-operator fluent-pvc-operator

.PHONY: clean-e2e-test
clean-e2e-test: setup-e2e-test e2e-test ## Run e2e tests with relaunching the kind cluster.

.PHONY: e2e-test
e2e-test: ## Run e2e tests with the existing kind cluster.
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.2/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); USE_EXISTING_CLUSTER=true go test -timeout 1800s ./e2e -coverprofile cover-e2e.out

##@ Example Log Collection (User Defined Commands)

.PHONY: build-example-log-collection
build-example-log-collection: build-fluentd build-gcloud-pubsub-emulator build-sample-app ## Build all images for the log collection example.

EXAMPLE_LOG_COLLECTION_DIR = examples/log-collection
EXAMPLE_LOG_COLLECTION_IMG_PREFIX ?= fluent-pvc-operator-

.PHONY: build-fluentd
FLUENTD_IMG ?= fluentd:development
build-fluentd: ## Build fluentd image.
	cd $(CURDIR)/${EXAMPLE_LOG_COLLECTION_DIR}/fluentd \
		&& docker build -t ${EXAMPLE_LOG_COLLECTION_IMG_PREFIX}${FLUENTD_IMG} .

.PHONY: build-gcloud-pubsub-emulator
GCLOUD_PUBSUB_EMULATOR_IMG ?= gcloud-pubsub-emulator:development
build-gcloud-pubsub-emulator: ## Build gcloud-pubsub-emulator image.
	cd $(CURDIR)/${EXAMPLE_LOG_COLLECTION_DIR}/gcloud-pubsub-emulator \
		&& docker build -t ${EXAMPLE_LOG_COLLECTION_IMG_PREFIX}${GCLOUD_PUBSUB_EMULATOR_IMG} .

.PHONY: build-sample-app
SAMPLE_APP_IMG ?= sample-app:development
build-sample-app: ## Build sample-app image.
	cd $(CURDIR)/${EXAMPLE_LOG_COLLECTION_DIR}/sample-app \
		&& docker build -t ${EXAMPLE_LOG_COLLECTION_IMG_PREFIX}${SAMPLE_APP_IMG} .

.PHONY: kind-load-image-example-log-collection
kind-load-image-example-log-collection: kind-load-image-fluentd kind-load-image-gcloud-pubsub-emulator kind-load-image-sample-app ## Load all images for the log collection example into the K8s cluster launched by kind.

.PHONY: kind-load-image-fluentd
kind-load-image-fluentd: build-fluentd  ## Load the fluentd image into the K8s cluster launched by kind.
	$(BINDIR)/kind load docker-image --name $(KIND_CLUSTER_NAME) ${EXAMPLE_LOG_COLLECTION_IMG_PREFIX}${FLUENTD_IMG}

.PHONY: kind-load-image-gcloud-pubsub-emulator
kind-load-image-gcloud-pubsub-emulator: build-gcloud-pubsub-emulator  ## Load the gcloud-pubsub-emulator image into the K8s cluster launched by kind.
	$(BINDIR)/kind load docker-image --name $(KIND_CLUSTER_NAME) ${EXAMPLE_LOG_COLLECTION_IMG_PREFIX}${GCLOUD_PUBSUB_EMULATOR_IMG}

.PHONY: kind-load-image-sample-app
kind-load-image-sample-app: build-sample-app  ## Load the sample-app image into the K8s cluster launched by kind.
	$(BINDIR)/kind load docker-image --name $(KIND_CLUSTER_NAME) ${EXAMPLE_LOG_COLLECTION_IMG_PREFIX}${SAMPLE_APP_IMG}

.PHONY: deploy-example-log-collection
clean-deploy-example-log-collection: launch-kind cert-manager kind-load-image-fluent-pvc-operator deploy wait-fluent-pvc-operator kind-load-image-example-log-collection deploy-example-log-collection  ## Clean up the K8s cluster launched by kind, then deploy the log collection example.

.PHONY: deploy-example-log-collection
deploy-example-log-collection:  ## Deploy the log collection example.
	touch $(CURDIR)/${EXAMPLE_LOG_COLLECTION_DIR}/manifests/fluentd/credential.json
	$(KUSTOMIZE) build $(CURDIR)/${EXAMPLE_LOG_COLLECTION_DIR}/manifests | kubectl apply -f -

.PHONY: undeploy-example-log-collection
undeploy-example-log-collection:  ## Undeploy the log collection example.
	$(KUSTOMIZE) build $(CURDIR)/${EXAMPLE_LOG_COLLECTION_DIR}/manifests | kubectl delete -f -
