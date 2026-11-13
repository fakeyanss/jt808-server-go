package contracts

import "errors"

// ErrFTPLoginRejected 表示 FTP 一次性账号无效、已吊销或任务已终态。
//
// 附件节点用它区分“账号被拒”（530）与“control 不可达”（421/503）：
// 前者不应重试，后者必须继续重试但不得放行新登录。
var ErrFTPLoginRejected = errors.New("ftp login rejected")
