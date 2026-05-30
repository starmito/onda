# Onda — Makefile raíz
# ============================================================================
# Uso rápido:
#   make setup     — primer inicio: detecta GPU, crea .env y directorios
	@echo "  make download-models  Descarga los modelos (~3.2 GB)"
#   make build     — construye imágenes Docker
#   make up        — levanta los contenedores
#   make test      — prueba el pipeline con un audio de test
#   make down      — para los contenedores
#   make validate  — validación pre-build (sintaxis, archivos, anti-patrones)
# ============================================================================

.DEFAULT_GOAL := help

# ── Detect GPU ──────────────────────────────────
HAS_NVIDIA := $(shell command -v nvidia-smi >/dev/null 2>&1 && echo 1 || echo 0)
HAS_AMD := $(shell ls /dev/kfd >/dev/null 2>&1 && echo 1 || echo 0)
GPU_TYPE ?= cpu

# ── Paths ───────────────────────────────────────
MODEL_DIR ?= ./models
INPUT_DIR := ./input
OUTPUT_DIR := ./output

# ── File lists for GPU detection ────────────────
COMPOSE_FILES := -f docker-compose.yml
ifeq ($(GPU_TYPE),nvidia)
  COMPOSE_FILES += -f docker-compose.nvidia.yml
else ifeq ($(GPU_TYPE),amd)
  COMPOSE_FILES += -f docker-compose.amd.yml
endif

# ── Colors ──────────────────────────────────────
GREEN  := \033[0;32m
YELLOW := \033[1;33m
CYAN   := \033[0;36m
NC     := \033[0m

# ============================================================================
# Targets
# ============================================================================

help: ## Muestra esta ayuda
	@echo "$(CYAN)Onda — Audio Separation Pipeline$(NC)"
	@echo ""
	@echo "$(GREEN)Primer despliegue:$(NC)"
	@echo "  make setup    Detecta GPU, crea .env y directorios"

	@echo "  make download-models  Descarga los modelos (~3.2 GB)"
	@echo "  make download-models  Descarga los modelos (~3.2 GB)"
	@echo "  make build    Construye las imágenes Docker"
	@echo "  make up       Levanta los contenedores"
	@echo ""
	@echo "$(GREEN)Día a día:$(NC)"
	@echo "  make test     Prueba el pipeline con audio de test"
	@echo "  make down     Para los contenedores"
	@echo "  make logs     Muestra los logs"
	@echo ""
	@echo "$(GREEN)Desarrollo:$(NC)"
	@echo "  make validate  Validación pre-build"
	@echo "  make clean     Limpia todo (contenedores + imágenes + outputs)"

setup: ## Configuración inicial: detecta GPU, crea .env y directorios
	@echo "$(CYAN)⚙️  Onda — Configuración inicial$(NC)"
	@echo ""
	@# Detect GPU
	@if [ "$(HAS_NVIDIA)" = "1" ]; then \
		echo "  ✅ NVIDIA GPU detectada (nvidia-smi)"; \
	elif [ "$(HAS_AMD)" = "1" ]; then \
		echo "  ✅ AMD GPU detectada (/dev/kfd)"; \
	else \
		echo "  ⚠️  No se detectó GPU — usando CPU"; \
	fi
	@# Create .env from example
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		if [ "$(HAS_NVIDIA)" = "1" ]; then \
			sed -i 's/GPU_TYPE=cpu/GPU_TYPE=nvidia/' .env; \
		elif [ "$(HAS_AMD)" = "1" ]; then \
			sed -i 's/GPU_TYPE=cpu/GPU_TYPE=amd/' .env; \
		fi; \
		echo "  ✅ .env creado desde .env.example"; \
	else \
		echo "  ⚠️  .env ya existe — no se sobrescribe"; \
	fi
	@# Create directories
	@mkdir -p "$(INPUT_DIR)" "$(OUTPUT_DIR)" "$(MODEL_DIR)"
	@echo "  ✅ Directorios creados: $(INPUT_DIR)/ $(OUTPUT_DIR)/ $(MODEL_DIR)/"
	@# Validate
	@if [ -f scripts/validate.sh ]; then \
		bash scripts/validate.sh || true; \
	fi
	@echo ""
	@echo "$(GREEN)✅ Setup completo.$(NC)"
	@echo ""
	@echo "  Siguientes pasos:"
	@echo "    1. Pon tus modelos en $(MODEL_DIR)/"
	@echo "       Estructura esperada:"
	@echo "       $(MODEL_DIR)/VR_Models/BS_Roformer_Viperx/"
	@echo "       $(MODEL_DIR)/Demucs_ONNX/ (opcional)"
	@echo "  make download-models  Descarga los modelos (~3.2 GB)"
	@echo "    2. make build"
	@echo "    3. make up"
	@echo "    4. Abre http://localhost:3000"

build: ## Construye las imágenes Docker
	@echo "$(CYAN)🔨 Construyendo imágenes...$(NC)"
	docker compose $(COMPOSE_FILES) build
	@echo "$(GREEN)✅ Build completo$(NC)"

up: ## Levanta los contenedores
	@echo "$(CYAN)🚀 Iniciando Onda...$(NC)"
	docker compose $(COMPOSE_FILES) up -d
	@echo "$(GREEN)✅ Onda en http://localhost:3000$(NC)"

down: ## Para los contenedores
	@echo "$(CYAN)⏹️  Parando Onda...$(NC)"
	docker compose $(COMPOSE_FILES) down
	@echo "$(GREEN)✅ Contenedores parados$(NC)"

test: ## Prueba el pipeline con el audio e2e_test.wav
	@echo "$(CYAN)🧪 Probando pipeline...$(NC)"
	@if [ ! -f "$(INPUT_DIR)/e2e_test.wav" ]; then \
		echo "  ⚠️  $(INPUT_DIR)/e2e_test.wav no encontrado"; \
		echo "  Pon un archivo de audio en $(INPUT_DIR)/e2e_test.wav"; \
		exit 1; \
	fi
	docker exec onda /app/pipeline.sh --viperx /input/e2e_test.wav
	@echo "$(GREEN)✅ Test completado — revisa $(OUTPUT_DIR)/e2e_test/$(NC)"

download-models: ## Descarga los modelos desde HuggingFace	@bash scripts/download-models.sh all

validate: ## Ejecuta la validación pre-build
	bash scripts/validate.sh

logs: ## Muestra los logs de los contenedores
	docker compose $(COMPOSE_FILES) logs -f --tail=50

clean: ## Limpia todo (¡peligroso!)
	@echo "$(YELLOW)⚠️  Esto borrará contenedores, imágenes y outputs.$(NC)"
	@read -p "¿Confirmar? [y/N] " yn; \
	if [ "$$yn" = "y" ]; then \
		docker compose $(COMPOSE_FILES) down -v --rmi all 2>/dev/null || true; \
		rm -rf $(OUTPUT_DIR)/*; \
		echo "$(GREEN)✅ Limpieza completa$(NC)"; \
	else \
		echo "Cancelado."; \
	fi

.PHONY: help setup build up down test validate logs clean
