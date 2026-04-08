package storage

import "testing"

// TestS3ResolveObjectKeyChecksBucket 确保 s3:// 地址必须属于当前 bucket。
func TestS3ResolveObjectKeyChecksBucket(t *testing.T) {
	store := &S3Storage{bucket: "admin-bucket", region: "ap-east-1"}
	key, err := store.ResolveObjectKey("s3://admin-bucket/export/demo.xlsx")
	if err != nil {
		t.Fatalf("解析当前 bucket 对象失败: %v", err)
	}
	if key != "export/demo.xlsx" {
		t.Fatalf("对象 key 不正确: %s", key)
	}
	if _, err = store.ResolveObjectKey("s3://other-bucket/export/demo.xlsx"); err == nil {
		t.Fatal("期望拒绝其他 bucket 的 s3 地址")
	}
}

// TestS3ResolveObjectKeyRejectsUntrustedURL 确保任意外部 URL 不会被当作对象 key。
func TestS3ResolveObjectKeyRejectsUntrustedURL(t *testing.T) {
	store := &S3Storage{bucket: "admin-bucket", region: "ap-east-1"}
	if _, err := store.ResolveObjectKey("https://evil.example.com/export/demo.xlsx"); err == nil {
		t.Fatal("期望拒绝不受信任 URL")
	}
}
