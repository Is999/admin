package mysqlx

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"admin_cron/internal/config"
)

// TestResolveDataSources 验证对应场景。
func TestResolveDataSources(t *testing.T) {
	t.Run("write_data_source required", func(t *testing.T) {
		writeDSN, readDSNs, err := resolveDataSources(config.MySQLConfig{
			WriteDataSource: "root:pwd@tcp(127.0.0.1:3306)/primary",
		})
		if err != nil {
			t.Fatalf("resolveDataSources returned error: %v", err)
		}
		if writeDSN != "root:pwd@tcp(127.0.0.1:3306)/primary" {
			t.Fatalf("unexpected writeDSN: %s", writeDSN)
		}
		if len(readDSNs) != 0 {
			t.Fatalf("expected no readDSNs, got %+v", readDSNs)
		}
	})

	t.Run("normalize and deduplicate read_data_sources", func(t *testing.T) {
		writeDSN, readDSNs, err := resolveDataSources(config.MySQLConfig{
			WriteDataSource: "root:pwd@tcp(127.0.0.1:3306)/primary",
			ReadDataSources: []string{
				"",
				"root:pwd@tcp(127.0.0.1:3306)/replica1",
				"root:pwd@tcp(127.0.0.1:3306)/replica1",
				"root:pwd@tcp(127.0.0.1:3306)/primary",
				"root:pwd@tcp(127.0.0.1:3306)/replica2",
			},
		})
		if err != nil {
			t.Fatalf("resolveDataSources returned error: %v", err)
		}
		if writeDSN != "root:pwd@tcp(127.0.0.1:3306)/primary" {
			t.Fatalf("unexpected writeDSN: %s", writeDSN)
		}
		expected := []string{
			"root:pwd@tcp(127.0.0.1:3306)/replica1",
			"root:pwd@tcp(127.0.0.1:3306)/replica2",
		}
		if !reflect.DeepEqual(readDSNs, expected) {
			t.Fatalf("expected readDSNs %+v, got %+v", expected, readDSNs)
		}
	})

	t.Run("missing write datasource", func(t *testing.T) {
		_, _, err := resolveDataSources(config.MySQLConfig{})
		if err == nil {
			t.Fatalf("expected error when write_data_source is empty")
		}
	})
}

// TestCheckMySQLDataSourcesPingsEveryDataSource 确保启动探测覆盖写库和每一个读库。
func TestCheckMySQLDataSourcesPingsEveryDataSource(t *testing.T) {
	// got 记录探测函数的实际调用顺序，用于确认写库和读库都被覆盖。
	var got []string
	err := checkMySQLDataSourcesWithPing(
		context.Background(),
		"write-dsn",
		[]string{"read-dsn-1", "read-dsn-2"},
		func(_ context.Context, label, dsn string) error {
			got = append(got, label+"="+dsn)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("checkMySQLDataSourcesWithPing returned error: %v", err)
	}

	// expected 描述启动期探测必须遵守的顺序：写库先行，读库逐个验证。
	expected := []string{
		"write_data_source=write-dsn",
		"read_data_sources[0]=read-dsn-1",
		"read_data_sources[1]=read-dsn-2",
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected ping sequence %+v, got %+v", expected, got)
	}
}

// TestValidateMySQLDataSourceDatabase 确保 DSN 必须声明默认数据库。
func TestValidateMySQLDataSourceDatabase(t *testing.T) {
	t.Run("with database", func(t *testing.T) {
		err := validateMySQLDataSourceDatabase("write_data_source", "root:pwd@tcp(127.0.0.1:3306)/primary?charset=utf8mb4&parseTime=true&loc=Local")
		if err != nil {
			t.Fatalf("validateMySQLDataSourceDatabase returned error: %v", err)
		}
	})

	t.Run("missing database", func(t *testing.T) {
		err := validateMySQLDataSourceDatabase("write_data_source", "root:pwd@tcp(127.0.0.1:3306)/?charset=utf8mb4")
		if err == nil {
			t.Fatalf("expected error when database name is empty")
		}
		if !strings.Contains(err.Error(), "必须包含数据库名") {
			t.Fatalf("expected database name error, got %v", err)
		}
	})

	t.Run("bad dsn", func(t *testing.T) {
		err := validateMySQLDataSourceDatabase("write_data_source", "%%%%")
		if err == nil {
			t.Fatalf("expected error when DSN is invalid")
		}
		if !strings.Contains(err.Error(), "解析 MySQL write_data_source DSN 失败") {
			t.Fatalf("expected parse error, got %v", err)
		}
	})
}
