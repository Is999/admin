package ipregion

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"admin/internal/config"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

// 真实 XDB 测试环境变量与合成记录集中定义。
const (
	// testIPv4XDBEnv 指定真实 IPv4 XDB 测试文件路径。
	testIPv4XDBEnv = "IPREGION_TEST_IPV4_XDB"
	// testIPv6XDBEnv 指定真实 IPv6 XDB 测试文件路径。
	testIPv6XDBEnv = "IPREGION_TEST_IPV6_XDB"
	// testXDBRegion 是合成 XDB 查询应返回的完整原始归属地。
	testXDBRegion = "中国|广东|深圳|电信|CN"
)

// TestLocatorLookup 验证禁用场景和保留地址的稳定行为。
func TestLocatorLookup(t *testing.T) {
	locator, err := New(config.IPRegionConfig{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := locator.Lookup("127.0.0.1"); got != "" {
		t.Fatalf("Lookup(disabled) = %q, want empty", got)
	}
	locator.enabled = true
	if got := locator.Lookup("127.0.0.1"); got != regionLoopback {
		t.Fatalf("Lookup(loopback) = %q, want %q", got, regionLoopback)
	}
	if got := locator.Lookup("10.0.0.1"); got != regionLocal {
		t.Fatalf("Lookup(private) = %q, want %q", got, regionLocal)
	}
	if got := locator.Lookup("100.64.0.1"); got != regionLocal {
		t.Fatalf("Lookup(carrier-grade NAT) = %q, want %q", got, regionLocal)
	}
	if got := locator.Lookup("bad-ip"); got != "" {
		t.Fatalf("Lookup(invalid) = %q, want empty", got)
	}
}

// TestDisplayRegion 验证展示字段不泄漏 ISP 与 ISO 编码。
func TestDisplayRegion(t *testing.T) {
	if got, want := displayRegion("中国|广东|深圳|电信|CN"), "中国 广东 深圳"; got != want {
		t.Fatalf("displayRegion() = %q, want %q", got, want)
	}
	if got, want := displayRegion("中国|0|0|0|CN"), "中国"; got != want {
		t.Fatalf("displayRegion() = %q, want %q", got, want)
	}
	if got := displayRegion(string([]byte{0xff, '|', '0'})); got != "" {
		t.Fatalf("displayRegion(invalid UTF-8) = %q, want empty", got)
	}
	if got := displayRegion("中国|广\x00东|深圳"); got != "" {
		t.Fatalf("displayRegion(control character) = %q, want empty", got)
	}
	if got := displayRegion("中国|广东|深圳|电信"); got != "" {
		t.Fatalf("displayRegion(invalid field count) = %q, want empty", got)
	}
	if got, want := displayRegion("中国|广东|深圳|电信|CN|华南"), "中国 广东 深圳"; got != want {
		t.Fatalf("displayRegion(extended fields) = %q, want %q", got, want)
	}
	if got := displayRegion(strings.Repeat("界", maxDisplayRegionRunes+1) + "|0|0|0|CN"); got != "" {
		t.Fatalf("displayRegion(overlong) rune_count=%d, want empty", maxDisplayRegionRunes+1)
	}
	if got := displayRegion(strings.Repeat("界", maxDisplayRegionRunes) + "|0|0|0|CN"); got == "" {
		t.Fatalf("displayRegion(at limit) = empty, want value")
	}
}

// TestNewRequiresDatabasePath 验证启用后不能静默跳过数据库加载。
func TestNewRequiresDatabasePath(t *testing.T) {
	if _, err := New(config.IPRegionConfig{Enabled: true}); err == nil {
		t.Fatal("New() error = nil, want missing XDB path error")
	}
}

// TestLoadMemoryDBRejectsOversizedFile 验证异常大文件会在读取前被拒绝，避免启动期内存失控。
func TestLoadMemoryDBRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.xdb")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建测试 XDB 失败: %v", err)
	}
	if err = file.Truncate(maxXDBTotalBytes + 1); err != nil {
		_ = file.Close()
		t.Fatalf("扩展测试 XDB 失败: %v", err)
	}
	if err = file.Close(); err != nil {
		t.Fatalf("关闭测试 XDB 失败: %v", err)
	}
	if _, err = loadMemoryDB(path, xdb.IPv4, maxXDBTotalBytes); err == nil {
		t.Fatal("超限 XDB 应在读取前被拒绝")
	}
}

// TestNewLimitsAggregateXDBBytes 验证双库共享 128MiB 总额度而不是分别占用额度。
func TestNewLimitsAggregateXDBBytes(t *testing.T) {
	v4Content := newSyntheticXDB(t, xdb.IPv4)
	v4Path := writeSyntheticXDB(t, "ipv4.xdb", v4Content)
	v6Path := filepath.Join(t.TempDir(), "ipv6.xdb")
	v6File, err := os.Create(v6Path)
	if err != nil {
		t.Fatalf("创建 IPv6 聚合上限测试文件失败: %v", err)
	}
	oversized := maxXDBTotalBytes - int64(len(v4Content)) + 1
	if err = v6File.Truncate(oversized); err != nil {
		_ = v6File.Close()
		t.Fatalf("扩展 IPv6 聚合上限测试文件失败: %v", err)
	}
	if err = v6File.Close(); err != nil {
		t.Fatalf("关闭 IPv6 聚合上限测试文件失败: %v", err)
	}
	if _, err = New(config.IPRegionConfig{Enabled: true, IPv4XDBPath: v4Path, IPv6XDBPath: v6Path}); err == nil {
		t.Fatal("IPv4 与 IPv6 XDB 聚合大小超限时 New() 应失败")
	}
}

// TestLoadSyntheticXDB 验证 IPv4 与 IPv6 合成库均可通过完整校验并执行查询。
func TestLoadSyntheticXDB(t *testing.T) {
	tests := []struct {
		name    string       // IP 版本名称
		version *xdb.Version // XDB IP 版本
		ip      string       // 合成库查询地址
	}{
		{name: "IPv4", version: xdb.IPv4, ip: "8.8.8.8"},
		{name: "IPv6", version: xdb.IPv6, ip: "2001:4860:4860::8888"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeSyntheticXDB(t, strings.ToLower(test.name)+".xdb", newSyntheticXDB(t, test.version))
			database, err := loadMemoryDB(path, test.version, maxXDBTotalBytes)
			if err != nil {
				t.Fatalf("loadMemoryDB() error = %v", err)
			}
			if got, searchErr := database.search(test.ip); searchErr != nil || got != testXDBRegion {
				t.Fatalf("search() = %q, error = %v, want %q", got, searchErr, testXDBRegion)
			}
			invalidFieldCount := []byte("中国|广东|深圳|CN")
			if err := validateXDB(newSyntheticXDBWithRegion(t, test.version, invalidFieldCount), test.version); err == nil {
				t.Fatal("字段数量错误的归属地记录应被拒绝")
			}
			extendedRegion := []byte("中国|广东|深圳|电信|CN|华南")
			if err := validateXDB(newSyntheticXDBWithRegion(t, test.version, extendedRegion), test.version); err != nil {
				t.Fatalf("尾部扩展字段应兼容: %v", err)
			}
			controlRegion := []byte("中国|广东|深圳|电信|C\n")
			if err := validateXDB(newSyntheticXDBWithRegion(t, test.version, controlRegion), test.version); err == nil {
				t.Fatal("包含控制字符的归属地记录应被拒绝")
			}
		})
	}
}

// TestLocatorWithSyntheticDualStackXDB 验证双栈配置沿真实入口选择各自数据库并输出安全展示值。
func TestLocatorWithSyntheticDualStackXDB(t *testing.T) {
	v4Path := writeSyntheticXDB(t, "ipv4.xdb", newSyntheticXDB(t, xdb.IPv4))
	v6Path := writeSyntheticXDB(t, "ipv6.xdb", newSyntheticXDB(t, xdb.IPv6))
	locator, err := New(config.IPRegionConfig{Enabled: true, IPv4XDBPath: v4Path, IPv6XDBPath: v6Path})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(locator.Close)
	if !locator.Enabled() {
		t.Fatal("Enabled() = false, want true")
	}
	for _, ip := range []string{"8.8.8.8", "2001:4860:4860::8888", "2001:4860:4860::8888%eth0"} {
		if got, want := locator.Lookup(ip), "中国 广东 深圳"; got != want {
			t.Fatalf("Lookup(%s) = %q, want %q", ip, got, want)
		}
	}
}

// TestLoadMemoryDBRejectsVersionMismatch 验证文件头 IP 版本与配置入口不一致时启动失败。
func TestLoadMemoryDBRejectsVersionMismatch(t *testing.T) {
	path := writeSyntheticXDB(t, "ipv4.xdb", newSyntheticXDB(t, xdb.IPv4))
	if _, err := loadMemoryDB(path, xdb.IPv6, maxXDBTotalBytes); err == nil {
		t.Fatal("IPv4 XDB 不应被 IPv6 入口接受")
	}
}

// TestValidateXDBRejectsCorruption 验证两种 IP 版本的主要结构损坏均在启动期被拒绝。
func TestValidateXDBRejectsCorruption(t *testing.T) {
	versions := []*xdb.Version{xdb.IPv4, xdb.IPv6}
	for _, version := range versions {
		t.Run(version.Name, func(t *testing.T) {
			tests := []struct {
				name    string       // 损坏场景
				corrupt func([]byte) // 对合成 XDB 执行的损坏操作
			}{
				{
					name: "header_structure_version",
					corrupt: func(content []byte) {
						binary.LittleEndian.PutUint16(content[0:], xdb.Structure20)
					},
				},
				{
					name: "header_index_policy",
					corrupt: func(content []byte) {
						binary.LittleEndian.PutUint16(content[2:], uint16(xdb.BTreeIndexPolicy))
					},
				},
				{
					name: "header_pointer_width",
					corrupt: func(content []byte) {
						binary.LittleEndian.PutUint16(content[18:], 8)
					},
				},
				{
					name: "header_index_end",
					corrupt: func(content []byte) {
						end := binary.LittleEndian.Uint32(content[12:])
						binary.LittleEndian.PutUint32(content[12:], end+1)
					},
				},
				{
					name: "vector_pointer",
					corrupt: func(content []byte) {
						binary.LittleEndian.PutUint32(content[xdb.HeaderInfoLength:], uint32(xdbVectorEnd))
					},
				},
				{
					name: "vector_unpaired_pointer",
					corrupt: func(content []byte) {
						binary.LittleEndian.PutUint32(content[xdb.HeaderInfoLength+4:], 0)
					},
				},
				{
					name: "vector_missing_bucket",
					corrupt: func(content []byte) {
						binary.LittleEndian.PutUint32(content[xdb.HeaderInfoLength:], 0)
						binary.LittleEndian.PutUint32(content[xdb.HeaderInfoLength+4:], 0)
					},
				},
				{
					name: "vector_misaligned_pointer",
					corrupt: func(content []byte) {
						start := binary.LittleEndian.Uint32(content[8:])
						binary.LittleEndian.PutUint32(content[xdb.HeaderInfoLength:], start+1)
					},
				},
				{
					name: "segment_range",
					corrupt: func(content []byte) {
						indexStart := int(binary.LittleEndian.Uint32(content[8:]))
						for index := range version.Bytes {
							content[indexStart+index] = 0xff
							content[indexStart+version.Bytes+index] = 0
						}
					},
				},
				{
					name: "segment_data_pointer",
					corrupt: func(content []byte) {
						indexStart := int(binary.LittleEndian.Uint32(content[8:]))
						dataOffset := indexStart + version.Bytes*2 + 2
						binary.LittleEndian.PutUint32(content[dataOffset:], uint32(indexStart))
					},
				},
				{
					name: "empty_segment_data",
					corrupt: func(content []byte) {
						indexStart := int(binary.LittleEndian.Uint32(content[8:]))
						dataOffset := indexStart + version.Bytes*2
						binary.LittleEndian.PutUint16(content[dataOffset:], 0)
					},
				},
				{
					name: "invalid_utf8_data",
					corrupt: func(content []byte) {
						content[xdbVectorEnd] = 0xff
					},
				},
			}
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					content := newSyntheticXDB(t, version)
					test.corrupt(content)
					if err := validateXDB(content, version); err == nil {
						t.Fatal("validateXDB() error = nil, want corruption error")
					}
				})
			}
		})
	}
}

// TestMemoryDBSearchRecoversCorruptContent 验证损坏内容不会把审计或登录进程直接打崩。
func TestMemoryDBSearchRecoversCorruptContent(t *testing.T) {
	database := &memoryDB{version: xdb.IPv4, content: make([]byte, 128)}
	if _, err := database.search("114.114.114.114"); err == nil {
		t.Fatal("损坏 XDB 查询应返回错误")
	}
}

// TestLocatorCloseIsConcurrentSafe 验证关闭操作可重复执行且不会与查询产生数据竞争。
func TestLocatorCloseIsConcurrentSafe(t *testing.T) {
	locator := &Locator{enabled: true}
	var waitGroup sync.WaitGroup
	for range 20 {
		waitGroup.Add(2)
		go func() {
			defer waitGroup.Done()
			_ = locator.Lookup("127.0.0.1")
		}()
		go func() {
			defer waitGroup.Done()
			locator.Close()
		}()
	}
	waitGroup.Wait()
	locator.Close()
	if got := locator.Lookup("127.0.0.1"); got != "" {
		t.Fatalf("Lookup(after Close) = %q, want empty", got)
	}
}

// TestLocatorWithRealXDB 验证真实数据的版本、并发查询、关闭与文件句柄生命周期。
func TestLocatorWithRealXDB(t *testing.T) {
	path := strings.TrimSpace(os.Getenv(testIPv4XDBEnv))
	if path == "" {
		t.Skipf("%s 未配置，跳过真实 XDB 测试", testIPv4XDBEnv)
	}
	beforeFDs, canInspectFDs := countOpenXDBFileDescriptors(path)
	locator, err := New(config.IPRegionConfig{Enabled: true, IPv4XDBPath: path})
	if err != nil {
		t.Fatalf("加载真实 XDB 失败: %v", err)
	}
	if afterFDs, ok := countOpenXDBFileDescriptors(path); canInspectFDs && ok && afterFDs != beforeFDs {
		t.Fatalf("XDB 初始化后仍持有文件句柄 before=%d after=%d", beforeFDs, afterFDs)
	}
	if got := locator.Lookup("114.114.114.114"); got == "" {
		t.Fatal("真实 XDB 未返回公共 IPv4 归属地")
	}
	if _, err = New(config.IPRegionConfig{Enabled: true, IPv6XDBPath: path}); err == nil {
		t.Fatal("IPv4 XDB 不应被接受为 IPv6 数据库")
	}

	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for range 64 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			for range 100 {
				_ = locator.Lookup("114.114.114.114")
			}
		}()
	}
	close(start)
	locator.Close()
	waitGroup.Wait()
	locator.Close()
	if got := locator.Lookup("114.114.114.114"); got != "" {
		t.Fatalf("关闭后查询=%q，期望空字符串", got)
	}
	if afterFDs, ok := countOpenXDBFileDescriptors(path); canInspectFDs && ok && afterFDs != beforeFDs {
		t.Fatalf("XDB 关闭后文件句柄未恢复 before=%d after=%d", beforeFDs, afterFDs)
	}
}

// TestLocatorWithRealIPv6XDB 验证真实 IPv6 数据的版本、公共地址查询与并发关闭。
func TestLocatorWithRealIPv6XDB(t *testing.T) {
	path := strings.TrimSpace(os.Getenv(testIPv6XDBEnv))
	if path == "" {
		t.Skipf("%s 未配置，跳过真实 IPv6 XDB 测试", testIPv6XDBEnv)
	}
	locator, err := New(config.IPRegionConfig{Enabled: true, IPv6XDBPath: path})
	if err != nil {
		t.Fatalf("加载真实 IPv6 XDB 失败: %v", err)
	}
	t.Cleanup(locator.Close)
	const publicIPv6 = "2001:4860:4860::8888"
	if got := locator.Lookup(publicIPv6); got == "" {
		t.Fatal("真实 XDB 未返回公共 IPv6 归属地")
	}
	if _, err = New(config.IPRegionConfig{Enabled: true, IPv4XDBPath: path}); err == nil {
		t.Fatal("IPv6 XDB 不应被接受为 IPv4 数据库")
	}

	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for range 64 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			for range 100 {
				_ = locator.Lookup(publicIPv6)
			}
		}()
	}
	close(start)
	locator.Close()
	waitGroup.Wait()
	locator.Close()
	if got := locator.Lookup(publicIPv6); got != "" {
		t.Fatalf("关闭后 IPv6 查询=%q，期望空字符串", got)
	}
}

// countOpenXDBFileDescriptors 统计当前进程指向指定 XDB 的文件句柄；平台不支持时返回 false。
func countOpenXDBFileDescriptors(path string) (int, bool) {
	target, err := filepath.Abs(path)
	if err != nil {
		return 0, false
	}
	for _, descriptorDir := range []string{"/proc/self/fd", "/dev/fd"} {
		entries, readErr := os.ReadDir(descriptorDir)
		if readErr != nil {
			continue
		}
		count := 0
		inspected := false
		for _, entry := range entries {
			linked, linkErr := os.Readlink(filepath.Join(descriptorDir, entry.Name()))
			if linkErr != nil {
				continue
			}
			inspected = true
			linked = strings.TrimSuffix(linked, " (deleted)")
			if !filepath.IsAbs(linked) {
				continue
			}
			if filepath.Clean(linked) == filepath.Clean(target) {
				count++
			}
		}
		if inspected {
			return count, true
		}
	}
	return 0, false
}

// newSyntheticXDB 构造覆盖完整地址空间的最小合法 XDB 测试内容。
func newSyntheticXDB(t *testing.T, version *xdb.Version) []byte {
	return newSyntheticXDBWithRegion(t, version, []byte(testXDBRegion))
}

// newSyntheticXDBWithRegion 构造使用指定归属地数据且覆盖完整地址空间的最小合法 XDB。
func newSyntheticXDBWithRegion(t *testing.T, version *xdb.Version, region []byte) []byte {
	t.Helper()
	if version == nil || (version.Id != xdb.IPv4VersionNo && version.Id != xdb.IPv6VersionNo) {
		t.Fatalf("不支持的合成 XDB 版本: %v", version)
	}
	if len(region) == 0 || len(region) > int(^uint16(0)) {
		t.Fatalf("合成 XDB 归属地长度无效: %d", len(region))
	}
	indexStart := xdbVectorEnd + len(region)
	indexLimit := indexStart + version.SegmentIndexSize
	content := make([]byte, indexLimit)

	binary.LittleEndian.PutUint16(content[0:], xdb.Structure30)
	binary.LittleEndian.PutUint16(content[2:], uint16(xdb.VectorIndexPolicy))
	binary.LittleEndian.PutUint32(content[8:], uint32(indexStart))
	binary.LittleEndian.PutUint32(content[12:], uint32(indexStart))
	binary.LittleEndian.PutUint16(content[16:], uint16(version.Id))
	binary.LittleEndian.PutUint16(content[18:], 4)
	for offset := xdb.HeaderInfoLength; offset < xdbVectorEnd; offset += xdb.VectorIndexSize {
		binary.LittleEndian.PutUint32(content[offset:], uint32(indexStart))
		binary.LittleEndian.PutUint32(content[offset+4:], uint32(indexLimit))
	}
	copy(content[xdbVectorEnd:], region)
	record := content[indexStart:indexLimit]
	for index := version.Bytes; index < version.Bytes*2; index++ {
		record[index] = 0xff
	}
	dataOffset := version.Bytes * 2
	binary.LittleEndian.PutUint16(record[dataOffset:], uint16(len(region)))
	binary.LittleEndian.PutUint32(record[dataOffset+2:], uint32(xdbVectorEnd))
	return content
}

// writeSyntheticXDB 把合成内容写入独立临时文件供真实加载路径验证。
func writeSyntheticXDB(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写入合成 XDB 失败: %v", err)
	}
	return path
}
