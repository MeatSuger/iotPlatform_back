# ===========================================
# IoT Platform (Go) Makefile
# ===========================================

.PHONY: build dev deps clean push swagger

# 变量
APP_NAME   = iot-platform
BUILD_DIR  = ./build
GO         = /usr/local/go/bin/go
SSH_HOST  ?= aliyun-ubuntu
REMOTE_DIR ?= /home/ubuntu/iotPlatform_back
REMOTE_BIN ?= $(REMOTE_DIR)/build/$(APP_NAME)

VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
TIMESTAMP := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

LDFLAGS    = -s -w \
             -X 'main.Version=$(VERSION)' \
             -X 'main.BuildTime=$(TIMESTAMP)'
GOFLAGS    = -ldflags="$(LDFLAGS)" -trimpath

# ===========================================
# 开发
# ===========================================

dev:
	@bash ./scripts/dev.sh

# ===========================================
# 编译
# ===========================================

build:
	@echo "→ 格式化..."
	$(GO) fmt ./...
	@echo "→ 静态分析 (vet)..."
	$(GO) vet ./...
	@echo "→ 生成 Ent 代码..."
	$(GO) generate ./internal/ent
	@echo "→ 测试..."
	$(GO) test -count=1 ./...
	@echo "→ 编译 Linux amd64 (static)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/iot-platform
	@ls -lh $(BUILD_DIR)/$(APP_NAME)
	@echo "✓ 编译完成"

# ===========================================
# 依赖
# ===========================================

deps:
	$(GO) mod tidy
	$(GO) mod download

# ===========================================
# 清理
# ===========================================

clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# ===========================================
# Swagger
# ===========================================

swagger:
	cd cmd/iot-platform && swag init -d . -g main.go --output ../../api/swagger

# ===========================================
# 推送
# ===========================================

push: build
	@echo "→ 上传到 $(SSH_HOST):$(REMOTE_DIR)..."
	@echo '  (1/3) 上传二进制...'
	scp $(BUILD_DIR)/$(APP_NAME) $(SSH_HOST):$(REMOTE_BIN).new || { echo "✗ 二进制上传失败"; exit 1; }
	@echo '  (2/3) 同步配置...'
	ssh $(SSH_HOST) 'mkdir -p $(REMOTE_DIR)/configs'
	scp configs/config.yaml configs/config.prod.yaml $(SSH_HOST):$(REMOTE_DIR)/configs/ || { echo "✗ 配置同步失败"; exit 1; }
	@echo '  (3/3) 替换二进制...'
	ssh $(SSH_HOST) 'mv $(REMOTE_BIN).new $(REMOTE_BIN) && chmod +x $(REMOTE_BIN)' || { echo "✗ 替换失败"; exit 1; }
	@echo "✓ 推送完成"

# ===========================================
# 部署
# ===========================================

deploy: push
	@echo "→ 上传部署文件..."
	ssh $(SSH_HOST) 'mkdir -p $(REMOTE_DIR)/deployments'
	scp -r deployments/app deployments/mqtt deployments/sql $(SSH_HOST):$(REMOTE_DIR)/deployments/ || { echo "✗ 部署文件上传失败"; exit 1; }
	@echo "✓ 部署文件已同步"
