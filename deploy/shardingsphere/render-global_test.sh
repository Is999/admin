#!/bin/sh
set -eu

# render-global_test.sh 验证集群配置不会在 ZooKeeper ACL 凭据缺失或非法时生成。
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM
output_path="$test_dir/global.yaml"

export PROXY_NAMESPACE=prod_user_sharding
export ZOOKEEPER_SERVERS=zk1.example:2181,zk2.example:2181,zk3.example:2181
export PROXY_ADMIN_USER=proxy_admin
export PROXY_ADMIN_PASSWORD=AdminPassword_123
export PROXY_APP_USER=app_user
export PROXY_APP_PASSWORD=ApplicationPassword_123
export PROXY_DATABASE=app_db
export PROXY_KERNEL_EXECUTOR_SIZE=32
export PROXY_FRONTEND_MAX_CONNECTIONS=2000

unset ZOOKEEPER_DIGEST
if "$script_dir/render-global.sh" "$output_path" >"$test_dir/missing.out" 2>&1; then
  echo "ZOOKEEPER_DIGEST 缺失时不应生成配置" >&2
  exit 1
fi

export ZOOKEEPER_DIGEST=invalid
if "$script_dir/render-global.sh" "$output_path" >"$test_dir/invalid.out" 2>&1; then
  echo "ZOOKEEPER_DIGEST 非法时不应生成配置" >&2
  exit 1
fi

export ZOOKEEPER_DIGEST=proxy_meta:ZooKeeperPassword_123
"$script_dir/render-global.sh" "$output_path" >"$test_dir/valid.out"
grep -Fq 'digest: proxy_meta:ZooKeeperPassword_123' "$output_path"
if grep -Eq '__[A-Z0-9_]+__' "$output_path"; then
  echo "渲染结果仍包含占位符" >&2
  exit 1
fi
