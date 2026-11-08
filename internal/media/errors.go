package media

import "errors"

// 1078 媒体载荷的哨兵错误。调用方可用 errors.Is 区分“数据不完整”与“正常结束”。
var (
	// ErrTruncatedPayload 表示载荷不完整：RTP 分片缺片、PS/PES 头被截断，
	// 或 PES 声明长度与实际字节数不符。调用方应丢弃当前帧，但可继续接收后续帧。
	ErrTruncatedPayload = errors.New("media payload truncated")

	// ErrFrameTooLarge 表示单个重组帧超过大小上限，属于异常终端数据，应丢弃当前帧。
	ErrFrameTooLarge = errors.New("media frame exceeds size limit")

	// ErrCorruptPayload 表示载荷结构损坏：未知分包类型、PES 内无可识别的 Annex B 起始码等。
	ErrCorruptPayload = errors.New("media payload corrupt")

	// ErrUnsupportedPayload 表示载荷形态不受支持，例如既非 MPEG-PS 也非 Annex B。
	ErrUnsupportedPayload = errors.New("unsupported media payload")
)
