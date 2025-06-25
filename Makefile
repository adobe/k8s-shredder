.PHONY: default help format lint vet security build build-prereq push unit-test local-test local-test-karpenter local-test-node-labels ci clean e2e-tests check-license helm-docs

NAME ?= adobe/k8s-shredder
K8S_SHREDDER_VERSION ?= "dev"
KINDNODE_VERSION ?= "v1.31.9"
COMMIT ?= $(shell git rev-parse --short HEAD)
TEST_CLUSTERNAME ?= "k8s-shredder-test-cluster"
TEST_CLUSTERNAME_KARPENTER ?= "k8s-shredder-test-cluster-karpenter"
TEST_CLUSTERNAME_NODE_LABELS ?= "k8s-shredder-test-cluster-node-labels"
KUBECONFIG_LOCALTEST ?= "kubeconfig-localtest"
KUBECONFIG_KARPENTER ?= "kubeconfig-local-test-karpenter"
KUBECONFIG_NODE_LABELS ?= "kubeconfig-local-test-node-labels"

GOSEC=gosec -quiet -exclude=G107

default: help

help: ## Print this help text
	@printf "\n"
	@awk 'BEGIN {FS = ":.*?## "}; ($$2 && !/@awk/){printf "${CYAN}%-30s${NC} %s\n", $$1, $$2}' $(lastword ${MAKEFILE_LIST}) | sort
	@printf "\n"

# CI
# -----------
format: helm-docs ## Format go code and YAML files
	@echo "Format go code..."
	@go fmt ./...
	@hash golangci-lint 2>/dev/null && { golangci-lint run --fix ./... ; } || { \
  		echo >&2 "[WARN] I require golangci-lint but it's not installed (see https://github.com/golangci/golangci-lint). Skipping golangci-lint format."; \
  	}
	@hash yamlfix 2>/dev/null && { \
		echo "Format YAML files..."; \
		find . -name "*.yaml" -o -name "*.yml" | grep -v "/templates/" | xargs yamlfix 2>/dev/null || true ; \
		echo "YAML files formatted!" ; \
	} || { \
		echo >&2 "[WARN] I require yamlfix but it's not installed (see https://github.com/lyz-code/yamlfix). Skipping YAML format."; \
	}

lint: ## Lint go code and YAML files
	@hash golangci-lint 2>/dev/null && { \
		echo "Checking go code style..."; \
		echo "Run "make format" in case of failures!"; \
		golangci-lint run -v --timeout 5m --no-config ./... ; \
		echo "Go code style OK!" ; \
	} || { \
		echo >&2 "[WARN] I require golangci-lint but it's not installed (see https://github.com/golangci/golangci-lint). Skipping lint."; \
	}
	@hash yamlfix 2>/dev/null && { \
		echo "Checking YAML files..."; \
		find . -name "*.yaml" -o -name "*.yml" | grep -v "/templates/" | xargs yamlfix --check 2>/dev/null || { \
			echo "YAML files have formatting issues. Run 'make format' to fix them."; \
			exit 1; \
		} ; \
		echo "YAML files OK!" ; \
	} || { \
		echo >&2 "[WARN] I require yamlfix but it's not installed (see https://github.com/lyz-code/yamlfix). Skipping YAML lint."; \
	}
	@hash kubeconform 2>/dev/null && { \
		echo "Validating Kubernetes manifests with kubeconform..."; \
		find internal/testing -name "*.yaml" -o -name "*.yml" | xargs kubeconform -strict -skip CustomResourceDefinition,EC2NodeClass,NodePool,Rollout,Cluster || { \
			echo "Kubeconform found schema errors. Please fix them."; \
			exit 1; \
		} ; \
		echo "Kubeconform validation OK!" ; \
	} || { \
		echo >&2 "[WARN] I require kubeconform but it's not installed (see https://github.com/yannh/kubeconform). Skipping kubeconform lint."; \
	}
	@hash helm-docs 2>/dev/null && { \
		echo "Checking Helm documentation..."; \
		helm-docs --chart-search-root=charts --template-files=README.md.gotmpl --dry-run >/dev/null 2>&1 || { \
			echo "Helm documentation is out of date. Run 'make format' to update it."; \
			exit 1; \
		} ; \
		echo "Helm documentation OK!" ; \
	} || { \
		echo >&2 "[WARN] I require helm-docs but it's not installed (see https://github.com/norwoodj/helm-docs). Skipping Helm documentation lint."; \
	}

vet: ## Vetting go code
	@echo 'Vetting go code and identify subtle source code issues...'
	@go vet ./...
	@echo 'Not issues found in go codebase!'

security: ## Inspects go source code for security problems
	@hash gosec 2>/dev/null && { \
		echo "Checking go source code for security problems..."; \
		$(GOSEC) ./... ; \
		echo "No security problems found in the go codebase!" ; \
	} || { \
		echo >&2 "[WARN] I require gosec but it's not installed (see https://github.com/securego/gosec). Skipping security inspections."; \
	}
check-license: ## Check if all go files have the license header set
	@echo "Checking files for license header"
	@./internal/check_license.sh

helm-docs: ## Generate Helm chart documentation
	@hash helm-docs 2>/dev/null && { \
		echo "Generating Helm chart documentation..."; \
		helm-docs --chart-search-root=charts --template-files=README.md.gotmpl ; \
		echo "Helm documentation generated!" ; \
	} || { \
		echo >&2 "[WARN] I require helm-docs but it's not installed (see https://github.com/norwoodj/helm-docs). Skipping documentation generation."; \
	}

build: check-license lint vet security unit-test ## Builds the local Docker container for development
	@CGO_ENABLED=0 GOOS=linux go build \
    	-ldflags="-s -w -X github.com/adobe/k8s-shredder/cmd.buildVersion=${K8S_SHREDDER_VERSION}-${COMMIT} -X github.com/adobe/k8s-shredder/cmd.gitSHA=${COMMIT} -X github.com/adobe/k8s-shredder/cmd.buildTime=$(date)" \
    	-o k8s-shredder
	@DOCKER_BUILDKIT=1 docker build -t ${NAME}:${K8S_SHREDDER_VERSION} .

# TEST
# -----------
local-test: build ## Test docker image in a kind cluster (with Karpenter drift and node label detection disabled)
	@hash kind 2>/dev/null && { \
		echo "Test docker image in a kind cluster..."; \
		./internal/testing/local_env_prep.sh "${K8S_SHREDDER_VERSION}" "${KINDNODE_VERSION}" "${TEST_CLUSTERNAME}" "${KUBECONFIG_LOCALTEST}" && \
		./internal/testing/cluster_upgrade.sh "${TEST_CLUSTERNAME}" "${KUBECONFIG_LOCALTEST}" || \
		exit 1; \
	} || { \
		echo >&2 "[WARN] I require kind but it's not installed(see https://kind.sigs.k8s.io). Assuming a cluster is already accessible."; \
	}

local-test-karpenter: build ## Test docker image in a kind cluster with Karpenter and drift detection enabled
	@hash kind 2>/dev/null && { \
		echo "Test docker image in a kind cluster with Karpenter..."; \
		./internal/testing/local_env_prep_karpenter.sh "${K8S_SHREDDER_VERSION}" "${KINDNODE_VERSION}" "${TEST_CLUSTERNAME_KARPENTER}" "${KUBECONFIG_KARPENTER}" && \
		./internal/testing/cluster_upgrade_karpenter.sh "${TEST_CLUSTERNAME_KARPENTER}" "${KUBECONFIG_KARPENTER}" || \
		exit 1; \
	} || { \
		echo >&2 "[WARN] I require kind but it's not installed(see https://kind.sigs.k8s.io). Assuming a cluster is already accessible."; \
	}

local-test-node-labels: build ## Test docker image in a kind cluster with node label detection enabled
	@hash kind 2>/dev/null && { \
		echo "Test docker image in a kind cluster with node label detection..."; \
		./internal/testing/local_env_prep_node_labels.sh "${K8S_SHREDDER_VERSION}" "${KINDNODE_VERSION}" "${TEST_CLUSTERNAME_NODE_LABELS}" "${KUBECONFIG_NODE_LABELS}" && \
		./internal/testing/cluster_upgrade_node_labels.sh "${TEST_CLUSTERNAME_NODE_LABELS}" "${KUBECONFIG_NODE_LABELS}" || \
		exit 1; \
	} || { \
		echo >&2 "[WARN] I require kind but it's not installed(see https://kind.sigs.k8s.io). Assuming a cluster is already accessible."; \
	}

unit-test: ## Run unit tests
	@echo "Run unit tests for k8s-shredder..."
	@go test ./pkg/... -coverprofile=


e2e-tests:  ## Run e2e tests for k8s-shredder deployed in a local kind cluster
	@echo "Run e2e tests for k8s-shredder..."
	@if [ -f "${PWD}/${KUBECONFIG_KARPENTER}" ]; then \
		echo "Using Karpenter test cluster configuration..."; \
		KUBECONFIG=${PWD}/${KUBECONFIG_KARPENTER} go test internal/testing/e2e_test.go -v; \
	elif [ -f "${PWD}/${KUBECONFIG_NODE_LABELS}" ]; then \
		echo "Using node labels test cluster configuration..."; \
		KUBECONFIG=${PWD}/${KUBECONFIG_NODE_LABELS} go test internal/testing/e2e_test.go -v; \
	else \
		echo "Using default test cluster configuration..."; \
		KUBECONFIG=${PWD}/${KUBECONFIG_LOCALTEST} go test internal/testing/e2e_test.go -v; \
	fi

# DEMO targets
# -----------
.PHONY: demo.prep demo.run demo.rollback
demo.prep: build ## Setup demo cluster
	echo "Setup demo cluster..."
	./internal/testing/local_env_prep.sh "${K8S_SHREDDER_VERSION}" "${KINDNODE_VERSION}" "${TEST_CLUSTERNAME}"

demo.run: ## Run demo
	./internal/testing/cluster_upgrade.sh "${TEST_CLUSTERNAME}"

demo.rollback: ## Rollback demo
	./internal/testing/rollback_cluster_upgrade.sh "${TEST_CLUSTERNAME}"


ci: local-test e2e-tests clean ## Run CI

# PUBLISH
# -----------
publish: ## Release a new version
	@goreleaser release --clean

# CLEANUP
# -----------
clean: ## Clean up local testing environment
	@echo "Cleaning up your local testing environment..."
	@kind delete cluster --name="${TEST_CLUSTERNAME}" ## > /dev/null 2>&1 || true
	@kind delete cluster --name="${TEST_CLUSTERNAME_KARPENTER}" ## > /dev/null 2>&1 || true
	@kind delete cluster --name="${TEST_CLUSTERNAME_NODE_LABELS}" ## > /dev/null 2>&1 || true
	@echo "Removing all generated files and directories"
	@rm -rf dist/ k8s-shredder kubeconfig ${KUBECONFIG_LOCALTEST} ${KUBECONFIG_KARPENTER} ${KUBECONFIG_NODE_LABELS}
	@echo "Done!"

