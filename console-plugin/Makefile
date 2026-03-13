PLUGIN_IMG ?= quay.io/mooyeg/kea-dhcp-console-plugin:latest
CONTAINER_TOOL ?= docker

.PHONY: install-deps
install-deps: ## Install node dependencies
	npm ci

.PHONY: build-frontend
build-frontend: install-deps ## Build the frontend
	npm run build

.PHONY: build-backend
build-backend: ## Build the Go backend proxy
	CGO_ENABLED=0 GOOS=linux go build -a -o bin/backend ./cmd/backend/

.PHONY: build
build: build-frontend build-backend ## Build frontend and backend

.PHONY: lint
lint: ## Run frontend linter
	npm run lint
	npm run type-check

.PHONY: docker-build
docker-build: ## Build the plugin container image
	$(CONTAINER_TOOL) build -t $(PLUGIN_IMG) -f Dockerfile .

.PHONY: docker-push
docker-push: ## Push the plugin container image
	$(CONTAINER_TOOL) push $(PLUGIN_IMG)

.PHONY: deploy
deploy: ## Deploy the plugin using Helm
	helm upgrade --install kea-dhcp-console-plugin charts/kea-dhcp-console-plugin \
		--namespace openshift-kea-dhcp --create-namespace \
		--set plugin.image=$(PLUGIN_IMG)

.PHONY: undeploy
undeploy: ## Remove the plugin
	helm uninstall kea-dhcp-console-plugin --namespace openshift-kea-dhcp

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
