package storage

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"admin/internal/config"

	"github.com/Is999/go-utils/errors"
)

const (
	// defaultClamAVCommand 是未显式配置时使用的 ClamAV 客户端命令。
	defaultClamAVCommand = "clamdscan"
	// defaultClamAVTimeout 限制单次病毒扫描最长执行时间。
	defaultClamAVTimeout = 120 * time.Second
)

// NewVirusScanner 按配置创建病毒扫描器。
func NewVirusScanner(cfg config.FileStorageVirusScannerConfig) (VirusScanner, error) {
	name := strings.ToLower(strings.TrimSpace(cfg.Name))
	switch name {
	case "", config.VirusScannerNoop:
		return noopVirusScanner{}, nil
	case config.VirusScannerClamAV:
		return newClamAVScanner(cfg)
	default:
		return nil, errors.Errorf("不支持的病毒扫描器: %s", name)
	}
}

// clamAVScanner 使用 clamdscan 把文件内容流式发送给 ClamAV daemon。
type clamAVScanner struct {
	// command 是已解析的 clamdscan 可执行文件路径。
	command string
	// configFile 是 clamdscan 可选配置文件路径。
	configFile string
	// timeout 限制单次扫描时长。
	timeout time.Duration
}

// newClamAVScanner 创建 ClamAV 扫描器并确认客户端命令存在。
func newClamAVScanner(cfg config.FileStorageVirusScannerConfig) (VirusScanner, error) {
	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		command = defaultClamAVCommand
	}
	command, err := exec.LookPath(command)
	if err != nil {
		return nil, errors.Wrap(err, "查找 clamdscan 命令失败")
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultClamAVTimeout
	}
	return &clamAVScanner{
		command:    command,
		configFile: strings.TrimSpace(cfg.ConfigFile),
		timeout:    timeout,
	}, nil
}

// Ready 用空流完成一次真实扫描，确认 clamd 可连接且病毒库可用。
func (s *clamAVScanner) Ready(ctx context.Context) error {
	return s.scan(ctx, "-")
}

// ScanFile 扫描上传完成后的临时文件，任何扫描异常都阻断文件入库。
func (s *clamAVScanner) ScanFile(ctx context.Context, filePath string) error {
	if strings.TrimSpace(filePath) == "" {
		return errors.Errorf("病毒扫描文件路径不能为空")
	}
	return s.scan(ctx, filePath)
}

// scan 执行单次 clamdscan；退出码 1 表示发现病毒，其余异常均按扫描失败处理。
func (s *clamAVScanner) scan(ctx context.Context, filePath string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	scanCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	args := []string{"--no-summary", "--stream"}
	if s.configFile != "" {
		args = append(args, "--config-file="+s.configFile)
	}
	args = append(args, filePath)
	output, err := exec.CommandContext(scanCtx, s.command, args...).CombinedOutput()
	if err == nil {
		return nil
	}
	if scanCtx.Err() != nil {
		return errors.Wrap(scanCtx.Err(), "ClamAV 扫描超时或取消")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return errors.Errorf("文件未通过病毒扫描")
	}
	detail := strings.TrimSpace(string(output))
	if len(detail) > 512 {
		detail = detail[:512]
	}
	if detail != "" {
		return errors.Wrapf(err, "ClamAV 扫描失败: %s", detail)
	}
	return errors.Wrap(err, "ClamAV 扫描失败")
}

// noopVirusScanner 表示当前部署不执行病毒扫描。
type noopVirusScanner struct{}

// Ready 返回 noop 扫描器状态。
func (noopVirusScanner) Ready(context.Context) error {
	return nil
}

// ScanFile 不执行扫描。
func (noopVirusScanner) ScanFile(context.Context, string) error {
	return nil
}
