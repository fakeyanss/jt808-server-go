package adminapi

import "github.com/gin-gonic/gin"

// 管理面统一错误响应格式：所有失败响应都使用同一套 code + message 结构，
// 便于调用方按 code 分支处理，而不依赖具体的 HTTP 状态码文本。
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// 统一错误码。调用方应以此为准，HTTP 状态码只表达类别。
const (
	codeUnauthorized = "unauthorized"
	codeRateLimited  = "rate_limited"
	codeBadRequest   = "bad_request"
	codeNotFound     = "not_found"
	codeConflict     = "conflict"
	codeUnavailable  = "unavailable"
	codeInternal     = "internal"
)

// writeJSONError 输出统一 JSON 错误响应并终止后续处理。
func writeJSONError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorBody{Code: code, Message: message})
}

// writeJSONOK 输出统一成功响应。
func writeJSONOK(c *gin.Context, payload any) {
	c.JSON(200, payload)
}
