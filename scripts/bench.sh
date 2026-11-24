#!/usr/bin/env bash
# ============================================================
# 服务端本机压测：编译 scripts/bench 交叉二进制 → 传到远端 → 打 127.0.0.1
#
# 为什么不用家宽跑 k6：实测从家宽/热点并发 400 新建连接里有 150 个被本地 NAT
# 丢掉（dial timeout），握手中位数从 94ms 涨到 1s，k6 的 p95/错误率量的是家宽。
# 这个脚本在服务器上跑，链路隔离，数字才是服务端的。
#
# 用法:
#   ./scripts/bench.sh                 # 默认 100 并发 × 每接口 15s
#   ./scripts/bench.sh 363 20          # 363 并发 × 每接口 20s
#   DEVICE_TOKEN=xxx ./scripts/bench.sh 363 20   # 连写接口一起压
# ============================================================
set -euo pipefail

REMOTE="${REMOTE:-aliyun}"
CONC="${1:-100}"
DUR="${2:-15}"
BASE="${BASE:-http://127.0.0.1:9091/api}"
ACCOUNT="${ACCOUNT:-meatsuger}"
PASSWD="${PASSWD:-aaaaaa}"
DEVICE="${DEVICE:-adc0c5}"
DEVICE_TOKEN="${DEVICE_TOKEN:-}"

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BIN=$(mktemp -d)/bench

echo "→ 编译压测器 (linux/amd64)..."
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$BIN" ./scripts/bench)

echo "→ 上传到 $REMOTE:/tmp/bench"
scp -q "$BIN" "$REMOTE:/tmp/bench"
chmod +x "$BIN" 2>/dev/null || true

echo "→ 在服务器本机压 ${BASE}（并发 ${CONC}，每接口 ${DUR}s）"
# 压测器和被测服务同机：给压测器降优先级（nice 19），避免它抢 CPU 把“服务端上限”测成“客户端上限”。
# 代价是客户端自身可能先跑满——看 top 里 bench 的 CPU% 超过 ~150% 就说明是客户端瓶颈了。
ssh "$REMOTE" "nice -n 19 /tmp/bench -base '$BASE' -account '$ACCOUNT' -passwd '$PASSWD' \
  -device '$DEVICE' -device-token '$DEVICE_TOKEN' -c $CONC -d ${DUR}s"

echo
echo "对照：同一时刻服务端日志里的慢请求（>500ms）"
ssh "$REMOTE" "docker logs iot-platform-go --since 10m 2>&1 | grep -c '慢请求' || true"
