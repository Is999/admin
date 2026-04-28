package logic

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	// AdminSuperRoleID 表示超级管理员角色 ID。
	AdminSuperRoleID = 1
)

// formatDateTime 把数据库时间统一转换成前端使用的日期时间字符串。
func formatDateTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateTime)
}

// buildTreePids 根据父级 ID 和父级族谱生成当前节点族谱。
func buildTreePids(parentID int, parentPids string) string {
	if parentID <= 0 {
		return ""
	}
	parentPids = strings.TrimSpace(parentPids)
	if parentPids == "" {
		return strconv.Itoa(parentID)
	}
	return fmt.Sprintf("%s,%d", parentPids, parentID)
}

// applyGenealogyScopeFilter 使用 MySQL `FIND_IN_SET` 过滤指定祖先节点下的全部子孙节点。
// 当前项目的 `pids` 以逗号分隔祖先链存储，统一收口到这里，避免各业务线继续散落低效模糊匹配。
func applyGenealogyScopeFilter(db *gorm.DB, genealogyField string, id int) *gorm.DB {
	if db == nil || id <= 0 {
		return db
	}
	genealogyField = strings.TrimSpace(genealogyField)
	if genealogyField == "" {
		genealogyField = "pids"
	}
	return db.Where(fmt.Sprintf("FIND_IN_SET(?, %s)", genealogyField), id)
}

// containsTreeID 判断逗号分隔族谱中是否包含指定 ID。
func containsTreeID(pids string, id int) bool {
	if id <= 0 {
		return false
	}
	target := strconv.Itoa(id)
	for _, part := range strings.Split(pids, ",") {
		if strings.TrimSpace(part) == target {
			return true
		}
	}
	return false
}

// normalizedOrderField 把前端常见小驼峰排序字段映射成数据库字段。
func normalizedOrderField(orderBy string, fallback string) string {
	switch strings.TrimSpace(orderBy) {
	case "createdAt":
		return "created_at"
	case "updatedAt":
		return "updated_at"
	case "realName":
		return "real_name"
	case "lastLoginTime":
		return "last_login_time"
	case "lastLoginIP":
		return "last_login_ip"
	case "":
		return fallback
	default:
		return orderBy
	}
}

// normalizedOrderDirection 归一化排序方向，非法值统一回落到降序。
func normalizedOrderDirection(order string) string {
	if strings.ToLower(strings.TrimSpace(order)) == "asc" {
		return "asc"
	}
	return "desc"
}

// intPtrDefault 返回指针整数值；指针为空时使用默认值。
func intPtrDefault(value *int, defaultValue int) int {
	if value == nil {
		return defaultValue
	}
	return *value
}

// FormatDateTime 把数据库时间统一转换成前端使用的日期时间字符串。
func FormatDateTime(t time.Time) string {
	return formatDateTime(t)
}

// BuildTreePids 根据父级 ID 和父级族谱生成当前节点族谱。
func BuildTreePids(parentID int, parentPids string) string {
	return buildTreePids(parentID, parentPids)
}

// ApplyGenealogyScopeFilter 使用 MySQL `FIND_IN_SET` 过滤指定祖先节点下的全部子孙节点。
func ApplyGenealogyScopeFilter(db *gorm.DB, genealogyField string, id int) *gorm.DB {
	return applyGenealogyScopeFilter(db, genealogyField, id)
}

// ContainsTreeID 判断逗号分隔族谱中是否包含指定 ID。
func ContainsTreeID(pids string, id int) bool {
	return containsTreeID(pids, id)
}

// NormalizedOrderField 把前端常见小驼峰排序字段映射成数据库字段。
func NormalizedOrderField(orderBy string, fallback string) string {
	return normalizedOrderField(orderBy, fallback)
}

// NormalizedOrderDirection 归一化排序方向，非法值统一回落到降序。
func NormalizedOrderDirection(order string) string {
	return normalizedOrderDirection(order)
}

// IntPtrDefault 返回指针整数值；指针为空时使用默认值。
func IntPtrDefault(value *int, defaultValue int) int {
	return intPtrDefault(value, defaultValue)
}

// FreshTxStatement 基于当前事务创建干净语句上下文。
func FreshTxStatement(tx *gorm.DB) *gorm.DB {
	if tx == nil {
		return nil
	}
	return tx.Session(&gorm.Session{NewDB: true})
}
