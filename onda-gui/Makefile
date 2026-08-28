# Onda GUI — Makefile
# ============================================================================
# La imagen onda-gui empaqueta el frontend Svelte, por lo que usa GUI_VERSION
# (tag gui-vX.Y.Z). Si no existe, fallback al tag más reciente.
GUI_VERSION := $(shell git describe --tags --match 'gui-*' --abbrev=0 2>/dev/null || git describe --tags --abbrev=0 2>/dev/null || echo unknown)
PROJECT_DIR ?= $(shell cd .. && pwd)
INPUT_DIR ?= $(PROJECT_DIR)/input
OUTPUT_DIR ?= $(PROJECT_DIR)/output

.PHONY: build deploy rebuild

build:
	docker build --build-arg GUI_VERSION=$(GUI_VERSION) -t onda-gui .

deploy:
	docker stop onda-gui 2>/dev/null || true
	docker rm onda-gui 2>/dev/null || true
	docker run -d --name onda-gui --network host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(INPUT_DIR):/input \
		-v $(OUTPUT_DIR):/output \
		onda-gui

rebuild: build deploy
