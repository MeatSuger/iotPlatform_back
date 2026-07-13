#!/bin/bash

# ============================================================
# sshuttle 隧道管理脚本 (WSL + TPROXY + DNS + 自动 hosts 更新)
# 用法: ./tunnel.sh on | off | status
# ============================================================

SSH_ALIAS="aliyun"                    # 对应 ~/.ssh/config 中的 Host
SUBNET="172.18.0.0/16"                # 要转发的子网
METHOD="tproxy"                       # 使用 TPROXY 方法
SSHUTTLE_CMD="sshuttle -r $SSH_ALIAS $SUBNET --method=$METHOD --dns"

# 策略路由参数
MARK=0x01
TABLE=100

# hosts 文件标记
HOSTS_MARK_START="# BEGIN_DOCKER_SSHUTTLE"
HOSTS_MARK_END="# END_DOCKER_SSHUTTLE"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# --------------------------------------------
# 辅助函数
# --------------------------------------------

# 检查隧道是否已启动
is_tunnel_running() {
    pgrep -f "sshuttle.*$SSH_ALIAS.*$SUBNET" > /dev/null 2>&1
    return $?
}

# 从远端服务器获取容器名和 IP（抑制 motd，过滤无效 IP）
fetch_container_hosts() {
    echo -e "${GREEN}正在从远端服务器获取容器信息...${NC}" >&2
    ssh  -q -T "$SSH_ALIAS" 2>/dev/null << 'EOF'
docker ps -q | xargs -r docker inspect -f '{{.Name}} {{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' | sed 's/^\/\///' | awk '$2 != "" && $2 != "<no value>" && $2 !~ /invalid/ {print $1, $2}'
EOF
}
# 更新本地 /etc/hosts（添加容器映射）
update_hosts() {
    echo -e "${GREEN}正在更新 /etc/hosts...${NC}"
    # 先删除旧的标记区域（如果有）
    remove_hosts

    # 获取容器列表
    local entries=$(fetch_container_hosts)
    if [ -z "$entries" ]; then
        echo -e "${YELLOW}未获取到任何容器信息，跳过 hosts 更新。${NC}"
        return 0
    fi

    # 备份 hosts（可选）
    sudo cp /etc/hosts /etc/hosts.bak

    # 添加标记和条目（格式：IP    name）
    {
        echo "$HOSTS_MARK_START"
        echo "$entries" | while read -r name ip; do
	    name=${name#/}	
            echo "$ip    $name"
        done
        echo "$HOSTS_MARK_END"
    } | sudo tee -a /etc/hosts > /dev/null

    echo -e "${GREEN}hosts 文件已更新，添加了以下容器映射：${NC}"
    sudo sed -n "/$HOSTS_MARK_START/,/$HOSTS_MARK_END/p" /etc/hosts | grep -v "^#"
}

# 删除 /etc/hosts 中的标记区域
remove_hosts() {
    if sudo grep -q "$HOSTS_MARK_START" /etc/hosts; then
        echo -e "${GREEN}正在从 /etc/hosts 中移除容器映射...${NC}"
        sudo sed -i "/$HOSTS_MARK_START/,/$HOSTS_MARK_END/d" /etc/hosts
        echo -e "${GREEN}已移除。${NC}"
    else
        echo -e "${YELLOW}未找到之前的容器映射标记，跳过。${NC}"
    fi
}

# --------------------------------------------
# 核心操作
# --------------------------------------------

# 启动隧道
start_tunnel() {
    if is_tunnel_running; then
        echo -e "${YELLOW}隧道已经在运行中。${NC}"
        return 0
    fi

    echo -e "${GREEN}正在启动 sshuttle 隧道 (含 DNS 转发)...${NC}"
    sudo $SSHUTTLE_CMD &
    sleep 2  # 等待隧道初始化

    if ! is_tunnel_running; then
        echo -e "${RED}错误：sshuttle 启动失败，请检查 SSH 连接。${NC}"
        return 1
    fi

    echo -e "${GREEN}sshuttle 已启动 (PID: $(pgrep -f "sshuttle.*$SSH_ALIAS.*$SUBNET"))${NC}"

    # 添加策略路由
    echo -e "${GREEN}配置策略路由...${NC}"
    sudo ip rule del fwmark $MARK table $TABLE 2>/dev/null
    sudo ip route del local 0.0.0.0/0 dev lo table $TABLE 2>/dev/null

    sudo ip rule add fwmark $MARK table $TABLE
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}规则添加成功。${NC}"
    else
        echo -e "${RED}规则添加失败。${NC}"
    fi

    sudo ip route add local 0.0.0.0/0 dev lo table $TABLE
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}路由添加成功。${NC}"
    else
        echo -e "${RED}路由添加失败。${NC}"
    fi

    # 更新 hosts 文件
    update_hosts

    echo -e "${GREEN}隧道已完全启用，可以访问 $SUBNET 网段，DNS 请求也将通过隧道转发。${NC}"
    echo -e "${YELLOW}提示：如果 DNS 解析仍然失败，请检查远端服务器的 /etc/resolv.conf 配置。${NC}"
}

# 停止隧道
stop_tunnel() {
    if ! is_tunnel_running; then
        echo -e "${YELLOW}隧道未在运行。${NC}"
    else
        echo -e "${GREEN}正在停止 sshuttle 隧道...${NC}"
        sudo pkill -f "sshuttle.*$SSH_ALIAS.*$SUBNET"
        sleep 1
        if is_tunnel_running; then
            echo -e "${RED}停止失败，尝试强制终止...${NC}"
            sudo pkill -9 -f "sshuttle.*$SSH_ALIAS.*$SUBNET"
        fi
        echo -e "${GREEN}隧道已停止。${NC}"
    fi

    # 清理策略路由
    echo -e "${GREEN}清理策略路由...${NC}"
    sudo ip rule del fwmark $MARK table $TABLE 2>/dev/null && echo -e "${GREEN}规则已删除。${NC}" || echo -e "${YELLOW}规则不存在或已删除。${NC}"
    sudo ip route del local 0.0.0.0/0 dev lo table $TABLE 2>/dev/null && echo -e "${GREEN}路由已删除。${NC}" || echo -e "${YELLOW}路由不存在或已删除。${NC}"

    # 清理 hosts 文件中的条目
    remove_hosts
}

# 显示状态
show_status() {
    echo -e "隧道状态："
    if is_tunnel_running; then
        echo -e "  ${GREEN}运行中${NC} (PID: $(pgrep -f "sshuttle.*$SSH_ALIAS.*$SUBNET"))"
    else
        echo -e "  ${RED}未运行${NC}"
    fi

    echo -e "策略路由规则："
    if ip rule show | grep -q "fwmark $MARK"; then
        echo -e "  ${GREEN}已配置${NC}"
        ip rule show | grep "fwmark $MARK"
    else
        echo -e "  ${RED}未配置${NC}"
    fi

    echo -e "策略路由表条目："
    if ip route show table $TABLE | grep -q "local default"; then
        echo -e "  ${GREEN}已配置${NC}"
        ip route show table $TABLE
    else
        echo -e "  ${RED}未配置${NC}"
    fi

    echo -e "hosts 文件中的容器映射："
    if sudo grep -q "$HOSTS_MARK_START" /etc/hosts; then
        echo -e "  ${GREEN}已配置${NC}"
        sudo sed -n "/$HOSTS_MARK_START/,/$HOSTS_MARK_END/p" /etc/hosts | grep -v "^#"
    else
        echo -e "  ${RED}未配置${NC}"
    fi
}

# 帮助信息
usage() {
    echo "用法: $0 {on|off|status|help}"
    echo "  on     - 启动隧道、配置策略路由、更新 hosts"
    echo "  off    - 停止隧道、清理策略路由、移除 hosts 条目"
    echo "  status - 显示当前状态"
    echo "  help   - 显示此帮助信息"
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
