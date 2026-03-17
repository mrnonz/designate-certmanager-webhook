IMAGE_NAME := "ghcr.io/stackitcloud/designate-certmanager-webhook"
IMAGE_TAG  ?= $(shell git describe --tags --always --dirty)

# Multi-arch build (x86/amd64 + arm64)
MULTIARCH_IMAGE ?= ghcr.io/mrnonz/designate-certmanager-webhook
MULTIARCH_TAG   ?= v0.5.0
PLATFORMS       := linux/amd64,linux/arm64

build:
	docker build -t "$(IMAGE_NAME):$(IMAGE_TAG)" .

# Build for amd64 and arm64 and push to registry (requires docker buildx)
docker-build-multiarch:
	docker buildx build --platform $(PLATFORMS) \
		-t $(MULTIARCH_IMAGE):$(MULTIARCH_TAG) \
		--push \
		.

# Build for current platform only (no push). Use docker-build-multiarch to push both arches.
docker-build-multiarch-local:
	docker build -t $(MULTIARCH_IMAGE):$(MULTIARCH_TAG) .

check:
	@if test -n "$$(find . -not \( \( -wholename "./vendor" \) -prune \) -name "*.go" | xargs gofmt -l)"; then \
		find . -not \( \( -wholename "./vendor" \) -prune \) -name "*.go" | xargs gofmt -d; \
		exit 1; \
	fi
	go build .

test:
	docker build --file Dockerfile_test . -t $(IMAGE_NAME)-test
	docker run --rm -v $$(pwd):/workspace \
		 -e TEST_ZONE_NAME=$$TEST_ZONE_NAME \
		 -e OS_TENANT_NAME=$$OS_TENANT_NAME \
		 -e OS_TENANT_ID=$$OS_PROJECT_ID \
		 -e OS_DOMAIN_NAME=$$OS_DOMAIN_NAME \
		 -e OS_USERNAME=$$OS_USERNAME \
		 -e OS_PASSWORD=$$OS_PASSWORD \
		 -e OS_AUTH_URL=$$OS_AUTH_URL \
		 -e OS_REGION_NAME=$$OS_REGION_NAME \
	     $(IMAGE_NAME)-test go test -v .

ci-push:
	echo "$$DOCKER_PASSWORD" | docker login -u "$$DOCKER_USERNAME" --password-stdin
	docker push "$(IMAGE_NAME):$(IMAGE_TAG)"
