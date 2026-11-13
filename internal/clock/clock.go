package clock

import "time"

// Clock 是进程内唯一的时钟源。
//
// 参与判定与过期的时刻必须从这里取得；生产代码不要直接调用 time.Now()，
// 以便测试注入可控时间，也便于统一做时钟偏差观测。
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// System 返回系统时钟。整个进程只应通过依赖注入使用它。
func System() Clock { return systemClock{} }
