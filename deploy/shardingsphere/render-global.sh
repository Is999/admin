#!/bin/sh
set -eu

# render-global.sh 只接受受限字符，避免密钥或标识符破坏 YAML 与 sed 替换语义。
require_value() {
  value_name="$1"
  value=$(printenv "$value_name" 2>/dev/null || true)
  if [ -z "$value" ]; then
    echo "缺少环境变量 $value_name" >&2
    exit 1
  fi
}

validate_value() {
  value_name="$1"
  pattern="$2"
  value=$(printenv "$value_name" 2>/dev/null || true)
  if ! printf '%s' "$value" | LC_ALL=C grep -Eq "$pattern"; then
    echo "环境变量 $value_name 包含不支持的字符或长度不合法" >&2
    exit 1
  fi
}

for value_name in PROXY_NAMESPACE ZOOKEEPER_SERVERS ZOOKEEPER_DIGEST PROXY_ADMIN_USER PROXY_ADMIN_PASSWORD PROXY_APP_USER PROXY_APP_PASSWORD PROXY_DATABASE PROXY_KERNEL_EXECUTOR_SIZE PROXY_FRONTEND_MAX_CONNECTIONS; do
  require_value "$value_name"
done

validate_value PROXY_NAMESPACE '^[A-Za-z][A-Za-z0-9_-]{2,63}$'
validate_value ZOOKEEPER_SERVERS '^[A-Za-z0-9_.:-]+(,[A-Za-z0-9_.:-]+)*$'
validate_value ZOOKEEPER_DIGEST '^[A-Za-z][A-Za-z0-9_]{2,31}:[A-Za-z0-9_.@%+=/-]{16,128}$'
validate_value PROXY_ADMIN_USER '^[A-Za-z][A-Za-z0-9_]{2,31}$'
validate_value PROXY_APP_USER '^[A-Za-z][A-Za-z0-9_]{2,31}$'
validate_value PROXY_DATABASE '^[A-Za-z][A-Za-z0-9_]{1,63}$'
validate_value PROXY_ADMIN_PASSWORD '^[A-Za-z0-9_.@%+=:/-]{16,128}$'
validate_value PROXY_APP_PASSWORD '^[A-Za-z0-9_.@%+=:/-]{16,128}$'
validate_value PROXY_KERNEL_EXECUTOR_SIZE '^[0-9]{1,4}$'
validate_value PROXY_FRONTEND_MAX_CONNECTIONS '^[0-9]{3,6}$'

if [ "$PROXY_KERNEL_EXECUTOR_SIZE" -lt 4 ] || [ "$PROXY_KERNEL_EXECUTOR_SIZE" -gt 4096 ]; then
  echo "环境变量 PROXY_KERNEL_EXECUTOR_SIZE 必须在 4..4096 之间" >&2
  exit 1
fi
if [ "$PROXY_FRONTEND_MAX_CONNECTIONS" -lt 100 ] || [ "$PROXY_FRONTEND_MAX_CONNECTIONS" -gt 200000 ]; then
  echo "环境变量 PROXY_FRONTEND_MAX_CONNECTIONS 必须在 100..200000 之间" >&2
  exit 1
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
template_path="$script_dir/conf/global.yaml.tmpl"
output_path="${1:-$script_dir/runtime/global.yaml}"
output_dir=$(dirname -- "$output_path")
mkdir -p "$output_dir"
umask 077
tmp_path=$(mktemp "$output_dir/.global.yaml.XXXXXX")
trap 'rm -f "$tmp_path"' EXIT HUP INT TERM

# 通过环境读取替换值，避免口令出现在进程命令行参数中。
awk '
  {
    gsub(/__PROXY_NAMESPACE__/, ENVIRON["PROXY_NAMESPACE"])
    gsub(/__ZOOKEEPER_SERVERS__/, ENVIRON["ZOOKEEPER_SERVERS"])
    gsub(/__ZOOKEEPER_DIGEST__/, ENVIRON["ZOOKEEPER_DIGEST"])
    gsub(/__PROXY_ADMIN_USER__/, ENVIRON["PROXY_ADMIN_USER"])
    gsub(/__PROXY_ADMIN_PASSWORD__/, ENVIRON["PROXY_ADMIN_PASSWORD"])
    gsub(/__PROXY_APP_USER__/, ENVIRON["PROXY_APP_USER"])
    gsub(/__PROXY_APP_PASSWORD__/, ENVIRON["PROXY_APP_PASSWORD"])
    gsub(/__PROXY_DATABASE__/, ENVIRON["PROXY_DATABASE"])
    gsub(/__PROXY_KERNEL_EXECUTOR_SIZE__/, ENVIRON["PROXY_KERNEL_EXECUTOR_SIZE"])
    gsub(/__PROXY_FRONTEND_MAX_CONNECTIONS__/, ENVIRON["PROXY_FRONTEND_MAX_CONNECTIONS"])
    print
  }
' "$template_path" > "$tmp_path"

mv "$tmp_path" "$output_path"
trap - EXIT HUP INT TERM
echo "已生成 ${output_path}（权限 600）"
