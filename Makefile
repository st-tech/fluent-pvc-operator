
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
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9\\\/\\\%_-]+:.*?##/ { printf "  \033[36m%-50s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: bin/controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: bin/controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: manifests generate fmt vet ## Run tests.
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.2/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test $(go list ./... | grep -v /e2e) -coverprofile cover.out

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

install: manifests bin/kustomize bin/kubectl ## Install CRDs into the k8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests bin/kustomize bin/kubectl ## Uninstall CRDs from the k8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests bin/kustomize bin/kubectl ## Deploy controller to the k8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

undeploy: bin/kubectl ## Undeploy controller from the k8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

KUBECTL_WAIT_TIMEOUT ?= 300s
FLUENT_PVC_NAMESPACE = fluent-pvc-operator-system
fluent-pvc-operator: deploy ## Deploy fluent-pvc-operator into the k8s cluster specified in ~/.kube/config && Wait until it becomes available.
	$(KUBECTL) wait -n $(FLUENT_PVC_NAMESPACE) --for=condition=Available deployments --all --timeout=$(KUBECTL_WAIT_TIMEOUT)

CERT_MANAGER_VERSION = 1.3.1
cert-manager: bin/kubectl ## Deploy cert-manager into the k8s cluster specified in ~/.kube/config && Wait until it becomes available.
	$(KUBECTL) apply -f https://github.com/jetstack/cert-manager/releases/download/v$(CERT_MANAGER_VERSION)/cert-manager.yaml
	$(KUBECTL) wait -n cert-manager --for=condition=Available deployments --all --timeout=$(KUBECTL_WAIT_TIMEOUT)

##@ Install Development Tools

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

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
bin/controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

KUSTOMIZE = $(shell pwd)/bin/kustomize
bin/kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

KIND = $(shell pwd)/bin/kind
bin/kind: ## Download kind locally if necessary.
	$(call go-get-tool,$(KIND),sigs.k8s.io/kind@v0.11.1)

GINKGO = $(shell pwd)/bin/ginkgo
bin/ginkgo: ## Download ginkgo locally if necessary.
	$(call go-get-tool,$(GINKGO),github.com/onsi/ginkgo/ginkgo@v1.16.4)

KUBECTL = $(shell pwd)/bin/kubectl
bin/kubectl: ## Download kubectl locally if necessary.
	curl --create-dirs -o $(KUBECTL) -sfL https://storage.googleapis.com/kubernetes-release/release/$(shell curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/$(shell uname -s | awk '{print tolower($0)}')/amd64/kubectl
	chmod a+x $(KUBECTL)

##@ Kind Cluster Management
KIND_CLUSTER_NAME ?= fluent-pvc-operator
TEST_KUBERNETES_VERSION ?= 1.20
ifeq ($(TEST_KUBERNETES_VERSION),1.20)
	KUBERNETES_VERSION := 1.20.7
else ifeq ($(TEST_KUBERNETES_VERSION),1.19)
	KUBERNETES_VERSION := 1.19.11
else ifeq ($(TEST_KUBERNETES_VERSION),1.18)
	KUBERNETES_VERSION := 1.18.19
endif
kind-create-cluster: bin/kind kind-delete-cluster ## Launch a k8s cluster by kind.
	$(KIND) create cluster --name=$(KIND_CLUSTER_NAME) --image kindest/node:v$(KUBERNETES_VERSION)

kind-delete-cluster: bin/kind ## Shutdown the k8s cluster by kind.
	$(KIND) delete cluster --name=$(KIND_CLUSTER_NAME) || true

kind-load-image-fluent-pvc-operator: ## Load the fluent-pvc-operator docker image into the k8s cluster launched by kind.
	$(MAKE) .kind-load-image-$(IMG)

.kind-load-image-%: bin/kind # NOTE: A hidden utility target to load docker images into the k8s cluster launched by kind.
	$(KIND) load docker-image --name $(KIND_CLUSTER_NAME) ${@:.kind-load-image-%=%}

##@ E2E Test
e2e/setup: cert-manager kind-load-image-fluent-pvc-operator fluent-pvc-operator ## Setup the k8s cluster specified in ~/.kube/config for the e2e tests.
e2e/clean-setup: kind-create-cluster e2e/setup ## Re-create the k8s cluster && Setup the k8s cluster specified in ~/.kube/config for the e2e tests.
e2e/test: ## Run the e2e tests in the k8s cluster specified in ~/.kube/config.
	go test -timeout 1800s ./e2e -coverprofile cover-e2e.out
e2e/clean-test: e2e/clean-setup e2e/test ## Run the e2e tests with relaunching the k8s cluster.

##@ Example Log Collection (User Defined Commands)
EXAMPLE_LOG_COLLECTION_DIR = $(shell pwd)/examples/log-collection
EXAMPLE_LOG_COLLECTION_IMG_PREFIX ?= fluent-pvc-operator-
EXAMPLE_LOG_COLLECTION_IMAGES = fluentd gcloud-pubsub-emulator sample-app
examples/log-collection/build: $(addprefix examples/log-collection/build-, $(EXAMPLE_LOG_COLLECTION_IMAGES)) ## Build all images in examples/log-collection.
examples/log-collection/build-%: ## Build an image in examples/log-collection
	cd $(EXAMPLE_LOG_COLLECTION_DIR)/${@:examples/log-collection/build-%=%} \
		&& docker build -t $(EXAMPLE_LOG_COLLECTION_IMG_PREFIX)${@:examples/log-collection/build-%=%}:development .
examples/log-collection/kind-load-image: $(addprefix examples/log-collection/kind-load-image-, $(EXAMPLE_LOG_COLLECTION_IMAGES)) ## Load all images in examples/log-collection into the k8s cluster launched by kind.
examples/log-collection/kind-load-image-%: ## Load an image in examples/log-collection into the k8s cluster launched by kind.
	$(MAKE) .kind-load-image-$(EXAMPLE_LOG_COLLECTION_IMG_PREFIX)${@:examples/log-collection/kind-load-image-%=%}:development

examples/log-collection/clean-deploy: e2e/clean-setup examples/log-collection/build examples/log-collection/kind-load-image examples/log-collection/deploy  ## Clean up the k8s cluster launched by kind, then deploy the log collection example.

examples/log-collection/deploy: ## Deploy the log collection example to the k8s cluster specified in ~/.kube/config.
	touch $(EXAMPLE_LOG_COLLECTION_DIR)/manifests/fluentd/credential.json
	$(KUSTOMIZE) build $(EXAMPLE_LOG_COLLECTION_DIR)/manifests | kubectl apply -f -

examples/log-collection/undeploy: ## Undeploy the log collection example from the k8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build $(EXAMPLE_LOG_COLLECTION_DIR)/manifests | kubectl delete -f -

examples/log-collection/show-pubsub-subscription: ## Show fluentd-published logs by subscription.
	kubectl get po -l app=gcloud-pubsub-emulator -o json \
		| jq -r '.items[] | select(.status.phase == "Running") | .metadata.name' \
		| xargs -I%% kubectl exec %% -- ./subscription.sh
