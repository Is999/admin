package tracing

import (
	"context"
	"testing"

	"admin/internal/config"
)

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
