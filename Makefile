# ============================================================
# IoT Platform (Go) Makefile
#
# 遵循 GNU Coding Standards，只保留 GNU 标准目标。
# 参考: GNU coreutils (Makefile.am/cfg.mk), GNU hello (GNUmakefile)
#
# 功能拆解映射（原自定义目标 -> GNU 标准目标）:
#   deps  -> all      (构建流程的一部分: 依赖整理/下载)
#   build -> all      (GNU 默认构建目标)
#   dev   -> all + check (构建 + 测试验证; 运行脚本保留在 scripts/dev.sh)
#   swagger -> html   (GNU 文档目标: info/dvi/html/ps/pdf)
#   push  -> install  (安装到目标机: 推送二进制)
#   deploy -> install (安装到目标机: 推送 + 部署文件)
#   restart -> install (安装完成后重启服务)
#   release -> dist + install (打包发布 + 安装部署)
# ============================================================

# ------------------------------------------------------------
# 颜色支持 (GNU 惯例: ?= 允许覆盖, USE_COLOR=0 可关闭)
# ------------------------------------------------------------
USE_COLOR ?= 1
ifeq ($(USE_COLOR),1)
  C_RESET  := \033[0m
  C_RED    := \033[0;31m
  C_GREEN  := \033[0;32m
  C_YELLOW := \033[0;33m
  C_CYAN   := \033[0;36m
else
  C_RESET  :=
  C_RED    :=
  C_GREEN  :=
  C_YELLOW :=
  C_CYAN   :=
endif

# ------------------------------------------------------------
# 包信息 (GNU 惯例: PACKAGE / VERSION)
# ------------------------------------------------------------
PACKAGE   := iot-platform
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
TIMESTAMP := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

# ------------------------------------------------------------
# 工具 (GNU 惯例: ?= 允许用户/命令行覆盖)
# ------------------------------------------------------------
GO        ?= go
GOFLAGS   ?=
LDFLAGS    = -s -w \
             -X 'main.Version=$(VERSION)' \
             -X 'main.BuildTime=$(TIMESTAMP)'
GO_BUILD_FLAGS = -ldflags="$(LDFLAGS)" -trimpath $(GOFLAGS)

# ------------------------------------------------------------
# 构建 / 部署目录
# ------------------------------------------------------------
BUILD_DIR  ?= ./build
BIN         = $(BUILD_DIR)/$(PACKAGE)

SSH_HOST   ?= aliyun-ubuntu
REMOTE_DIR ?= /home/ubuntu/iotPlatform_back

# 远程 Docker 命令 (远程用户 ubuntu 不在 docker 组, 需 sudo)
# 可用 make install REMOTE_DOCKER="docker" 覆盖
REMOTE_DOCKER ?= sudo docker

# 上传工具: rsync 支持压缩+增量+断点续传 (本地/远程均需安装 rsync)
# 可用 make install UPLOAD_TOOL=scp UPLOAD_FLAGS="-C" 回退到 scp
UPLOAD_TOOL ?= rsync
UPLOAD_FLAGS ?= -az --partial

# 部署文件目录 (相对项目根, rsync -R 保持路径)
DEPLOY_DIRS ?= configs deployments

# 发布包 (GNU 惯例: PACKAGE-VERSION.tar.gz)
DIST_FILE   = dist/$(PACKAGE)-$(VERSION).tar.gz

# 用户自定义片段 (参考 GNU 项目 cfg.mk 机制)，可覆盖上述变量
-include local.mk

# ------------------------------------------------------------
# 目标声明
# ------------------------------------------------------------
.PHONY: all check check-expensive dev \
        install uninstall \
        clean mostlyclean distclean maintainer-clean \
        html dist distcheck \
        version help

# 默认目标 (GNU 惯例: all)
# 原 deps + build 拆解于此: 依赖整理 -> 格式化 -> vet ->
# 生成代码 -> 测试 -> 编译
# ============================================================
all:
	@printf '$(C_CYAN)→ 整理依赖...$(C_RESET)\n'
	$(GO) mod tidy
	$(GO) mod download
	@printf '$(C_CYAN)→ 格式化...$(C_RESET)\n'
	$(GO) fmt ./...
	@printf '$(C_CYAN)→ 静态分析 (vet)...$(C_RESET)\n'
	$(GO) vet ./...
	@printf '$(C_CYAN)→ 生成 Ent 代码...$(C_RESET)\n'
	$(GO) generate ./internal/ent
	@printf '$(C_CYAN)→ 测试...$(C_RESET)\n'
	$(GO) test -count=1 ./...
	@printf '$(C_CYAN)→ 编译 Linux amd64 (static)...$(C_RESET)\n'
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(GO_BUILD_FLAGS) -o $(BIN) ./cmd/iot-platform
	@ls -lh $(BIN)
	@printf '$(C_GREEN)✓ 编译完成$(C_RESET)\n'

# 本地运行 (自定义目标)
# SSH 隧道 + air 热重载开发服务器，按 q 退出，
# 退出时自动关闭隧道并清理所有端口映射/hosts
# (见 scripts/dev.sh, scripts/tunnel.sh)
dev:
	@bash ./scripts/dev.sh

# ============================================================
# 测试 (GNU: check)
# ============================================================
check:
	$(GO) test -count=1 ./...

# 参考 coreutils: check-expensive 分级测试
check-expensive:
	$(MAKE) check VERBOSE=1

# ============================================================
# 安装/部署 (GNU: install / uninstall)
# 原 push + deploy + restart + release 的部署部分拆解于此:
#   1. 构建并推送二进制
#   2. 打包并上传部署文件 (configs/deployments/sql)
#   3. 重启远程服务
# ============================================================
install: all
	@printf '$(C_CYAN)→ 并行上传 (2 线程): 二进制 + 部署文件...$(C_RESET)\n'
	@$(UPLOAD_TOOL) $(UPLOAD_FLAGS) $(BIN) $(SSH_HOST):$(REMOTE_DIR)/build/$(PACKAGE).new & \
	p1=$$!; \
	$(UPLOAD_TOOL) $(UPLOAD_FLAGS) -R $(DEPLOY_DIRS) $(SSH_HOST):$(REMOTE_DIR)/ & \
	p2=$$!; \
	wait $$p1 || { printf '$(C_RED)✗ 二进制上传失败$(C_RESET)\n'; exit 1; }; \
	wait $$p2 || { printf '$(C_RED)✗ 部署文件上传失败$(C_RESET)\n'; exit 1; }
	@printf '$(C_CYAN)→ 替换远程二进制...$(C_RESET)\n'
	@ssh $(SSH_HOST) 'mv $(REMOTE_DIR)/build/$(PACKAGE).new $(REMOTE_DIR)/build/$(PACKAGE) && chmod +x $(REMOTE_DIR)/build/$(PACKAGE)' || { printf '$(C_RED)✗ 二进制替换失败$(C_RESET)\n'; exit 1; }
	@printf '$(C_CYAN)→ 重启远程 Docker 容器...$(C_RESET)\n'
	@ssh $(SSH_HOST) 'cd $(REMOTE_DIR) && $(REMOTE_DOCKER) compose -f deployments/app/docker-compose.yml restart' || { printf '$(C_RED)✗ 容器重启失败 (远程用户需 docker 组或 sudo 权限)$(C_RESET)\n'; exit 1; }
	@printf '$(C_GREEN)✓ 安装部署完成$(C_RESET)\n'

uninstall:
	@printf '$(C_CYAN)→ 删除远程二进制...$(C_RESET)\n'
	ssh $(SSH_HOST) 'rm -f $(REMOTE_DIR)/build/$(PACKAGE)' || { printf '$(C_RED)✗ 卸载失败$(C_RESET)\n'; exit 1; }
	@printf '$(C_GREEN)✓ 已卸载远程二进制$(C_RESET)\n'

# ============================================================
# 清理 (GNU: mostlyclean/clean/distclean/maintainer-clean)
#   mostlyclean      删除编译产物
#   clean            在 mostlyclean 基础上删测试/覆盖文件
#   distclean        在 clean 基础上删打包产物
#   maintainer-clean 在 distclean 基础上删生成文件
# ============================================================
mostlyclean:
	rm -rf $(BUILD_DIR)

clean: mostlyclean
	rm -f coverage.out coverage.html

distclean: clean
	rm -rf dist

maintainer-clean: distclean
	rm -f .version

# ============================================================
# 文档 (GNU: info/dvi/html/ps/pdf)
# 原 swagger 拆解于此: 生成 API 文档 (HTML)
# ============================================================
html:
	cd cmd/iot-platform && swag init -d . -g main.go --output ../../api/swagger

# ============================================================
# 打包 (GNU: dist / distcheck)
# 原 release 的发布包部分拆解于此。
# tarball 含顶层目录 PACKAGE-VERSION/ (GNU tarball 惯例)
# ============================================================
dist:
	@mkdir -p dist
	@tar --exclude='.git' --exclude='$(BUILD_DIR)' --exclude='dist' \
	  --transform='s,^\./,$(PACKAGE)-$(VERSION)/,' \
	  -czf $(DIST_FILE) .
	@ls -lh $(DIST_FILE)
	@printf '$(C_GREEN)✓ 打包完成: $(DIST_FILE)$(C_RESET)\n'

# GNU: distcheck = 打包后在干净环境验证
distcheck: dist
	@printf '$(C_CYAN)→ 解压验证发布包...$(C_RESET)\n'
	@rm -rf /tmp/$(PACKAGE)-distcheck
	@mkdir -p /tmp/$(PACKAGE)-distcheck
	@tar xzf $(DIST_FILE) -C /tmp/$(PACKAGE)-distcheck --strip-components=1
	@cd /tmp/$(PACKAGE)-distcheck && $(GO) build -o /dev/null ./cmd/iot-platform
	@rm -rf /tmp/$(PACKAGE)-distcheck
	@printf '$(C_GREEN)✓ 发布包验证通过$(C_RESET)\n'

# 参考 coreutils: 用临时文件 + 原子 mv 生成 .version
.version:
	@echo "$(VERSION)" > $@-t && mv -f $@-t $@

# ============================================================
# 版本信息
# ============================================================
version:
	@echo "$(PACKAGE) $(VERSION) ($(TIMESTAMP))"

# ============================================================
# 帮助
# ============================================================
help:
	@printf '$(C_CYAN)用法: make [目标]  (GNU 标准目标)$(C_RESET)\n\n'
	@printf '$(C_YELLOW)构建与测试:$(C_RESET)\n'
	@echo "  all              构建 (默认目标; 含依赖整理/下载)"
	@echo "  check            运行测试"
	@echo "  check-expensive  运行测试 (详细输出)"
	@echo "  dev              本地运行: SSH 隧道 + air 热重载, 按 q 退出"
	@printf '$(C_YELLOW)安装/部署:$(C_RESET)\n'
	@echo "  install          构建 + 推送二进制 + 部署文件 + 重启"
	@echo "  uninstall        删除远程二进制"
	@printf '$(C_YELLOW)清理:$(C_RESET)\n'
	@echo "  mostlyclean      删除编译产物"
	@echo "  clean            删除编译产物和覆盖率文件"
	@echo "  distclean        再删除打包产物"
	@echo "  maintainer-clean 再删除生成文件"
	@printf '$(C_YELLOW)文档与打包:$(C_RESET)\n'
	@echo "  html             生成 Swagger API 文档"
	@echo "  dist             打包发布包 dist/$(PACKAGE)-$(VERSION).tar.gz"
	@echo "  distcheck        打包并在干净环境验证"
	@printf '$(C_YELLOW)其他:$(C_RESET)\n'
	@echo "  version          显示版本信息"
	@echo "  help             显示本帮助"
	@printf '$(C_YELLOW)常用变量:$(C_RESET) GO, GOFLAGS, BUILD_DIR, SSH_HOST, REMOTE_DIR, REMOTE_DOCKER, UPLOAD_TOOL, UPLOAD_FLAGS (可在命令行覆盖)\n\n'
	@echo "开发运行 (原 dev): 构建用 make all, 热重载服务器见 scripts/dev.sh"
