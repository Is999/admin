package tracing

import (
	"context"
	"testing"

	"admin/internal/config"

	"go.opentelemetry.io/otel"
)

// TestSetupDisabledSkipsExporter 验证关闭 trace 时不会初始化无效的 OTLP exporter。
func TestSetupDisabledSkipsExporter(t *testing.T) {
	shutdown, err := Setup(context.Background(), config.ObservabilityConfig{
		TraceEnabled: false,
		OTLPProtocol: "invalid",
		OTLPEndpoint: "blackhole.invalid:4317",
		SampleRatio:  1,
	})
	if err != nil {
		t.Fatalf("Setup(disabled) error = %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })
}

// TestSetupPreservesZeroSampleRatio 验证显式零采样不会被静默改成全量采样。
func TestSetupPreservesZeroSampleRatio(t *testing.T) {
	shutdown, err := Setup(context.Background(), config.ObservabilityConfig{
		TraceEnabled: true,
		SampleRatio:  0,
	})
	if err != nil {
		t.Fatalf("Setup(sample=0) error = %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	_, span := otel.Tracer("test").Start(context.Background(), "zero-sample")
	defer span.End()
	if span.IsRecording() {
		t.Fatal("sample_ratio=0 should not record spans")
	}
}

// TestNormalizeOTLPProtocolRejectsHTTPJSON 验证 OTLP HTTP 不把 JSON 误当作 protobuf 协议。
func TestNormalizeOTLPProtocolRejectsHTTPJSON(t *testing.T) {
	if got := normalizeOTLPProtocol("http/json"); got == "http" {
		t.Fatalf("http/json should not normalize to protobuf HTTP: %q", got)
	}
}

// TestSetupWithHTTPOTLP 验证对应场景。
func TestSetupWithHTTPOTLP(t *testing.T) {
	shutdown, err := Setup(context.Background(), config.ObservabilityConfig{
		TraceEnabled: true,
		SampleRatio:  1,
		OTLPProtocol: "http/protobuf",
		OTLPEndpoint: "opentelemetry-collector.telemetry-system:4318",
		OTLPInsecure: true,
	})
	if err != nil {
		t.Fatalf("初始化 tracing 失败: %v", err)
	}
	_ = shutdown(context.Background())
}

// TestSetupWithHTTPOTLPURL 验证对应场景。
func TestSetupWithHTTPOTLPURL(t *testing.T) {
	shutdown, err := Setup(context.Background(), config.ObservabilityConfig{
		TraceEnabled: true,
		SampleRatio:  1,
		OTLPProtocol: "http/protobuf",
		OTLPEndpoint: "http://opentelemetry-collector.telemetry-system:4318/v1/traces",
		OTLPInsecure: true,
	})
	if err != nil {
		t.Fatalf("初始化 tracing 失败: %v", err)
	}
	_ = shutdown(context.Background())
}

// TestSetupWithUnknownOTLPProtocol 验证对应场景。
func TestSetupWithUnknownOTLPProtocol(t *testing.T) {
	_, err := Setup(context.Background(), config.ObservabilityConfig{
		TraceEnabled: true,
		SampleRatio:  1,
		OTLPProtocol: "unknown",
		OTLPEndpoint: "opentelemetry-collector.telemetry-system:4318",
		OTLPInsecure: true,
	})
	if err == nil {
		t.Fatalf("期望返回错误")
	}
}
