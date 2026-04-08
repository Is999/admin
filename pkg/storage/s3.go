package storage

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"admin_cron/internal/config"

	"github.com/Is999/go-utils/errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Storage 表示基于 AWS S3 的统一文件存储实现。
type S3Storage struct {
	bucket        string            // bucket 名称
	region        string            // 区域
	domain        string            // 自定义访问域名
	pathPrefix    string            // S3 对象路径统一前缀
	presignExpire time.Duration     // 预签名有效期
	client        *s3.Client        // S3 客户端
	presignClient *s3.PresignClient // S3 预签名客户端
}

// NewS3Storage 创建 S3 文件存储实现。
func NewS3Storage(cfg config.FileStorageConfig) (*S3Storage, error) {
	if !cfg.S3.Enabled {
		return nil, errors.Errorf("S3 存储未启用")
	}
	if strings.TrimSpace(cfg.S3.Bucket) == "" {
		return nil, errors.Errorf("S3 bucket 不能为空")
	}
	if strings.TrimSpace(cfg.S3.Region) == "" {
		return nil, errors.Errorf("S3 region 不能为空")
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(strings.TrimSpace(cfg.S3.Region)),
	}
	accessKey := strings.TrimSpace(cfg.S3.AccessKey)
	secretKey := strings.TrimSpace(cfg.S3.SecretKey)
	if accessKey != "" || secretKey != "" {
		if accessKey == "" || secretKey == "" {
			return nil, errors.Errorf("S3 access_key 和 secret_key 必须同时配置")
		}
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOptions...)
	if err != nil {
		return nil, errors.Wrap(err, "初始化 S3 配置失败")
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.S3.UsePathStyle
		if endpoint := strings.TrimSpace(cfg.S3.Endpoint); endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
	})
	presignExpire := time.Duration(cfg.S3.PresignExpireSeconds) * time.Second
	if presignExpire <= 0 {
		presignExpire = 15 * time.Minute
	}
	return &S3Storage{
		bucket:        strings.TrimSpace(cfg.S3.Bucket),
		region:        strings.TrimSpace(cfg.S3.Region),
		domain:        trimTrailingSlash(cfg.S3.Domain),
		pathPrefix:    strings.TrimSpace(cfg.S3.PathPrefix),
		presignExpire: presignExpire,
		client:        client,
		presignClient: s3.NewPresignClient(client),
	}, nil
}

// Type 返回当前存储类型。
func (s *S3Storage) Type() string {
	return TypeS3
}

// SaveLocalFile 把本地文件上传到 S3。
func (s *S3Storage) SaveLocalFile(ctx context.Context, req SaveLocalFileReq) (*StoredObject, error) {
	ctx = nonNilContext(ctx)
	info, err := fileInfoFromPath(req.LocalPath)
	if err != nil {
		return nil, errors.Tag(err)
	}
	file, err := os.Open(strings.TrimSpace(req.LocalPath))
	if err != nil {
		return nil, errors.Wrap(err, "打开待上传文件失败")
	}
	defer file.Close()
	return s.putObject(ctx, saveObjectInput{
		bizType:          req.BizType,
		body:             file,
		contentType:      DetectContentType(req.OriginalFileName, req.ContentType),
		originalFileName: req.OriginalFileName,
		size:             info.Size(),
		storedFileName:   req.StoredFileName,
		visibility:       req.Visibility,
	})
}

// SaveContent 把内容流直接写入 S3。
func (s *S3Storage) SaveContent(ctx context.Context, req SaveContentReq) (*StoredObject, error) {
	ctx = nonNilContext(ctx)
	return s.putObject(ctx, saveObjectInput{
		bizType:          req.BizType,
		body:             req.Body,
		contentType:      DetectContentType(req.OriginalFileName, req.ContentType),
		originalFileName: req.OriginalFileName,
		size:             req.Size,
		storedFileName:   req.StoredFileName,
		visibility:       req.Visibility,
	})
}

// CreateDirectUpload 生成浏览器直传 S3 的 PUT 预签名。
func (s *S3Storage) CreateDirectUpload(ctx context.Context, req DirectUploadReq) (*DirectUploadPlan, error) {
	if s == nil || s.presignClient == nil {
		return nil, errors.Errorf("S3 预签名客户端未初始化")
	}
	ctx = nonNilContext(ctx)
	objectKey := buildObjectKey(s.pathPrefix, req.BizType, strings.TrimSpace(req.StoredFileName))
	contentType := DetectContentType(req.OriginalFileName, req.ContentType)
	presignResult, err := s.presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(objectKey),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(req.FileSize),
	}, func(options *s3.PresignOptions) {
		options.Expires = s.presignExpire
	})
	if err != nil {
		return nil, errors.Wrap(err, "生成 S3 直传预签名失败")
	}
	return &DirectUploadPlan{
		Method:    "PUT",
		URL:       presignResult.URL,
		Headers:   map[string]string{"Content-Type": contentType},
		ExpiresAt: time.Now().Add(s.presignExpire).Format("2006-01-02 15:04:05"),
		ObjectKey: objectKey,
	}, nil
}

// CompleteDirectUpload 通过 HeadObject 校验前端直传结果并返回对象信息。
func (s *S3Storage) CompleteDirectUpload(ctx context.Context, objectKey string) (*StoredObject, error) {
	if s == nil || s.client == nil {
		return nil, errors.Errorf("S3 文件存储未初始化")
	}
	ctx = nonNilContext(ctx)
	objectKey, err := s.ResolveObjectKey(objectKey)
	if err != nil {
		return nil, errors.Tag(err)
	}
	headResult, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return nil, errors.Wrap(err, "读取 S3 对象信息失败")
	}
	contentType := defaultContentType
	if headResult.ContentType != nil && strings.TrimSpace(*headResult.ContentType) != "" {
		contentType = strings.TrimSpace(*headResult.ContentType)
	}
	contentLength := int64(0)
	if headResult.ContentLength != nil {
		contentLength = *headResult.ContentLength
	}
	return &StoredObject{
		StorageType: TypeS3,
		ObjectKey:   objectKey,
		FileName:    filepath.Base(objectKey),
		ContentType: contentType,
		Size:        contentLength,
		PublicURL:   "",
	}, nil
}

// Open 打开 S3 对象流。
func (s *S3Storage) Open(ctx context.Context, req OpenObjectReq) (*OpenObjectResult, error) {
	if s == nil || s.client == nil {
		return nil, errors.Errorf("S3 文件存储未初始化")
	}
	ctx = nonNilContext(ctx)
	objectKey, err := s.ResolveObjectKey(req.ObjectKey)
	if err != nil {
		return nil, errors.Tag(err)
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	}
	if strings.TrimSpace(req.RangeHeader) != "" {
		input.Range = aws.String(strings.TrimSpace(req.RangeHeader))
	}
	getResult, err := s.client.GetObject(ctx, input)
	if err != nil {
		return nil, errors.Wrap(err, "读取 S3 对象失败")
	}
	contentType := defaultContentType
	if getResult.ContentType != nil && strings.TrimSpace(*getResult.ContentType) != "" {
		contentType = strings.TrimSpace(*getResult.ContentType)
	}
	lastModified := time.Time{}
	if getResult.LastModified != nil {
		lastModified = *getResult.LastModified
	}
	contentLength := int64(0)
	if getResult.ContentLength != nil {
		contentLength = *getResult.ContentLength
	}
	return &OpenObjectResult{
		Reader:        getResult.Body,
		ContentType:   contentType,
		ContentLength: contentLength,
		FileName:      filepath.Base(objectKey),
		LastModified:  lastModified,
		AcceptRanges:  strings.TrimSpace(aws.ToString(getResult.AcceptRanges)) != "",
		ContentRange:  strings.TrimSpace(aws.ToString(getResult.ContentRange)),
	}, nil
}

// Delete 删除 S3 对象，供上传校验失败时回滚使用。
func (s *S3Storage) Delete(ctx context.Context, objectKey string) error {
	if s == nil || s.client == nil {
		return errors.Errorf("S3 文件存储未初始化")
	}
	ctx = nonNilContext(ctx)
	objectKey, err := s.ResolveObjectKey(objectKey)
	if err != nil {
		return errors.Tag(err)
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	}); err != nil {
		return errors.Wrap(err, "删除 S3 对象失败")
	}
	return nil
}

// ResolveObjectKey 把对象 key、公开 URL 或 s3:// URL 统一解析成 bucket 内 key。
func (s *S3Storage) ResolveObjectKey(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.Errorf("对象标识不能为空")
	}
	if objectKey, ok := parseURLObjectKey(raw, s.domain); ok {
		raw = objectKey
	}
	if strings.HasPrefix(strings.ToLower(raw), "s3://") {
		parsedURL, err := url.Parse(raw)
		if err != nil {
			return "", errors.Wrap(err, "解析 S3 对象地址失败")
		}
		if !strings.EqualFold(strings.TrimSpace(parsedURL.Host), s.bucket) {
			return "", errors.Errorf("S3 对象 bucket 不匹配")
		}
		raw = strings.TrimLeft(parsedURL.Path, "/")
	}
	if parsedURL, err := url.Parse(raw); err == nil && parsedURL.Scheme != "" && parsedURL.Host != "" {
		// 仅允许当前 bucket、自定义域名或显式配置 endpoint，避免把任意外部 URL 当作对象 key 解析。
		if !s.isTrustedObjectHost(parsedURL.Host) {
			return "", errors.Errorf("对象地址不受信任")
		}
		raw = strings.TrimLeft(parsedURL.Path, "/")
		if strings.HasPrefix(raw, s.bucket+"/") {
			raw = strings.TrimPrefix(raw, s.bucket+"/")
		}
	}
	raw, err := sanitizeResolvedObjectKey(raw)
	if err != nil {
		return "", errors.Tag(err)
	}
	return raw, nil
}

// saveObjectInput 表示统一写入 S3 时使用的内部输入参数。
type saveObjectInput struct {
	bizType          string    // 业务类型
	body             io.Reader // 对象内容流
	contentType      string    // 文件 MIME
	originalFileName string    // 原始文件名
	size             int64     // 文件大小
	storedFileName   string    // 存储文件名
	visibility       string    // 文件可见性
}

// putObject 把文件内容写入 S3，并按可见性决定是否生成公开访问地址。
func (s *S3Storage) putObject(ctx context.Context, input saveObjectInput) (*StoredObject, error) {
	if s == nil || s.client == nil {
		return nil, errors.Errorf("S3 文件存储未初始化")
	}
	if input.body == nil {
		return nil, errors.Errorf("S3 文件存储写入流不能为空")
	}
	if input.size < 0 {
		return nil, errors.Errorf("S3 文件大小不能为负数")
	}
	objectKey := buildObjectKey(s.pathPrefix, input.bizType, strings.TrimSpace(input.storedFileName))
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(objectKey),
		Body:          input.body,
		ContentType:   aws.String(input.contentType),
		ContentLength: aws.Int64(input.size),
	})
	if err != nil {
		return nil, errors.Wrap(err, "上传 S3 对象失败")
	}
	publicURL := ""
	if normalizeVisibility(input.visibility) == VisibilityPublic {
		publicURL = s.objectURL(objectKey)
	}
	return &StoredObject{
		StorageType: TypeS3,
		ObjectKey:   objectKey,
		FileName:    filepath.Base(objectKey),
		ContentType: input.contentType,
		Size:        input.size,
		PublicURL:   publicURL,
	}, nil
}

// objectURL 返回对象对外访问地址；仅在 public 语义下透出给上层。
func (s *S3Storage) objectURL(objectKey string) string {
	objectKey = strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	if s.domain != "" {
		return combineURL(s.domain, objectKey)
	}
	return "https://" + s.bucket + ".s3." + s.region + ".amazonaws.com/" + objectKey
}

// isTrustedObjectHost 校验对象 URL 主机是否属于当前配置的 S3 域名范围。
func (s *S3Storage) isTrustedObjectHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if s.domain != "" {
		if parsedDomain, err := url.Parse(s.domain); err == nil && strings.EqualFold(strings.TrimSpace(parsedDomain.Host), host) {
			return true
		}
	}
	if s.client != nil {
		if endpoint := aws.ToString(s.client.Options().BaseEndpoint); strings.TrimSpace(endpoint) != "" {
			if parsedEndpoint, err := url.Parse(endpoint); err == nil && strings.EqualFold(strings.TrimSpace(parsedEndpoint.Host), host) {
				return true
			}
		}
	}
	return host == strings.ToLower(s.bucket+".s3."+s.region+".amazonaws.com") ||
		host == strings.ToLower("s3."+s.region+".amazonaws.com")
}

var _ ObjectStorage = (*S3Storage)(nil)
