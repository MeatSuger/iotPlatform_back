#!/bin/bash
# ============================================================
# 简化隧道：SSH 端口转发 + hosts，无 SOCKS 代理
# 用法: ./tunnel.sh on | off
# ============================================================

SSH_ALIAS="aliyun"
CTRL_SOCK="/tmp/tunnel.sock"
HOSTS_MARK="# BEGIN_TUNNEL"

# 需要转发的容器名→端口
declare -A CONTAINERS=(
    ["1Panel-postgresql-atvS"]=5432
    ["1Panel-redis-143d"]=6379
    ["1Panel-influxdb-ks5i"]=8086
)

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

# 从远端获取容器 IP
resolve_ip() {
    ssh -o ConnectTimeout=3 -o ServerAliveInterval=2 "$SSH_ALIAS" \
        "docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $1 2>/dev/null"
}

on() {
    if [ -S "$CTRL_SOCK" ]; then
        printf "%b\n" "隧道已在运行"
        return 0
    fi

    printf "%b\n" "→ 建立 SSH 主控连接..."
    ssh -M -S "$CTRL_SOCK" -o ControlPersist=yes -N -f "$SSH_ALIAS" 2>/dev/null
    sleep 2

    if ! [ -S "$CTRL_SOCK" ]; then
        printf "%b\n" "${RED}SSH 连接失败${NC}"
        return 1
    fi
    printf "%b\n" "${GREEN}SSH 已连接${NC}"

    printf "%b\n" "→ 启动端口转发..."
    for container in "${!CONTAINERS[@]}"; do
        port="${CONTAINERS[$container]}"
        ip=$(resolve_ip "$container")
        if [ -z "$ip" ]; then
            printf "%b\n" "  ${RED}跳过 $container:$port（未运行或无法获取IP）${NC}"
            continue
        fi
        ssh -S "$CTRL_SOCK" -O forward -L "${port}:${ip}:${port}" "$SSH_ALIAS" 2>/dev/null
        if [ $? -eq 0 ]; then
            printf "%b\n" "  ${GREEN}127.0.0.1:$port ← $container ($ip)${NC}"
        else
            printf "%b\n" "  ${RED}127.0.0.1:$port 转发失败${NC}"
        fi
    done

    printf "\n%b\n" "${GREEN}===== 隧道就绪 =====${NC}"
    printf "%b\n" "  所有服务 → 127.0.0.1"
    printf "%b\n" "  PostgreSQL: 5432 | Redis: 6379 | InfluxDB: 8086"

    # 写入 hosts
    printf "%b\n" "→ 更新 /etc/hosts..."
    sudo sed -i "/$HOSTS_MARK/,/$HOSTS_MARK/d" /etc/hosts 2>/dev/null
    {
        echo "$HOSTS_MARK"
        for container in "${!CONTAINERS[@]}"; do
            echo "127.0.0.1 $container"
        done
        echo "$HOSTS_MARK"
    } | sudo tee -a /etc/hosts > /dev/null
    grep -A10 "$HOSTS_MARK" /etc/hosts | grep -v "^#" | tail -n+2
}

off() {
    if [ -S "$CTRL_SOCK" ]; then
        ssh -S "$CTRL_SOCK" -O exit "$SSH_ALIAS" 2>/dev/null
    fi
    rm -f "$CTRL_SOCK"
    sudo sed -i "/$HOSTS_MARK/,/$HOSTS_MARK/d" /etc/hosts 2>/dev/null
    printf "%b\n" "${GREEN}隧道已关闭，hosts 已清理${NC}"
}

case "$1" in
    on)  on  ;;
    off) off ;;
    *)   echo "用法: $0 on|off" ;;
esac
