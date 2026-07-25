#!/bin/bash

# ============================================================
# 隧道管理脚本 (SSH 端口转发 + SOCKS5 + hosts)
# 用法: ./tunnel.sh on | off | status
#
# 功能：
#   on  - 建立 SSH 主控连接 → 转发服务端口 → 启动 SOCKS5 → 更新 hosts
#   off - 关闭所有转发、代理、清理 hosts
# ============================================================

SSH_ALIAS="aliyun"
CTRL_SOCK="/tmp/sshuttle_tunnel.sock"
SOCKS_PORT=1080

# 需要转发的容器端口（格式：容器名:端口）
# 这些端口会被转发到本地 127.0.0.1，供 Go 后端直连
FORWARD_PORTS=(
    "1Panel-postgresql-atvS:5432"   # PostgreSQL
    "1Panel-redis-143d:6379"         # Redis
    "1Panel-influxdb-ks5i:8086"      # InfluxDB
    "mqtt:1883"                      # MQTT（预留）
)

# hosts 文件标记
HOSTS_MARK_START="# BEGIN_DOCKER_SSHUTTLE"
HOSTS_MARK_END="# END_DOCKER_SSHUTTLE"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# --------------------------------------------
# 辅助函数
# --------------------------------------------

is_tunnel_running() {
    [ -S "$CTRL_SOCK" ] && ssh -o ControlPath="$CTRL_SOCK" -O check "$SSH_ALIAS" 2>/dev/null
}

require_sudo() {
    if ! sudo -n true 2>/dev/null; then
        echo -e "${YELLOW}需要 sudo 权限（仅用于更新 /etc/hosts），请输入密码：${NC}"
        sudo -v || { echo -e "${YELLOW}无法获取 sudo 权限，将跳过 hosts 更新。${NC}"; return 1; }
    fi
    return 0
}

# 从远端获取容器名→IP 映射
fetch_container_ips() {
    local ssh_cmd="ssh -q -T -o ConnectTimeout=5"
    [ -S "$CTRL_SOCK" ] && ssh_cmd="ssh -S $CTRL_SOCK -q -T"
    $ssh_cmd "$SSH_ALIAS" 2>/dev/null << 'EOF'
docker ps -q 2>/dev/null | xargs -r docker inspect -f '{{.Name}} {{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' 2>/dev/null | awk '$2 != "" && $2 != "<no value>" && $2 !~ /invalid/ {gsub(/^\/*/,"",$1); print $1, $2}'
EOF
}

# 构建需要转发的容器名→IP 对照表
declare -A CONTAINER_IPS

build_container_map() {
    while read -r name ip; do
        CONTAINER_IPS["$name"]="$ip"
    done < <(fetch_container_ips)
}

# 解析容器名→IP（用于端口转发）
resolve_ip() {
    local container="$1"
    echo "${CONTAINER_IPS[$container]}"
}

# 更新 /etc/hosts（容器名 → 127.0.0.1，配合端口转发使用）
update_hosts() {
    echo -e "${GREEN}正在更新 /etc/hosts...${NC}"
    sudo cp /etc/hosts /etc/hosts.bak 2>/dev/null

    # 收集需要转发的容器名
    local names=()
    for entry in "${FORWARD_PORTS[@]}"; do
        local name="${entry%%:*}"
        names+=("$name")
    done

    # 写入 hosts（全部指向 127.0.0.1，因为已通过 SSH -L 转发）
    {
        echo "$HOSTS_MARK_START"
        for name in "${names[@]}"; do
            echo "127.0.0.1    $name"
        done
        echo "$HOSTS_MARK_END"
    } | sudo tee -a /etc/hosts > /dev/null

    echo -e "  ${GREEN}hosts 条目（均指向 127.0.0.1）：${NC}"
    sudo sed -n "/$HOSTS_MARK_START/,/$HOSTS_MARK_END/p" /etc/hosts | grep -v "^#" | sed 's/^/  /'
}

remove_hosts() {
    if sudo grep -q "$HOSTS_MARK_START" /etc/hosts 2>/dev/null; then
        sudo sed -i "/$HOSTS_MARK_START/,/$HOSTS_MARK_END/d" /etc/hosts
        echo -e "  ${GREEN}hosts 已清理${NC}"
    fi
}

# --------------------------------------------
# 核心操作
# --------------------------------------------

start_tunnel() {
    if is_tunnel_running; then
        echo -e "${YELLOW}隧道已在运行中。${NC}"
        show_status
        return 0
    fi

    echo -e "${GREEN}===== 启动隧道 =====${NC}"

    # 1. 建立 SSH 主控连接
    echo -e "${GREEN}[1/4] 建立 SSH 主控连接...${NC}"
    ssh -M -S "$CTRL_SOCK" -o ControlPersist=yes -N -f "$SSH_ALIAS" 2>/dev/null
    sleep 1
    if ! is_tunnel_running; then
        echo -e "${RED}SSH 连接失败，请检查 ~/.ssh/config 中的 aliyun 配置。${NC}"
        return 1
    fi
    echo -e "  ${GREEN}SSH 主控连接已建立${NC}"

    # 2. 获取容器 IP 映射
    echo -e "${GREEN}[2/4] 获取远端容器信息...${NC}"
    build_container_map
    if [ ${#CONTAINER_IPS[@]} -eq 0 ]; then
        echo -e "  ${YELLOW}未获取到容器信息，端口转发将跳过。${NC}"
    else
        echo -e "  ${GREEN}发现 ${#CONTAINER_IPS[@]} 个容器${NC}"
    fi

    # 3. 启动 SSH 端口转发（供 Go 后端直连）
    echo -e "${GREEN}[3/4] 启动服务端口转发...${NC}"
    local forwarded=0
    for entry in "${FORWARD_PORTS[@]}"; do
        local name="${entry%%:*}"
        local port="${entry##*:}"
        local ip="$(resolve_ip "$name")"

        if [ -z "$ip" ]; then
            echo -e "  ${YELLOW}跳过 $name:$port（未在远端运行）${NC}"
            continue
        fi

        # 检查本地端口是否已被占用
        if ss -tlnp 2>/dev/null | grep -q "127.0.0.1:$port "; then
            echo -e "  ${YELLOW}跳过 $name:$port → 本地 $port 已占用${NC}"
            continue
        fi

        ssh -S "$CTRL_SOCK" -O forward -L "${port}:${ip}:${port}" "$SSH_ALIAS" 2>/dev/null
        if [ $? -eq 0 ]; then
            echo -e "  ${GREEN}$name:$port ← 127.0.0.1:$port${NC}"
            forwarded=$((forwarded + 1))
        else
            echo -e "  ${RED}$name:$port 转发失败${NC}"
        fi
    done
    echo -e "  ${GREEN}共转发 $forwarded 个端口${NC}"

    # 4. 启动 SOCKS5 代理（供 curl 等 HTTP 客户端访问 Web 服务）
    echo -e "${GREEN}[4/4] 启动 SOCKS5 代理 (端口 $SOCKS_PORT)...${NC}"
    ssh -S "$CTRL_SOCK" -O forward -D "$SOCKS_PORT" "$SSH_ALIAS" 2>/dev/null
    echo -e "  ${GREEN}SOCKS5 代理: 127.0.0.1:$SOCKS_PORT${NC}"

    # 5. 更新 hosts（容器名 → 127.0.0.1）
    if require_sudo; then
        remove_hosts 2>/dev/null
        update_hosts
    fi

    echo ""
    echo -e "${GREEN}===== 隧道已启用 =====${NC}"
    echo -e "  ${YELLOW}Go 后端直连（容器名 → 127.0.0.1）：${NC}"
    echo -e "    PostgreSQL:  127.0.0.1:5432"
    echo -e "    Redis:       127.0.0.1:6379"
    echo -e "    InfluxDB:    127.0.0.1:8086"
    echo -e "  ${YELLOW}HTTP 访问（SOCKS5 代理 + 实际 IP）：${NC}"
    echo -e "    curl --proxy socks5://127.0.0.1:1080 http://<容器IP>:端口/路径"
    echo ""
    local api_ip="${CONTAINER_IPS[iot-api-v2]}"
    [ -n "$api_ip" ] && echo -e "    示例: curl --proxy socks5://127.0.0.1:1080 http://$api_ip:9091/api/user/login"
    echo ""
    echo -e "  运行 Go 后端: ${GREEN}cd back && go run .${NC}"
}

stop_tunnel() {
    echo -e "${GREEN}===== 关闭隧道 =====${NC}"

    if is_tunnel_running; then
        echo -e "${GREEN}关闭 SSH 主控连接及所有转发...${NC}"
        ssh -S "$CTRL_SOCK" -O exit "$SSH_ALIAS" 2>/dev/null
        echo -e "  ${GREEN}已关闭${NC}"
    else
        echo -e "  ${YELLOW}隧道未在运行${NC}"
    fi

    rm -f "$CTRL_SOCK" 2>/dev/null

    if require_sudo 2>/dev/null; then
        remove_hosts
    fi

    echo -e "${GREEN}===== 已全部清理 =====${NC}"
}

show_status() {
    echo -e "========== 隧道状态 =========="

    echo -e "SSH 主控连接："
    if is_tunnel_running; then
        echo -e "  ${GREEN}运行中${NC} (socket: $CTRL_SOCK)"
    else
        echo -e "  ${RED}未运行${NC}"
    fi

    echo -e "端口转发："
    local any=0
    for entry in "${FORWARD_PORTS[@]}"; do
        local port="${entry##*:}"
        local name="${entry%%:*}"
        if ss -tlnp 2>/dev/null | grep -q "127.0.0.1:$port "; then
            echo -e "  ${GREEN}$port ($name)${NC}"
            any=1
        fi
    done
    [ $any -eq 0 ] && echo -e "  ${RED}无${NC}"

    echo -e "SOCKS5 代理："
    if ss -tlnp 2>/dev/null | grep -q ":$SOCKS_PORT "; then
        echo -e "  ${GREEN}端口 $SOCKS_PORT 已监听${NC}"
    else
        echo -e "  ${RED}端口 $SOCKS_PORT 未监听${NC}"
    fi

    echo -e "hosts 映射："
    if sudo grep -q "$HOSTS_MARK_START" /etc/hosts 2>/dev/null; then
        sudo sed -n "/$HOSTS_MARK_START/,/$HOSTS_MARK_END/p" /etc/hosts | grep -v "^#" | sed 's/^/  /'
    else
        echo -e "  ${RED}未配置${NC}"
    fi
    echo -e "=============================="
}

usage() {
    echo "用法: $0 {on|off|status|help}"
    echo ""
    echo "  on     启动隧道（端口转发 + SOCKS5 + hosts）"
    echo "  off    关闭隧道并清理"
    echo "  status 查看状态"
    echo ""
    echo "Go 后端启动方式："
    echo "  1. ./tunnel.sh on          # 先建立隧道"
    echo "  2. cd back && go run .     # 再启动后端"
}

# --------------------------------------------
# 主逻辑
# --------------------------------------------
case "$1" in
    on)
        start_tunnel
        ;;
    off)
        stop_tunnel
        ;;
    status)
        show_status
        ;;
    help|--help|-h)
        usage
        ;;
    *)
        echo -e "${RED}错误：未知参数 '$1'${NC}"
        usage
        exit 1
        ;;
esac

exit 0
