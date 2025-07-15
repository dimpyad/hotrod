GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

RELEASE_TAG ?= $(shell git describe --always)
RELEASE_OSES ?= linux
RELEASE_ARCHES ?= amd64 arm64

DOCKER ?= docker
IMAGE_REGISTRY ?= docker.io/dimpyad
IMAGE_NAME ?= hotrod
FULL_IMAGE := $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(RELEASE_TAG)-$(GOOS)-$(GOARCH)

SHELL = /bin/bash
.PHONY: build

build: build-frontend-app
	mkdir -p dist/$(GOOS)/$(GOARCH)/bin
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o dist/$(GOOS)/$(GOARCH)/bin/hotrod ./cmd/hotrod

build-frontend-app:
	cd services/frontend/react_app && ./scripts/build.sh

dev-build-docker: build
	$(DOCKER) build -t $(IMAGE_REGISTRY)/$(IMAGE_NAME):latest \
		--platform $(GOOS)/$(GOARCH) \
		.

build-docker: build
	$(DOCKER) build -t $(FULL_IMAGE) \
		--platform $(GOOS)/$(GOARCH) \
		--provenance false \
		.

push-docker: build-docker
	$(DOCKER) push $(FULL_IMAGE)

build-release:
	for os in $(RELEASE_OSES); do \
		for arch in $(RELEASE_ARCHES); do \
			GOOS=$$os GOARCH=$$arch $(MAKE) build-docker; \
		done; \
	done;

release-images.txt:
	mkdir -p dist
	rm -f dist/release-images.txt
	for os in $(RELEASE_OSES); do \
		for arch in $(RELEASE_ARCHES); do \
			echo $(IMAGE_REGISTRY)/$(IMAGE_NAME):${RELEASE_TAG}-$$os-$$arch >> dist/release-images.txt; \
		done; \
	done;

tag-release:
	./tag-release.sh $(RELEASE_TAG)

release: build-release release-images.txt tag-release
	for os in $(RELEASE_OSES); do \
		for arch in $(RELEASE_ARCHES); do \
			GOOS=$$os GOARCH=$$arch $(MAKE) push-docker; \
		done; \
	done;
	# Remove existing manifest if it exists to ensure a fresh state
	$(DOCKER) manifest rm $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(RELEASE_TAG) || true
	# Create a new manifest with the freshly built images
	$(DOCKER) manifest create $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(RELEASE_TAG) \
		$(shell cat dist/release-images.txt)
	$(DOCKER) manifest push $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(RELEASE_TAG)

generate-proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		services/route/route.proto
