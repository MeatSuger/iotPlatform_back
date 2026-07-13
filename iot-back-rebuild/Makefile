# ===========================================
# IoT Platform (Go) Makefile
# ===========================================

.PHONY: run build build-win build-linux test test-cover lint fmt deps \
        clean \
        run-prod dev gen-wire gen-swagger check

# 变量
APP_NAME=iot-platform
BUILD_DIR=./build
GO=go
GOFLAGS=-ldflags="-s -w"

# ===========================================
# 开发
# ===========================================

# 开发运行
run:
	$(GO) run .

# 运行（带生产配置）
run-prod:
	CONFIG_PATH=config/config.prod.yaml $(GO) run .

# 开发模式（热重载需要 air 工具）
dev:
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

# 运行测试
test:
	@echo "→ 运行测试..."
	$(GO) test -v ./...

# 运行测试（带覆盖率）
test-cover:
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "覆盖率报告: coverage.html"

# 代码 Lint（需要 golangci-lint）
lint:
	golangci-lint run ./...

# ===========================================
# 编译（先检查再打包）
# ===========================================

# 编译当前平台（先 fmt → vet → test，通过后打包）
build: check
	@echo "→ 编译..."
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME) .
	@echo "编译完成: $(BUILD_DIR)/$(APP_NAME)"

# 编译 Windows 版本
build-win: check
	@echo "→ 交叉编译 Windows amd64..."
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME).exe .
	@echo "编译完成: $(BUILD_DIR)/$(APP_NAME).exe"

# 编译 Linux 版本（静态链接）
build-linux: check
	@echo "→ 交叉编译 Linux amd64 (static)..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME) .
	@echo "编译完成: $(BUILD_DIR)/$(APP_NAME)"

# ===========================================
# 代码生成
# ===========================================

# 生成 Wire 依赖注入代码
gen-wire:
	@echo "→ 生成 Wire 代码..."
	wire

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
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
