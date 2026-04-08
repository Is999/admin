package model

import (
	"admin_cron/internal/types"
	"strings"
	"time"

	"github.com/Is999/go-utils/errors"

	"gorm.io/gorm"
)

// TableNameAdminLog 管理员审计日志表名常量，统一供模型与查询复用。
const TableNameAdminLog = "admin_log"

// AdminLog 管理员操作日志。
// 这一版除了传统审计字段，还补充了 trace/span、HTTP 结果和耗时，便于把审计记录和运行日志串起来。
type AdminLog struct {
	ID           int       `gorm:"column:id;type:int unsigned;primaryKey;autoIncrement:true" json:"id"`                                                                // 日志主键 ID
	UserID       int       `gorm:"column:user_id;type:int unsigned;not null;comment:用户 ID" json:"user_id"`                                                             // 操作管理员 ID
	UserName     string    `gorm:"column:user_name;type:varchar(20);not null;index:idx_user_name,priority:1;comment:用户账户" json:"user_name"`                            // 操作管理员账号
	Action       string    `gorm:"column:action;type:varchar(100);not null;index:idx_action,priority:1;default:0;comment:动作名称" json:"action"`                          // 审计动作名称
	Route        string    `gorm:"column:route;type:varchar(255);not null;comment:路由名称" json:"route"`                                                                  // 请求路由
	Method       string    `gorm:"column:method;type:varchar(255);not null;comment:模块/类/方法" json:"method"`                                                             // 处理方法标识
	Describe     string    `gorm:"column:describe;type:varchar(255);not null;comment:描述" json:"describe"`                                                              // 操作描述
	Data         string    `gorm:"column:data;type:text;comment:操作数据" json:"data"`                                                                                     // 审计数据快照
	IP           string    `gorm:"column:ip;type:varchar(64);not null;comment:IP 地址" json:"ip"`                                                                        // 客户端 IP 地址
	Ipaddr       string    `gorm:"column:ipaddr;type:varchar(100);not null;comment:IP 地区信息" json:"ipaddr"`                                                             // IP 地区信息
	TraceID      string    `gorm:"column:trace_id;type:varchar(64);not null;default:'';index:idx_trace_id,priority:1;comment:Trace ID" json:"trace_id"`                // Trace ID
	SpanID       string    `gorm:"column:span_id;type:varchar(32);not null;default:'';comment:Span ID" json:"span_id"`                                                 // Span ID
	HTTPStatus   int       `gorm:"column:http_status;type:int;not null;default:200;comment:HTTP 状态码" json:"http_status"`                                               // HTTP 状态码
	BizCode      int       `gorm:"column:biz_code;type:int;not null;default:0;comment:业务码" json:"biz_code"`                                                            // 业务响应码
	LatencyMS    int64     `gorm:"column:latency_ms;type:bigint;not null;default:0;comment:请求耗时毫秒" json:"latency_ms"`                                                  // 请求耗时（毫秒）
	Success      bool      `gorm:"column:success;type:tinyint(1);not null;default:1;comment:是否成功" json:"success"`                                                      // 是否成功
	ErrorMessage string    `gorm:"column:error_message;type:varchar(500);not null;default:'';comment:错误信息" json:"error_message"`                                       // 错误摘要
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamp;not null;index:idx_created_at,priority:1;default:CURRENT_TIMESTAMP;comment:创建时间" json:"created_at"` // 创建时间
}

// TableName 返回管理员审计日志表名。
func (*AdminLog) TableName() string {
	return TableNameAdminLog
}

// AdminLogAction 定义管理员操作动作枚举，既用于审计落库，也用于后台筛选。
type AdminLogAction string

// 定义管理员操作日志的动作类型。
const (
	// 管理员登录相关。
	ActionAdminLogin  AdminLogAction = "管理员登录"
	ActionAdminLogout AdminLogAction = "管理员登出"

	// 管理员管理相关。
	ActionAdminAdd            AdminLogAction = "新增管理员"
	ActionAdminList           AdminLogAction = "查询管理员列表"
	ActionAdminInfo           AdminLogAction = "查询管理员详情"
	ActionAdminUpdate         AdminLogAction = "编辑管理员"
	ActionAdminDelete         AdminLogAction = "删除管理员"
	ActionAdminStatusUpdate   AdminLogAction = "修改管理员状态"
	ActionAdminPasswordReset  AdminLogAction = "重置管理员密码"
	ActionAdminRoleList       AdminLogAction = "查询管理员角色"
	ActionAdminRoleUpdate     AdminLogAction = "编辑管理员角色"
	ActionAdminRoleAdd        AdminLogAction = "添加管理员角色"
	ActionAdminRoleDelete     AdminLogAction = "解除管理员角色"
	ActionAdminExport         AdminLogAction = "导出管理员列表"
	ActionAdminExportStatus   AdminLogAction = "查询管理员导出进度"
	ActionAdminExportDownload AdminLogAction = "下载管理员导出文件"

	// 消息中心相关。
	ActionAdminMessageList        AdminLogAction = "查询消息列表"
	ActionAdminMessageSentList    AdminLogAction = "查询已发送消息"
	ActionAdminMessageReceivers   AdminLogAction = "查询消息收件人明细"
	ActionAdminMessageSend        AdminLogAction = "发送消息"
	ActionAdminMessageMarkRead    AdminLogAction = "标记消息已读"
	ActionAdminMessageDelete      AdminLogAction = "删除消息"
	ActionAdminMessageHandle      AdminLogAction = "标记消息已处理"
	ActionAdminMessageUnreadCount AdminLogAction = "查询未读消息数量"
	ActionAdminMessageNotifyList  AdminLogAction = "查询通知列表"

	// 角色与权限管理相关。
	ActionRoleList             AdminLogAction = "查询角色列表"
	ActionRoleAdd              AdminLogAction = "新增角色"
	ActionRoleUpdate           AdminLogAction = "编辑角色"
	ActionRoleDelete           AdminLogAction = "删除角色"
	ActionRoleStatusUpdate     AdminLogAction = "修改角色状态"
	ActionRolePermissionUpdate AdminLogAction = "编辑角色权限"
	ActionPermissionList       AdminLogAction = "查询权限列表"
	ActionPermissionAdd        AdminLogAction = "新增权限"
	ActionPermissionUpdate     AdminLogAction = "编辑权限"
	ActionPermissionDelete     AdminLogAction = "删除权限"
	ActionPermissionStatus     AdminLogAction = "修改权限状态"

	// 系统配置与缓存管理相关。
	ActionSysConfigList        AdminLogAction = "查询系统配置"
	ActionSysConfigAdd         AdminLogAction = "新增系统配置"
	ActionSysConfigUpdate      AdminLogAction = "编辑系统配置"
	ActionSysConfigExport      AdminLogAction = "导出系统配置"
	ActionSysConfigImport      AdminLogAction = "导入系统配置"
	ActionSysConfigCache       AdminLogAction = "查看系统配置缓存"
	ActionSysConfigRenew       AdminLogAction = "刷新系统配置缓存"
	ActionCacheList            AdminLogAction = "查询缓存列表"
	ActionCacheInfo            AdminLogAction = "查看缓存信息"
	ActionCacheSearch          AdminLogAction = "搜索缓存键"
	ActionCacheRenew           AdminLogAction = "刷新缓存"
	ActionCacheRenewAll        AdminLogAction = "刷新全部缓存"
	ActionCacheWarmup          AdminLogAction = "预热模板缓存"
	ActionSecretKeyList        AdminLogAction = "查询秘钥列表"
	ActionSecretKeyGet         AdminLogAction = "查询秘钥详情"
	ActionSecretKeyAdd         AdminLogAction = "新增秘钥"
	ActionSecretKeyUpdate      AdminLogAction = "编辑秘钥"
	ActionSecretKeyStatus      AdminLogAction = "修改秘钥状态"
	ActionSecretKeyRenew       AdminLogAction = "刷新秘钥缓存"
	ActionSecretKeyValidate    AdminLogAction = "预检秘钥配置"
	ActionSecretKeySelfCheck   AdminLogAction = "执行秘钥自检"
	ActionSecurityDebugSign    AdminLogAction = "安全调试签名"
	ActionSecurityDebugVerify  AdminLogAction = "安全调试验签"
	ActionSecurityDebugEncrypt AdminLogAction = "安全调试加密"
	ActionSecurityDebugDecrypt AdminLogAction = "安全调试解密"

	// 任务、日志与用户标签相关。
	ActionTaskEnqueue                 AdminLogAction = "手动投递任务"
	ActionAdminLogQuery               AdminLogAction = "查询管理员操作日志"
	ActionTaskInfoGet                 AdminLogAction = "查询任务详情"
	ActionTaskItemsList               AdminLogAction = "查询任务列表"
	ActionTaskRun                     AdminLogAction = "立即执行任务"
	ActionTaskDelete                  AdminLogAction = "删除任务"
	ActionTaskWorkflowTrigger         AdminLogAction = "手动触发工作流"
	ActionTaskWorkflowStatus          AdminLogAction = "查询工作流状态"
	ActionTaskQueueList               AdminLogAction = "查询任务队列概览"
	ActionTaskConfigReloadItems       AdminLogAction = "查询配置热加载配置项"
	ActionTaskConfigReloadStatus      AdminLogAction = "查询配置热加载状态"
	ActionTaskConfigReloadRun         AdminLogAction = "手动触发配置热加载"
	ActionTaskQueuePause              AdminLogAction = "暂停任务队列"
	ActionTaskQueueResume             AdminLogAction = "恢复任务队列"
	ActionUserTagWorkflowTrigger      AdminLogAction = "触发用户标签工作流"
	ActionUserTagRecalculate          AdminLogAction = "指定标签重新计算"
	ActionUserTagWorkflowLeaseRelease AdminLogAction = "释放用户标签工作流互斥锁"

	// 通用收集器相关。
	ActionCollectorOverview AdminLogAction = "查询Collector概览"
	ActionCollectorTaskList AdminLogAction = "查询Collector任务"
	ActionCollectorRun      AdminLogAction = "手动执行Collector"
	ActionCollectorRetry    AdminLogAction = "手动重试Collector任务"
)

// ListAdminLog 分页查询管理员日志。
func ListAdminLog(db *gorm.DB, req *types.AdminLogQueryReq) ([]AdminLog, int64, error) {
	page, pageSize, err := validatePage(req.Page, req.PageSize)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}

	dbq := db.Model(&AdminLog{})
	// trace_id 过滤用于把一次请求产生的访问日志、错误日志和审计日志串联起来排查问题。
	if req.TraceID != "" {
		dbq = dbq.Where("trace_id = ?", req.TraceID)
	}
	if req.UserID != nil {
		dbq = dbq.Where("user_id = ?", *req.UserID)
	}
	if req.UserName != "" {
		dbq = dbq.Where("user_name = ?", req.UserName)
	}
	if req.Action != "" {
		dbq = dbq.Where("action = ?", req.Action)
	}

	var total int64
	if err := dbq.Count(&total).Error; err != nil {
		return nil, 0, errors.Tag(err)
	}

	var logs []AdminLog
	if total == 0 {
		return logs, 0, nil
	}

	dbq, err = applyAdminLogOrder(dbq, req.OrderBy, req.Order)
	if err != nil {
		return nil, 0, errors.Tag(err)
	}

	err = dbq.Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs).Error
	return logs, total, errors.Tag(err)
}

// applyAdminLogOrder 统一处理管理员日志列表排序。
// 未显式传排序字段时，默认按创建时间倒序、ID 倒序返回，保证日志管理页始终最新记录在前。
func applyAdminLogOrder(db *gorm.DB, orderBy, order string) (*gorm.DB, error) {
	if strings.TrimSpace(orderBy) == "" {
		return db.Order("created_at DESC").Order("id DESC"), nil
	}
	return applySafeOrder(db, orderBy, order)
}

// CreateAdminLog 创建管理员操作日志记录。
func CreateAdminLog(db *gorm.DB, m *AdminLog) error {
	return db.Create(m).Error
}
