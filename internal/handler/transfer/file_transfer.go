package transfer

import (
	"net/http"
	"strconv"
	"strings"

	"admin/common/codes"
	i18n "admin/common/i18n"
	"admin/helper"
	"admin/internal/handler/shared"
	filelogic "admin/internal/logic/file"
	"admin/internal/svc"
	"admin/internal/types"
	"admin/pkg/transfer"

	"github.com/Is999/go-utils/errors"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// InitFileUploadHandler 初始化断点续传上传会话。
func InitFileUploadHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandler(func(r *http.Request, svcCtx *svc.ServiceContext, req *types.FileUploadInitReq) (shared.LogicObj, *types.BizResult) {
		logicObj := filelogic.NewFileTransferLogic(r, svcCtx)
		return logicObj, logicObj.InitUpload(req)
	})(sCtx)
}

// GetFileUploadStatusHandler 查询断点续传上传状态。
func GetFileUploadStatusHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return shared.RespHandler(func(r *http.Request, svcCtx *svc.ServiceContext, req *types.FileUploadStatusReq) (shared.LogicObj, *types.BizResult) {
		logicObj := filelogic.NewFileTransferLogic(r, svcCtx)
		return logicObj, logicObj.GetUploadStatus(req)
	})(sCtx)
}

// CompleteFileUploadHandler 完成断点续传上传会话。
func CompleteFileUploadHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := parseFileUploadCompleteReq(r)
		if err != nil {
			shared.WriteBizResponse(w, r, nil, shared.ParamErrorResult(err), nil)
			return
		}
		logicObj := filelogic.NewFileTransferLogic(r, sCtx)
		resp := logicObj.CompleteUpload(req)
		resp.WithReq(req)
		shared.WriteBizResponse(w, r, logicObj, resp, nil)
	}
}

// UploadFileChunkHandler 上传单个文件分片。
func UploadFileChunkHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := parseFileUploadChunkReq(r)
		if err != nil {
			shared.WriteBizResponse(w, r, nil, shared.ParamErrorResult(err), nil)
			return
		}
		logicObj := filelogic.NewFileTransferLogic(r, sCtx)
		resp := logicObj.UploadChunk(req, r.Body)
		resp.WithReq(req)
		shared.WriteBizResponse(w, r, logicObj, resp, nil)
	}
}

// DownloadUploadedFileHandler 下载当前管理员上传完成的文件。
func DownloadUploadedFileHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FileUploadStatusReq
		if err := httpx.Parse(r, &req); err != nil {
			shared.WriteBizResponse(w, r, nil, shared.ParamErrorResult(err), nil)
			return
		}
		logicObj := filelogic.NewFileTransferLogic(r, sCtx)
		session, err := logicObj.PrepareDownload(req.UploadID)
		if err != nil {
			resp := buildFileTransferAccessResp(err, "DownloadUploadedFileHandler 准备下载上传会话失败")
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, nil)
			return
		}
		objectStream, err := logicObj.OpenSessionObject(session, r.Header.Get("Range"))
		if err != nil {
			resp := types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
				"DownloadUploadedFileHandler 打开上传会话[%s]对象失败", req.UploadID).ToBizResult()
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, nil)
			return
		}
		defer objectStream.Reader.Close()
		if err := transfer.ServeStream(
			w,
			r,
			objectStream.Reader,
			helper.FirstNonEmptyString(session.StoredFileName, session.FileName, objectStream.FileName),
			helper.FirstNonEmptyString(session.ContentType, objectStream.ContentType),
			objectStream.ContentLength,
			"attachment",
			objectStream.AcceptRanges,
			objectStream.ContentRange,
		); err != nil {
			resp := types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
				"DownloadUploadedFileHandler 输出上传会话[%s]文件失败", req.UploadID).ToBizResult()
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, nil)
		}
	}
}

// AccessUploadedFileHandler 公开访问允许匿名预览的上传文件。
func AccessUploadedFileHandler(sCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FileUploadStatusReq
		if err := httpx.Parse(r, &req); err != nil {
			shared.WriteBizResponse(w, r, nil, shared.ParamErrorResult(err), nil)
			return
		}
		logicObj := filelogic.NewFileTransferLogic(r, sCtx)
		session, err := logicObj.PreparePublicAccess(req.UploadID)
		if err != nil {
			resp := buildFileTransferAccessResp(err, "AccessUploadedFileHandler 访问上传会话失败")
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, nil)
			return
		}
		objectStream, err := logicObj.OpenSessionObject(session, r.Header.Get("Range"))
		if err != nil {
			resp := types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
				"AccessUploadedFileHandler 打开上传会话[%s]对象失败", req.UploadID).ToBizResult()
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, nil)
			return
		}
		defer objectStream.Reader.Close()
		if err := transfer.ServeStream(
			w,
			r,
			objectStream.Reader,
			helper.FirstNonEmptyString(session.StoredFileName, session.FileName, objectStream.FileName),
			helper.FirstNonEmptyString(session.ContentType, objectStream.ContentType),
			objectStream.ContentLength,
			"inline",
			objectStream.AcceptRanges,
			objectStream.ContentRange,
		); err != nil {
			resp := types.ServerError(i18n.MsgKeyInternalErrorFormat, err,
				"AccessUploadedFileHandler 输出上传会话[%s]文件失败", req.UploadID).ToBizResult()
			resp.WithReq(&req)
			shared.WriteBizResponse(w, r, logicObj, resp, nil)
		}
	}
}

// parseFileUploadChunkReq 显式从 query/form 读取分片上传参数，避免二进制请求体场景下 `httpx.Parse` 漏掉 URL 参数。
func parseFileUploadChunkReq(r *http.Request) (*types.FileUploadChunkReq, error) {
	req := &types.FileUploadChunkReq{}

	// 分片上传请求体是二进制流，这里优先直接读取 URL query，避免 body 解析逻辑干扰参数绑定。
	req.UploadID = strings.TrimSpace(r.URL.Query().Get("uploadId"))
	chunkIndexText := strings.TrimSpace(r.URL.Query().Get("chunkIndex"))

	// 对部分代理或客户端改写场景，补一层 ParseForm 支持 form/query 合并读取。
	if req.UploadID == "" || chunkIndexText == "" {
		if err := r.ParseForm(); err == nil {
			if req.UploadID == "" {
				req.UploadID = strings.TrimSpace(r.FormValue("uploadId"))
			}
			if chunkIndexText == "" {
				chunkIndexText = strings.TrimSpace(r.FormValue("chunkIndex"))
			}
		}
	}

	if strings.TrimSpace(chunkIndexText) != "" {
		chunkIndex, err := strconv.Atoi(strings.TrimSpace(chunkIndexText))
		if err != nil {
			return nil, errors.Errorf("chunkIndex 格式不合法")
		}
		req.ChunkIndex = chunkIndex
	}
	if err := req.Validate(); err != nil {
		return nil, errors.Tag(err)
	}
	return req, nil
}

// parseFileUploadCompleteReq 显式读取完成上传请求参数，优先读取 query 传参，必要时再回退到通用 body 解析。
func parseFileUploadCompleteReq(r *http.Request) (*types.FileUploadCompleteReq, error) {
	req := &types.FileUploadCompleteReq{
		UploadID: strings.TrimSpace(r.URL.Query().Get("uploadId")),
	}

	// 完成上传接口允许 query/body 两种提交方式；query 缺失时再回退到通用解析，支持 query/body 两种提交方式。
	if req.UploadID == "" {
		if err := httpx.Parse(r, req); err != nil {
			return nil, errors.Tag(err)
		}
	}
	if err := req.Validate(); err != nil {
		return nil, errors.Tag(err)
	}
	return req, nil
}

// buildFileTransferAccessResp 把上传文件访问场景的常见业务错误映射为稳定的对外响应码。
func buildFileTransferAccessResp(err error, context string) *types.BizResult {
	if err == nil {
		return types.NewBizResult(codes.ServerError).SetI18nMessage(i18n.MsgKeyServerError)
	}
	message := strings.TrimSpace(err.Error())
	switch {
	case strings.Contains(message, "无权访问"), strings.Contains(message, "未登录"):
		return types.Forbidden(i18n.MsgKeyForbidden).ToBizResult().WithError(err)
	case strings.Contains(message, "上传会话不存在"),
		strings.Contains(message, "尚未完成"),
		strings.Contains(message, "不允许公开访问"),
		strings.Contains(message, "不能为空"):
		return types.NewBizResult(codes.ParamError).
			SetI18nMessage(i18n.MsgKeyParamErrorFormat, message).
			WithError(err)
	default:
		return types.ServerError(i18n.MsgKeyInternalErrorFormat, err, context).ToBizResult()
	}
}
