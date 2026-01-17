package errors

import (
	"errors"
	"fmt"
)

// ErrorType 定义错误类型
type ErrorType string

const (
	// 通用错误类型
	TypeGeneric ErrorType = "generic"

	// 配置相关错误
	TypeConfig ErrorType = "config"

	// 网络相关错误
	TypeNetwork ErrorType = "network"

	// Git相关错误
	TypeGit ErrorType = "git"

	// 权限相关错误
	TypePermission ErrorType = "permission"

	// 路径相关错误
	TypePath ErrorType = "path"

	// 资源相关错误
	TypeResource ErrorType = "resource"

	// 并发相关错误
	TypeConcurrency ErrorType = "concurrency"

	// 同步相关错误
	TypeSync ErrorType = "sync"

	// 仓库相关错误
	TypeRepo ErrorType = "repo"
)

// RepoError 自定义错误类型
type RepoError struct {
	Type    ErrorType
	Message string
	Cause   error
	Code    string // 错误代码，便于程序判断
}

// Error 实现error接口
func (e *RepoError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap 返回底层错误
func (e *RepoError) Unwrap() error {
	return e.Cause
}

// IsType 检查错误类型
func (e *RepoError) IsType(errorType ErrorType) bool {
	return e.Type == errorType
}

// NewConfigError 创建配置错误
func NewConfigError(message string, cause error) *RepoError {
	return &RepoError{
		Type:    TypeConfig,
		Message: message,
		Cause:   cause,
		Code:    "CONFIG_ERROR",
	}
}

// NewGitError 创建Git错误
func NewGitError(message string, cause error) *RepoError {
	return &RepoError{
		Type:    TypeGit,
		Message: message,
		Cause:   cause,
		Code:    "GIT_ERROR",
	}
}

// NewNetworkError 创建网络错误
func NewNetworkError(message string, cause error) *RepoError {
	return &RepoError{
		Type:    TypeNetwork,
		Message: message,
		Cause:   cause,
		Code:    "NETWORK_ERROR",
	}
}

// NewSyncError 创建同步错误
func NewSyncError(message string, cause error) *RepoError {
	return &RepoError{
		Type:    TypeSync,
		Message: message,
		Cause:   cause,
		Code:    "SYNC_ERROR",
	}
}

// NewConcurrencyError 创建并发错误
func NewConcurrencyError(message string, cause error) *RepoError {
	return &RepoError{
		Type:    TypeConcurrency,
		Message: message,
		Cause:   cause,
		Code:    "CONCURRENCY_ERROR",
	}
}

// New 创建新的通用错误
func New(message string) error {
	return &RepoError{
		Type:    TypeGeneric,
		Message: message,
		Code:    "GENERIC_ERROR",
	}
}

// NewWithType 创建指定类型的错误
func NewWithType(typ ErrorType, message string) error {
	return &RepoError{
		Type:    typ,
		Message: message,
		Code:    string(typ) + "_ERROR",
	}
}

// NewWithCode 创建带错误代码的错误
func NewWithCode(code string, message string) error {
	return &RepoError{
		Type:    TypeGeneric,
		Message: message,
		Code:    code,
	}
}

// Wrap 包装现有错误
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}
	return &RepoError{
		Type:    TypeGeneric,
		Message: message,
		Cause:   err,
		Code:    "WRAPPED_ERROR",
	}
}

// WrapWithType 包装错误并指定类型
func WrapWithType(err error, typ ErrorType, message string) error {
	if err == nil {
		return nil
	}
	return &RepoError{
		Type:    typ,
		Message: message,
		Cause:   err,
		Code:    string(typ) + "_WRAPPED",
	}
}

// Is 判断错误类型
func Is(err error, typ ErrorType) bool {
	var repoErr *RepoError
	if errors.As(err, &repoErr) {
		return repoErr.Type == typ
	}
	return false
}

// IsCode 判断错误代码
func IsCode(err error, code string) bool {
	var repoErr *RepoError
	if errors.As(err, &repoErr) {
		return repoErr.Code == code
	}
	return false
}

// GetCode 获取错误代码
func GetCode(err error) string {
	var repoErr *RepoError
	if errors.As(err, &repoErr) {
		return repoErr.Code
	}
	return ""
}

// GetType 获取错误类型
func GetType(err error) ErrorType {
	var repoErr *RepoError
	if errors.As(err, &repoErr) {
		return repoErr.Type
	}
	return TypeGeneric
}

// IsConfigError 是否为配置错误
func IsConfigError(err error) bool {
	return Is(err, TypeConfig)
}

// IsNetworkError 是否为网络错误
func IsNetworkError(err error) bool {
	return Is(err, TypeNetwork)
}

// IsGitError 是否为Git错误
func IsGitError(err error) bool {
	return Is(err, TypeGit)
}

// IsPermissionError 是否为权限错误
func IsPermissionError(err error) bool {
	return Is(err, TypePermission)
}

// IsPathError 是否为路径错误
func IsPathError(err error) bool {
	return Is(err, TypePath)
}

// IsResourceError 是否为资源错误
func IsResourceError(err error) bool {
	return Is(err, TypeResource)
}
