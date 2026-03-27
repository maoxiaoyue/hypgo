package errors

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// AbortWithAppError 中斷請求並回傳結構化錯誤
// 用於在 handler 中回傳預定義的 AppError
//
// 使用範例：
//
//	errors.AbortWithAppError(c, errors.ErrUserNotFound.With("id", 42))
func AbortWithAppError(c *hypcontext.Context, err *AppError) {
	c.AbortWithStatusJSON(err.HTTPStatus, err.JSON())
}

// RespondError 回傳錯誤但不中斷 middleware 鏈
func RespondError(c *hypcontext.Context, err *AppError) {
	c.JSON(err.HTTPStatus, err.JSON())
}
