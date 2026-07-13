# ===========================================
# IoT Platform (Go) Makefile
# ===========================================

.PHONY: run build build-win build-linux test test-cover lint fmt deps \
        clean \
        run-prod dev gen-wire gen-swagger check \
        build-all build-fast package

# 变量
APP_NAME   = iot-platform
BUILD_DIR  = ./build
DIST_DIR   = ./dist
GO         = go
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
TIMESTAMP := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

# 编译优化参数
LDFLAGS    = -s -w \
             -X 'main.Version=$(VERSION)' \
             -X 'main.BuildTime=$(TIMESTAMP)'
GOFLAGS    = -ldflags="$(LDFLAGS)" -trimpath

# ===========================================
# 开发
# ===========================================

# 开发运行
run:
	$(GO) run .

# 运行（带生产配置）
run-prod:
	CONFIG_PATH=config/config.prod.yaml $(GO) run .

# 开发模式（热重载需要 air 工具，自动启动隧道）
dev:
	@echo "→ 启动 sshuttle 隧道..."
	@bash ./tunnel.sh on
	@echo "→ 启动 air 热重载..."
	air -c .air.toml

# ===========================================
# 质量检查（编译前置）
# ===========================================

# 全量检查：格式化 → 静态分析 → 测试 → 编译
check: fmt vet test
	@echo "========== 检查全部通过 =========="

# 格式化代码
fmt:
	@echo "→ 格式化代码..."
	$(GO) fmt ./...

# 静态分析
vet:
	@echo "→ 静态分析..."
	$(GO) vet ./...

# 运行测试（禁用缓存）
test:
	@echo "→ 运行测试..."
	$(GO) test -v -count=1 ./...

# 运行测试（带覆盖率）
test-cover:
	$(GO) test -v -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "覆盖率报告: coverage.html"

# 代码 Lint（需要 golangci-lint）
lint:
	golangci-lint run ./...

# ===========================================
# 编译
# ===========================================

# 快速编译（跳过检查，仅编译当前平台）
build-fast:
	@echo "→ 快速编译..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME) .
	@ls -lh $(BUILD_DIR)/$(APP_NAME)

# 编译当前平台（先检查再打包）
build: check
	@echo "→ 编译 $(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME) .
	@ls -lh $(BUILD_DIR)/$(APP_NAME)

# 编译 Linux 版本（静态链接 + 编译优化）
build-linux: check
	@echo "→ 交叉编译 Linux amd64 静态链接..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME) .
	@ls -lh $(BUILD_DIR)/$(APP_NAME)

# 编译 Windows 版本
build-win: check
	@echo "→ 交叉编译 Windows amd64..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME).exe .
	@ls -lh $(BUILD_DIR)/$(APP_NAME).exe

# 全平台编译
build-all: check
	@echo "→ 编译全平台..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux-arm64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME).exe .
	@ls -lh $(BUILD_DIR)/

# ===========================================
# 打包发布
# ===========================================

# 打包当前平台（编译 + tar.gz）
package: build
	@echo "→ 打包..."
	@mkdir -p $(DIST_DIR)
	tar -czf $(DIST_DIR)/$(APP_NAME)-$(VERSION)-$(shell go env GOOS)-$(shell go env GOARCH).tar.gz \
		-C $(BUILD_DIR) $(APP_NAME) \
		-C .. config/
	@ls -lh $(DIST_DIR)/

# 打包 Linux amd64 发布包
package-linux: build-linux
	@echo "→ 打包 Linux amd64..."
	@mkdir -p $(DIST_DIR)
	tar -czf $(DIST_DIR)/$(APP_NAME)-$(VERSION)-linux-amd64.tar.gz \
		-C $(BUILD_DIR) $(APP_NAME) \
		-C .. config/
	@cp config/config.yaml $(DIST_DIR)/config.yaml 2>/dev/null || true
	@ls -lh $(DIST_DIR)/

# 全平台打包
package-all: build-all
	@echo "→ 全平台打包..."
	@mkdir -p $(DIST_DIR)
	@for bin in $(BUILD_DIR)/$(APP_NAME)-* $(BUILD_DIR)/$(APP_NAME).exe; do \
		base=$$(basename $$bin); \
		tar -czf $(DIST_DIR)/$$base-$(VERSION).tar.gz -C $(BUILD_DIR) $$base -C .. config/; \
	done
	@ls -lh $(DIST_DIR)/

# ===========================================
# 代码生成
# ===========================================

# 生成 Wire 依赖注入代码
gen-wire:
	@echo "→ 生成 Wire 代码..."
	wire ./...

# 生成 Swagger API 文档
gen-swagger:
	@echo "→ 生成 Swagger 文档..."
	swag init -g main.go --output docs/

# ===========================================
# 依赖和清理
# ===========================================

# 依赖管理
deps:
	$(GO) mod tidy
	$(GO) mod download

# 清理
clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)
	rm -f coverage.out coverage.html
