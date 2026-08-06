#!/bin/bash
# ============================================================
# 开发启动脚本：SSH 隧道 + air 热重载
# - 按 q 退出（优雅关闭：停止 air + 关闭隧道）
# - Ctrl+C 同样会触发清理（不再残留隧道）
# - air 意外退出时自动关闭隧道
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
TUNNEL="$SCRIPT_DIR/tunnel.sh"
AIR_BIN="${AIR_BIN:-/home/yu/go/bin/air}"
AIR_PID=""

# 清理：停止 air（air 会自动清理其拉起的应用进程）+ 关闭隧道
cleanup() {
    if [ -n "$AIR_PID" ] && kill -0 "$AIR_PID" 2>/dev/null; then
        printf "\n→ 正在停止 air...\n"
        # air 收到 TERM 后会自行杀掉 ./tmp/main 并退出（已验证）
        kill -TERM "$AIR_PID" 2>/dev/null
        for _ in $(seq 1 50); do
            kill -0 "$AIR_PID" 2>/dev/null || break
            sleep 0.1
        done
        # 兜底：air 未响应则强杀其残留子进程
        pkill -KILL -P "$AIR_PID" 2>/dev/null
        kill -KILL "$AIR_PID" 2>/dev/null
        wait "$AIR_PID" 2>/dev/null
    fi

    printf "→ 正在关闭隧道...\n"
    bash "$TUNNEL" off

    printf "%b\n" "\033[0;32m✓ 已退出\033[0m"
    exit 0
}

trap cleanup INT TERM HUP

cd "$ROOT_DIR" || { echo "无法进入项目目录 $ROOT_DIR"; exit 1; }

# 1. 启动隧道（幂等：已在运行则跳过）
bash "$TUNNEL" on || exit 1

# 2. 后台启动 air（stdin 置空，避免与按键监听抢输入）
"$AIR_BIN" -c .air.toml </dev/null &
AIR_PID=$!

# 3. 使用提示
printf "\n%s\n" "=========================================="
printf "%s\n" "  ✦ 按 q 退出   |   Ctrl+C 也可退出"
printf "%s\n" "  （退出时自动关闭 SSH 隧道）"
printf "%s\n" "=========================================="

# 4. 监听键盘；air 意外退出时循环自然结束
while kill -0 "$AIR_PID" 2>/dev/null; do
    if read -r -t 0.5 -n 1 -s key; then
        case "$key" in
            q|Q) cleanup ;;
        esac
    fi
done

# 5. air 自行退出（如构建失败停止），同样关闭隧道
printf "\n⚠ air 已退出，正在关闭隧道...\n"
bash "$TUNNEL" off
exit 0
