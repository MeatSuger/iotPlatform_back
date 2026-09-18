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
#   push    -> install  (安装到目标机: 推送二进制)
#   deploy  -> install  (install 的语义别名)
#   restart -> 重建容器 (不重新上传, 仅用当前远程二进制 force-recreate + 健康检查)
#   rollback -> 回滚到上次部署的二进制 (.prev)
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
GO_BUILD_FLAGS = -ldflags="$(LDFLAGS)" -trimpath $(GOFLAGS) -tags sonic

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

# ------------------------------------------------------------
# 部署校验参数
# ------------------------------------------------------------
# compose 项目目录与文件名 (容器在此目录下由 docker compose 管理)
COMPOSE_DIR  ?= $(REMOTE_DIR)/deployments/app
COMPOSE_FILE ?= docker-compose.yml
# 容器名 (compose service 名为 $(PACKAGE)-go)
APP_CONTAINER ?= $(PACKAGE)-go
# 健康检查: 在容器内 curl，避开宿主端口映射差异
HEALTH_URL    ?= http://localhost:9091/health
HEALTH_RETRY  ?= 15
HEALTH_DELAY  ?= 2

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
        version help deploy restart rollback health-check status

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
	@printf '$(C_CYAN)→ 生成 Ent 代码...$(C_RESET)\n'
	$(GO) generate ./internal/ent
	@printf '$(C_CYAN)→ 静态分析 (vet)...$(C_RESET)\n'
	$(GO) vet ./...
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
#
# 部署流程（每步失败即停，不留半成品）:
#   1. 确保远程目录存在        —— 目录被误删时不再直接失败
#   2. 备份当前远程二进制      —— 留下回滚点 .prev
#   3. 并行上传: 二进制 + 部署文件
#   4. 原子替换远程二进制 (mv 重命名，可替换正在被 bind mount 占用的文件；
#      注意 cp 覆盖会因 ETXTBSY 失败，必须 mv)
#   5. 重建容器 up -d --force-recreate
#      ⚠️ 必须 recreate 而非 restart —— 容器对二进制的 bind mount 钉的是
#         旧 inode，只 restart 不会重新绑定，等于没更新
#   6. 健康检查（轮询容器内 /health，失败则提示 make rollback）
# ============================================================
install: all
	@printf '$(C_CYAN)→ [1/6] 确认远程目录...$(C_RESET)\n'
	@ssh $(SSH_HOST) 'mkdir -p $(REMOTE_DIR)/build $(REMOTE_DIR)/configs $(REMOTE_DIR)/deployments' \
		|| { printf '$(C_RED)✗ 远程目录创建失败$(C_RESET)\n'; exit 1; }
	@printf '$(C_CYAN)→ [2/6] 备份当前二进制 → $(PACKAGE).prev$(C_RESET)\n'
	@ssh $(SSH_HOST) 'if [ -f $(REMOTE_DIR)/build/$(PACKAGE) ]; then cp -f $(REMOTE_DIR)/build/$(PACKAGE) $(REMOTE_DIR)/build/$(PACKAGE).prev && echo "  已留回滚点"; else echo "  首次部署，无历史二进制"; fi' \
		|| { printf '$(C_RED)✗ 备份失败$(C_RESET)\n'; exit 1; }
	@printf '$(C_CYAN)→ [3/6] 并行上传 (2 线程): 二进制 + 部署文件...$(C_RESET)\n'
	@$(UPLOAD_TOOL) $(UPLOAD_FLAGS) $(BIN) $(SSH_HOST):$(REMOTE_DIR)/build/$(PACKAGE).new & \
	p1=$$!; \
	$(UPLOAD_TOOL) $(UPLOAD_FLAGS) -R $(DEPLOY_DIRS) $(SSH_HOST):$(REMOTE_DIR)/ & \
	p2=$$!; \
	wait $$p1 || { printf '$(C_RED)✗ 二进制上传失败$(C_RESET)\n'; exit 1; }; \
	wait $$p2 || { printf '$(C_RED)✗ 部署文件上传失败$(C_RESET)\n'; exit 1; }
	@printf '$(C_CYAN)→ [4/6] 原子替换远程二进制...$(C_RESET)\n'
	@ssh $(SSH_HOST) 'cd $(REMOTE_DIR)/build && chmod +x $(PACKAGE).new && mv -f $(PACKAGE).new $(PACKAGE)' \
		|| { printf '$(C_RED)✗ 二进制替换失败$(C_RESET)\n'; exit 1; }
	@printf '$(C_CYAN)→ [5/6] 重建远程容器 (force-recreate)...$(C_RESET)\n'
	@ssh $(SSH_HOST) 'cd $(COMPOSE_DIR) && $(REMOTE_DOCKER) compose -f $(COMPOSE_FILE) up -d --force-recreate' \
		|| { printf '$(C_RED)✗ 容器重建失败 (远程用户需 docker 组或 sudo 权限)$(C_RESET)\n'; exit 1; }
	@printf '$(C_CYAN)→ [6/6] 健康检查: $(HEALTH_URL)$(C_RESET)\n'
	@$(MAKE) --no-print-directory health-check \
		|| { printf '$(C_RED)✗ 健康检查未通过 —— 执行 make rollback 回滚$(C_RESET)\n'; exit 1; }
	@printf '$(C_GREEN)✓ 安装部署完成$(C_RESET)\n'

# deploy: install 的语义别名（见文件头映射表）
deploy: install

# restart: 不重新上传，仅用当前远程二进制重建容器 + 健康检查
restart:
	@printf '$(C_CYAN)→ 重建远程容器 (force-recreate)...$(C_RESET)\n'
	@ssh $(SSH_HOST) 'cd $(COMPOSE_DIR) && $(REMOTE_DOCKER) compose -f $(COMPOSE_FILE) up -d --force-recreate' \
		|| { printf '$(C_RED)✗ 容器重建失败$(C_RESET)\n'; exit 1; }
	@$(MAKE) --no-print-directory health-check
	@printf '$(C_GREEN)✓ 容器已重建$(C_RESET)\n'

# rollback: 回滚到上次 install 自动备份的二进制
rollback:
	@printf '$(C_YELLOW)→ 回滚到 $(PACKAGE).prev ...$(C_RESET)\n'
	@ssh $(SSH_HOST) 'test -f $(REMOTE_DIR)/build/$(PACKAGE).prev' \
		|| { printf '$(C_RED)✗ 无回滚点 ($(PACKAGE).prev 不存在)$(C_RESET)\n'; exit 1; }
	@ssh $(SSH_HOST) 'cd $(REMOTE_DIR)/build && \
		cp -f $(PACKAGE) $(PACKAGE).failed && \
		cp -f $(PACKAGE).prev $(PACKAGE).next && chmod +x $(PACKAGE).next && \
		mv -f $(PACKAGE).next $(PACKAGE) && echo "  已回滚 (失败版本存为 $(PACKAGE).failed)"' \
		|| { printf '$(C_RED)✗ 回滚替换失败$(C_RESET)\n'; exit 1; }
	@ssh $(SSH_HOST) 'cd $(COMPOSE_DIR) && $(REMOTE_DOCKER) compose -f $(COMPOSE_FILE) up -d --force-recreate' >/dev/null \
		|| { printf '$(C_RED)✗ 容器重建失败$(C_RESET)\n'; exit 1; }
	@$(MAKE) --no-print-directory health-check
	@printf '$(C_GREEN)✓ 已回滚$(C_RESET)\n'

# health-check: 轮询容器内 /health，最多 $(HEALTH_RETRY) 次 × $(HEALTH_DELAY)s
health-check:
	@ssh $(SSH_HOST) 'for i in $$(seq 1 $(HEALTH_RETRY)); do \
		sleep $(HEALTH_DELAY); \
		if $(REMOTE_DOCKER) exec $(APP_CONTAINER) curl -s -m 3 $(HEALTH_URL) 2>/dev/null | grep -q "\"status\":\"ok\""; then \
			echo "  ✓ 健康 (第 $$((i * $(HEALTH_DELAY)))s)"; exit 0; \
		fi; \
	done; \
	echo "  ✗ $(HEALTH_RETRY) 次重试后仍未健康，最近日志:"; \
	$(REMOTE_DOCKER) logs --tail 15 $(APP_CONTAINER) 2>&1 | sed "s/^/    /"; \
	exit 1'

# status: 查看远程部署现状（诊断用）
status:
	@printf '$(C_CYAN)远程部署状态 ($(SSH_HOST):$(REMOTE_DIR))$(C_RESET)\n'
	@ssh $(SSH_HOST) 'echo "  容器:    $$($(REMOTE_DOCKER) ps --filter name=$(APP_CONTAINER) --format "{{.Status}}")"; \
		echo "  健康:    $$($(REMOTE_DOCKER) exec $(APP_CONTAINER) curl -s -m 3 $(HEALTH_URL) 2>/dev/null || echo 不可达)"; \
		echo "  二进制:  $$(ls -la $(REMOTE_DIR)/build/$(PACKAGE) 2>/dev/null | awk "{print \$$5\" bytes  \"\$$6\" \"\$$7\" \"\$$8}")"; \
		echo "  回滚点:  $$(ls -la $(REMOTE_DIR)/build/$(PACKAGE).prev 2>/dev/null | awk "{print \$$5\" bytes\"}" || echo 无)"; \
		echo "  目录:    $$(ls $(REMOTE_DIR) 2>/dev/null | tr "\n" " ")"'

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
# 说明: -d 需显式列出含 @Router 注释的目录（含 internal/ 需 --parseInternal，
#       此处直接列出目录更稳妥）；--packageName docs 与现有包名保持一致
# ============================================================
html:
	go run github.com/swaggo/swag/cmd/swag@v1.16.6 init \
	  -d cmd/iot-platform,internal/controller,internal/router,internal/service,internal/model,internal/ent,pkg/common \
	  -g main.go --output api/swagger --packageName docs

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
	@echo "  install          构建 + 推送 + 原子替换 + force-recreate + 健康检查"
	@echo "  deploy           install 的别名"
	@echo "  restart          仅重建容器 (不重新上传) + 健康检查"
	@echo "  rollback         回滚到上次 install 备份的二进制"
	@echo "  status           查看远程部署现状 (容器/健康/二进制/回滚点)"
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
	@printf '$(C_YELLOW)常用变量:$(C_RESET) GO, GOFLAGS, BUILD_DIR, SSH_HOST, REMOTE_DIR, REMOTE_DOCKER, COMPOSE_DIR, UPLOAD_TOOL, UPLOAD_FLAGS, HEALTH_RETRY, HEALTH_DELAY (可在命令行覆盖)\n\n'
	@echo "开发运行: 构建用 make all, 热重载服务器见 scripts/dev.sh"
