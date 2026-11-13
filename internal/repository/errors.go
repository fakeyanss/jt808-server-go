package repository

import "errors"

// 仓储层哨兵错误。内存实现与 SQL 实现必须返回同一组错误值，
// 调用方用 errors.Is 判断，不依赖具体后端。
var (
	ErrDeviceNotFound             = errors.New("device not found")
	ErrGisNotFound                = errors.New("gis not found")
	ErrDeviceParamsNotFound       = errors.New("device params not found")
	ErrTerminalPropertiesNotFound = errors.New("terminal properties not found")
)
