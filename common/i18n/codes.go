package i18n

import "admin_cron/common/codes"

// codeToMessageKey 维护业务响应码到多语言 key 的映射。
var codeToMessageKey = map[int]string{
	codes.Undefined:          MsgKeyUndefined,
	codes.Success:            MsgKeySuccess,
	codes.Fail:               MsgKeyFail,
	codes.CheckMFABind:       MsgKeyCheckMFABind,
	codes.CheckMFACode:       MsgKeyCheckMFA,
	codes.CheckPasswordReset: MsgKeyCheckPasswordReset,

	codes.Continue:     MsgKeyContinue,
	codes.OK:           MsgKeyOK,
	codes.BadRequest:   MsgKeyBadRequest,
	codes.Unauthorized: MsgKeyUnauthorized,
	codes.Forbidden:    MsgKeyForbidden,
	codes.NotFound:     MsgKeyNotFound,
	codes.ServerError:  MsgKeyServerError,
	codes.ServiceBusy:  MsgKeyServiceBusy,
	codes.Timeout:      MsgKeyTimeout,

	codes.ParamError:    MsgKeyParamError,
	codes.AuthFailed:    MsgKeyAuthFailed,
	codes.RateLimit:     MsgKeyRateLimit,
	codes.InternalError: MsgKeyInternalError,
	codes.DBError:       MsgKeyDBError,
	codes.CheckMFAAgain: MsgKeyMFAExpired,

	codes.CreateSuccess: MsgKeyCreateSuccess,
	codes.CreateFail:    MsgKeyCreateFail,
	codes.AddSuccess:    MsgKeyAddSuccess,
	codes.AddFail:       MsgKeyAddFail,
	codes.SaveSuccess:   MsgKeySaveSuccess,
	codes.SaveFail:      MsgKeySaveFail,
	codes.UpdateSuccess: MsgKeyUpdateSuccess,
	codes.UpdateFail:    MsgKeyUpdateFail,
	codes.DeleteSuccess: MsgKeyDeleteSuccess,
	codes.DeleteFail:    MsgKeyDeleteFail,
	codes.FetchSuccess:  MsgKeyFetchSuccess,
	codes.FetchFail:     MsgKeyFetchFail,

	codes.UserNotFound:      MsgKeyUserNotFound,
	codes.InvalidPassword:   MsgKeyInvalidPassword,
	codes.UserAlreadyExists: MsgKeyUserAlreadyExists,
	codes.UserDisabled:      MsgKeyUserDisabled,
	codes.InvalidCaptcha:    MsgKeyInvalidCaptcha,
	codes.InvalidMFACode:    MsgKeyMFACodeInvalid,

	codes.DependencyUnavailable: MsgKeyDependencyUnavailable,
	codes.MySQLUnavailable:      MsgKeyMySQLUnavailable,
	codes.RedisUnavailable:      MsgKeyRedisUnavailable,
	codes.ClickHouseUnavailable: MsgKeyClickHouseUnavailable,
	codes.KafkaUnavailable:      MsgKeyKafkaUnavailable,
	codes.TaskQueueUnavailable:  MsgKeyTaskQueueUnavailable,
	codes.CollectorUnavailable:  MsgKeyCollectorUnavailable,

	codes.UserTagWorkflowLeaseNotFound:      MsgKeyUserTagLeaseNotFound,
	codes.UserTagWorkflowLeaseOwnerMismatch: MsgKeyUserTagLeaseOwnerMismatch,
	codes.UserTagWorkflowLeaseReleaseFailed: MsgKeyUserTagLeaseReleaseFail,
}
