# 项目地图

## 发现 Redis Helper 区域

不要假设其它仓库的 helper 包名可用。用当前仓库搜索 helper：

```bash
rg --files | rg '(^|/)(common/keys|common/rediskeys|data/keys|rediskeys|redis_keys|cachekeys|cache/keys)(/|$)'
```

同时搜索 registry 或 static check：

```bash
rg -n 'RedisKey|redis key|registry|static_check|SCAN|KEYS|Scan\\(|Keys\\(' --glob '*.go'
```

## 长期规则

- 除非用户明确要求隔离，共享 cache 必须继续共享。
- 优先使用 helper function 加 registry/static test，不只做一次性调用点清理。
- 在线路径避免高基数 wildcard scan。
- invalidation、TTL、cache miss、rebuild、fallback 要一起审查。
- SQL/Lua/cache 资产要与 Go embed 和测试保持同步。

## 审查命令

- `rg -n "SCAN|Keys\\(|Scan\\(|redis\\.|Redis|fmt\\.Sprintf|:%|:\\*" --glob '*.go'`
- `python3 <skill-dir>/scripts/redis_key_scan.py <repo>`
- 存在 helper registry/static check 时，运行仓库已有相关测试。
