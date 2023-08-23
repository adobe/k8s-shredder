.PHONY: default help format lint vet security build build-prereq push unit-test local-test ci clean e2e-tests check-license

NAME ?= adobe/k8s-shredder
K8S_SHREDDER_VERSION ?= "dev"
KINDNODE_VERSION ?= "v1.25.11"
COMMIT ?= $(shell git rev-parse --short HEAD)
TEST_CLUSTERNAME ?= "k8s-shredder-test-cluster"

GOSEC=gosec -quiet -exclude=G107

default: help

help: ## Print this help text
	@printf "\n"
	@awk 'BEGIN {FS = ":.*?## "}; ($$2 && !/@awk/){printf "${CYAN}%-30s${NC} %s\n", $$1, $$2}' $(lastword ${MAKEFILE_LIST}) | sort
	@printf "\n"

# CI
# -----------
format: ## Format go code
	@echo "Format go code..."
	@go fmt ./...
	@hash golangci-lint 2>/dev/null && { golangci-lint run --fix ./... ; } || { \
  		echo >&2 "[WARN] I require golangci-lint but it's not installed (see https://github.com/golangci/golangci-lint). Skipping golangci-lint format."; \
  	}

lint: ## Lint go code
	@hash golangci-lint 2>/dev/null && { \
		echo "Checking go code style..."; \
		echo "Run "make format" in case of failures!"; \
		golangci-lint run -v --timeout 5m --no-config ./... ; \
		echo "Go code style OK!" ; \
	} || { \
		echo >&2 "[WARN] I require golangci-lint but it's not installed (see https://github.com/golangci/golangci-lint). Skipping lint."; \
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

build: check-license lint vet security unit-test ## Builds the local Docker container for development
	@CGO_ENABLED=0 GOOS=linux go build \
    	-ldflags="-s -w -X github.com/adobe/k8s-shredder/cmd.buildVersion=${K8S_SHREDDER_VERSION}-${COMMIT} -X github.com/adobe/k8s-shredder/cmd.gitSHA=${COMMIT} -X github.com/adobe/k8s-shredder/cmd.buildTime=$(date)" \
    	-o k8s-shredder
	@DOCKER_BUILDKIT=1 docker build -t ${NAME}:${K8S_SHREDDER_VERSION} .

# TEST
# -----------
local-test: build ## Test docker image in a kind cluster
	@hash kind 2>/dev/null && { \
		echo "Test docker image in a kind cluster..."; \
		./internal/testing/local_env_prep.sh "${K8S_SHREDDER_VERSION}" "${KINDNODE_VERSION}" "${TEST_CLUSTERNAME}" && \
		./internal/testing/cluster_upgrade.sh "${TEST_CLUSTERNAME}" || \
		exit 1; \
	} || { \
		echo >&2 "[WARN] I require kind but it's not installed(see https://kind.sigs.k8s.io). Assuming a cluster is already accessible."; \
	}

unit-test: ## Run unit tests
	@echo "Run unit tests for k8s-shredder..."
	@go test ./pkg/... -coverprofile=


e2e-tests:  ## Run e2e tests for k8s-shredder deployed in a local kind cluster
	@echo "Run e2e tests for k8s-shredder..."
	@KUBECONFIG=${PWD}/kubeconfig go test internal/testing/e2e_test.go -v

demo.prep: build ## Setup demo cluster
	echo "Setup demo cluster..."
	./internal/testing/local_env_prep.sh "${K8S_SHREDDER_VERSION}" "${KINDNODE_VERSION}" "${TEST_CLUSTERNAME}"

demo.run: ## Run demo
	./internal/testing/cluster_upgrade.sh "${TEST_CLUSTERNAME}"


ci: local-test e2e-tests clean ## Run CI

# PUBLISH
# -----------
publish: ## Release a new version
	@goreleaser release --clean

# CLEANUP
# -----------
clean: ## Clean up local testing environment
	@echo "Cleaning up your local testing environment..."
	@kind delete cluster --name="${TEST_CLUSTERNAME}" > /dev/null 2>&1 || true
	@echo "Removing all generated files and directories"
	@rm -rf dist/ k8s-shredder kubeconfig
	@echo "Done!"

