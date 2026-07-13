package keys

// RateLimitRedisKey 返回按当前应用、业务域和资源隔离的限流逻辑 key。
// scope 必须是稳定的低基数业务名；resource 由调用方使用不含敏感信息的稳定标识。
func RateLimitRedisKey(scope string, resource string) string {
	if !validRateLimitScope(scope) || !validRateLimitResource(resource) {
		return ""
	}
	return WithPrefix(rateLimitRedisRoot + ":" + scope + ":" + resource)
}

// RateLimitFixedIntervalRedisKey 返回按当前应用、业务域和资源隔离的固定间隔限流 key。
// 固定间隔状态与 GCRA 配额状态分开存储，避免两种算法误用同一 Redis key。
func RateLimitFixedIntervalRedisKey(scope string, resource string) string {
	if !validRateLimitScope(scope) || !validRateLimitResource(resource) {
		return ""
	}
	return WithPrefix(rateLimitRedisRoot + ":" + rateLimitFixedIntervalSegment + ":" + scope + ":" + resource)
}

// validRateLimitScope 校验低基数业务域只能使用适合 Redis key 的小写字符。
func validRateLimitScope(scope string) bool {
	if scope == "" || len(scope) > 64 {
		return false
	}
	for _, char := range scope {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

// validRateLimitResource 校验资源标识只包含 ASCII 安全字符，避免空白和 Cluster hash tag 混入 key。
func validRateLimitResource(resource string) bool {
	if resource == "" || len(resource) > 256 {
		return false
	}
	for _, char := range resource {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' && char != '-' && char != '.' && char != ':' {
			return false
		}
	}
	return true
}
