package keys

const (
	// EmptyValueMarker 表示空值缓存占位符，避免缓存穿透时重复回源。
	EmptyValueMarker = "__empty__"
)
