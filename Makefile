# Image URL to use all building/pushing image targets
IMG ?= ado-github-migration-operator:latest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.29.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Get the git commit hash
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
# Get the build date
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# Get the version from git tag or default to dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Linker flags for building the binary
LDFLAGS := -X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE) -X main.gitCommit=$(GIT_COMMIT)

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@\' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } \' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter & yamllint
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -ldflags="$(LDFLAGS)" -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host. Use ARGS to pass arguments: make run ARGS="--http-bind-address=:8083"
	go run -ldflags="$(LDFLAGS)" ./cmd/main.go $(ARGS)

.PHONY: ado-dev
ado-dev: ## Run the ADO migration service on port 8083 for local development
	go run -ldflags="$(LDFLAGS)" ./cmd/main.go --http-bind-address=:8083

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/dev-best-practices/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	cd ../../ && $(CONTAINER_TOOL) build --build-arg "VERSION=$(VERSION)" --build-arg "BUILD_DATE=$(BUILD_DATE)" --build-arg "GIT_COMMIT=$(GIT_COMMIT)" -t ${IMG} -f micro-services/ado-to-git-migration/Dockerfile .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have a multi-arch builder. More info: https://docs.docker.com/build/building/multi-platform/
# - be able to push the image to your registry (i.e. if you do not inform a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To properly provided solutions that supports more than one platform you should use this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --build-arg VERSION=$(VERSION) --build-arg BUILD_DATE=$(BUILD_DATE) --build-arg GIT_COMMIT=$(GIT_COMMIT) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.2
CONTROLLER_TOOLS_VERSION ?= v0.18.0
GOLANGCI_LINT_VERSION ?= v1.54.2

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || GOBIN=$(LOCALBIN) GO111MODULE=on go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	test -s $(LOCALBIN)/golangci-lint && $(LOCALBIN)/golangci-lint --version | grep -q $(GOLANGCI_LINT_VERSION) || \
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

##@ Helm

# Helm configuration
HELM_RELEASE ?= ado-migration-operator
HELM_NAMESPACE ?= ado-migration-operator
HELM_CHART ?= charts/ado-git-migration
IMAGE_TAG ?= $(VERSION)

.PHONY: helm-package
helm-package: ## Package Helm chart
	helm package charts/ado-git-migration -d dist/

.PHONY: helm-lint
helm-lint: ## Lint Helm chart
	helm lint charts/ado-git-migration
	helm lint charts/ado-git-migration -f charts/ado-git-migration/values.yaml
	helm lint charts/ado-git-migration -f charts/ado-git-migration/values-prod.yaml

.PHONY: helm-template
helm-template: ## Template Helm chart (default values)
	helm template $(HELM_RELEASE) charts/ado-git-migration

.PHONY: helm-template-dev
helm-template-dev: ## Template Helm chart with dev values
	helm template $(HELM_RELEASE) charts/ado-git-migration -f charts/ado-git-migration/values.yaml --set image.tag=$(IMAGE_TAG)

.PHONY: helm-template-prod
helm-template-prod: ## Template Helm chart with prod values
	helm template $(HELM_RELEASE) charts/ado-git-migration -f charts/ado-git-migration/values-prod.yaml --set image.tag=$(IMAGE_TAG)

.PHONY: helm-install
helm-install: ## Install Helm chart (basic install)
	helm install $(HELM_RELEASE) charts/ado-git-migration --namespace $(HELM_NAMESPACE) --create-namespace

.PHONY: helm-install-dev
helm-install-dev: ## Install Helm chart in development environment
	@echo "Installing Helm chart in development environment..."
	helm install $(HELM_RELEASE) $(HELM_CHART) \
		-f $(HELM_CHART)/values.yaml \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--set image.tag="$(IMAGE_TAG)"

.PHONY: helm-install-prod
helm-install-prod: ## Install Helm chart in production environment
	@echo "Installing Helm chart in production environment..."
	helm install $(HELM_RELEASE) $(HELM_CHART) \
		-f $(HELM_CHART)/values-prod.yaml \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--set image.tag="$(IMAGE_TAG)"

.PHONY: helm-upgrade
helm-upgrade: ## Upgrade Helm chart (basic upgrade)
	helm upgrade $(HELM_RELEASE) charts/ado-git-migration --namespace $(HELM_NAMESPACE)

.PHONY: helm-upgrade-dev
helm-upgrade-dev: ## Upgrade Helm chart in development environment
	@echo "Upgrading Helm chart in development environment..."
	helm upgrade $(HELM_RELEASE) $(HELM_CHART) \
		-f $(HELM_CHART)/values.yaml \
		--namespace $(HELM_NAMESPACE) \
		--install \
		--wait

.PHONY: helm-upgrade-prod
helm-upgrade-prod: ## Upgrade Helm chart in production environment
	@echo "Upgrading Helm chart in production environment..."
	helm upgrade $(HELM_RELEASE) $(HELM_CHART) \
		-f $(HELM_CHART)/values-prod.yaml \
		--namespace $(HELM_NAMESPACE) \
		--install \
		--wait

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall Helm chart
	helm uninstall $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

.PHONY: helm-sync-crds
helm-sync-crds: manifests ## Generate CRDs and sync them to Helm chart
	@echo "Syncing generated CRDs to Helm chart..."
	cp config/crd/bases/migration.ado-to-git-migration.io_adotogitmigrations.yaml charts/ado-git-migration/crds/adotogitmigrations.yaml
	cp config/crd/bases/migration.ado-to-git-migration.io_adodiscoveries.yaml charts/ado-git-migration/crds/adodiscoveries.yaml
	cp config/crd/bases/migration.ado-to-git-migration.io_pipelinetoworkflows.yaml charts/ado-git-migration/crds/pipelinetoworkflows.yaml
	cp config/crd/bases/migration.ado-to-git-migration.io_migrationjobs.yaml charts/ado-git-migration/crds/migrationjobs.yaml
	cp config/crd/bases/migration.ado-to-git-migration.io_workitemmigrations.yaml charts/ado-git-migration/crds/workitemmigrations.yaml
	@echo "✅ CRDs synced successfully!"

.PHONY: helm-diff-dev
helm-diff-dev: ## Show diff for dev upgrade (requires helm-diff plugin)
	@if helm plugin list | grep -q diff; then \
		helm diff upgrade $(HELM_RELEASE) $(HELM_CHART) \
			-f $(HELM_CHART)/values.yaml \
			--namespace $(HELM_NAMESPACE) \
			--set image.tag=$(IMAGE_TAG); \
	else \
		echo "❌ helm-diff plugin not installed. Install with: helm plugin install https://github.com/databus23/helm-diff"; \
	fi

.PHONY: helm-diff-prod
helm-diff-prod: ## Show diff for prod upgrade (requires helm-diff plugin)
	@if helm plugin list | grep -q diff; then \
		helm diff upgrade $(HELM_RELEASE) $(HELM_CHART) \
			-f $(HELM_CHART)/values-prod.yaml \
			--namespace $(HELM_NAMESPACE) \
			--set image.tag=$(IMAGE_TAG); \
	else \
		echo "❌ helm-diff plugin not installed. Install with: helm plugin install https://github.com/databus23/helm-diff"; \
	fi

##@ UI Dashboard

# UI Dashboard configuration
UI_IMG ?= migration-ui-dashboard:latest
UI_HELM_RELEASE ?= migration-dashboard
UI_HELM_NAMESPACE ?= ado-migration-operator
UI_HELM_CHART ?= charts/migration-ui-dashboard

.PHONY: ui-docker-build
ui-docker-build: ## Build UI dashboard Docker image
	@echo "Building UI dashboard Docker image..."
	@if [ -d "ui" ]; then \
		cd ui && $(CONTAINER_TOOL) build -t $(UI_IMG) .; \
	else \
		echo "⚠️  UI directory not found. Skipping UI build."; \
		echo "   Add your UI code to ./ui/ directory"; \
	fi

.PHONY: ui-docker-push
ui-docker-push: ## Push UI dashboard Docker image
	$(CONTAINER_TOOL) push $(UI_IMG)

.PHONY: ui-helm-lint
ui-helm-lint: ## Lint UI dashboard Helm chart
	@echo "Linting UI dashboard Helm chart..."
	helm lint $(UI_HELM_CHART)

.PHONY: ui-helm-template
ui-helm-template: ## Template UI dashboard Helm chart
	@echo "Templating UI dashboard Helm chart..."
	helm template $(UI_HELM_RELEASE) $(UI_HELM_CHART)

.PHONY: ui-helm-install
ui-helm-install: ## Install UI dashboard Helm chart
	@echo "Installing UI dashboard..."
	helm install $(UI_HELM_RELEASE) $(UI_HELM_CHART) \
		--namespace $(UI_HELM_NAMESPACE) \
		--create-namespace \
		--set image.repository=$(subst :latest,,$(UI_IMG)) \
		--set image.tag=$(if $(findstring :,$(UI_IMG)),$(lastword $(subst :, ,$(UI_IMG))),latest)

.PHONY: ui-helm-upgrade
ui-helm-upgrade: ## Upgrade UI dashboard Helm chart
	@echo "Upgrading UI dashboard..."
	helm upgrade $(UI_HELM_RELEASE) $(UI_HELM_CHART) \
		--namespace $(UI_HELM_NAMESPACE) \
		--install \
		--wait \
		--set image.repository=$(subst :latest,,$(UI_IMG)) \
		--set image.tag=$(if $(findstring :,$(UI_IMG)),$(lastword $(subst :, ,$(UI_IMG))),latest)

.PHONY: ui-helm-uninstall
ui-helm-uninstall: ## Uninstall UI dashboard Helm chart
	@echo "Uninstalling UI dashboard..."
	helm uninstall $(UI_HELM_RELEASE) --namespace $(UI_HELM_NAMESPACE)

.PHONY: ui-port-forward
ui-port-forward: ## Port forward UI dashboard for local access
	@echo "Port forwarding UI dashboard to http://localhost:8080"
	@echo "Press Ctrl+C to stop"
	kubectl port-forward -n $(UI_HELM_NAMESPACE) svc/$(UI_HELM_RELEASE) 8080:80

.PHONY: ui-logs
ui-logs: ## View UI dashboard logs
	kubectl logs -n $(UI_HELM_NAMESPACE) -l app.kubernetes.io/name=migration-ui-dashboard --tail=100 -f

.PHONY: ui-status
ui-status: ## Check UI dashboard status
	@echo "UI Dashboard Status:"
	@echo "===================="
	@kubectl get pods -n $(UI_HELM_NAMESPACE) -l app.kubernetes.io/name=migration-ui-dashboard
	@echo ""
	@kubectl get svc -n $(UI_HELM_NAMESPACE) $(UI_HELM_RELEASE)

##@ Full Stack Deployment

.PHONY: deploy-all
deploy-all: helm-install ui-helm-install ## Deploy both operator and UI dashboard
	@echo ""
	@echo "✅ Full stack deployed!"
	@echo "Operator: $(HELM_RELEASE) in namespace $(HELM_NAMESPACE)"
	@echo "UI Dashboard: $(UI_HELM_RELEASE) in namespace $(UI_HELM_NAMESPACE)"
	@echo ""
	@echo "Access UI dashboard:"
	@echo "  make ui-port-forward"

.PHONY: upgrade-all
upgrade-all: helm-upgrade ui-helm-upgrade ## Upgrade both operator and UI dashboard
	@echo ""
	@echo "✅ Full stack upgraded!"

.PHONY: uninstall-all
uninstall-all: ui-helm-uninstall helm-uninstall ## Uninstall both operator and UI dashboard
	@echo ""
	@echo "✅ Full stack uninstalled!"

.PHONY: status-all
status-all: ## Check status of operator and UI dashboard
	@echo "=== Operator Status ==="
	@kubectl get pods -n $(HELM_NAMESPACE) -l app.kubernetes.io/name=ado-git-migration
	@echo ""
	@echo "=== UI Dashboard Status ==="
	@kubectl get pods -n $(UI_HELM_NAMESPACE) -l app.kubernetes.io/name=migration-ui-dashboard
	@echo ""
	@echo "=== Migration Jobs ==="
	@kubectl get migrationjobs -n $(HELM_NAMESPACE) 2>/dev/null || echo "No migration jobs found"

##@ Utilities

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/
	rm -rf dist/
	rm -rf cover.out

.PHONY: install-tools
install-tools: controller-gen kustomize envtest golangci-lint ## Install all tools

.PHONY: version
version: ## Display version information
	@echo "Version: $(VERSION)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Git Commit: $(GIT_COMMIT)"