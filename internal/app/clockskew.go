package app

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

const (
	// clockSkewProbeInterval 是时钟偏差探针的执行间隔。
	clockSkewProbeInterval = time.Minute
	// clockSkewThreshold 是告警阈值。探针只观测、不参与任何判定；
	// 超过该值说明进程时钟与数据库/NTP 明显失步，应检查时间同步。
	clockSkewThreshold = 2 * time.Second
)

// startClockSkewProbe 周期性与数据库时间比对一次，超阈值时记录告警指标。
//
// 它只用于观测：所有租约与过期判定仍使用调用方传入的进程时刻，
// 探针结果不会反馈到任何业务分支，避免观测链路故障影响主流程。
// memory 后端没有数据库时钟，探针直接跳过；该类部署应由 NTP 保证同步。
func (d *deps) startClockSkewProbe() {
	storeSet := d.storeSet
	metrics := d.metrics
	clk := d.clock
	if storeSet == nil || metrics == nil || clk == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	d.cancelClockSkew = cancel
	d.clockSkewDone = done

	routines.GoSafe(func() {
		defer close(done)
		ticker := time.NewTicker(clockSkewProbeInterval)
		defer ticker.Stop()
		probe := func() {
			queryCtx, queryCancel := context.WithTimeout(ctx, 5*time.Second)
			defer queryCancel()
			before := clk.Now()
			dbMillis, ok, err := storeSet.CurrentTimeMillis(queryCtx)
			after := clk.Now()
			if err != nil {
				log.Warn().Err(err).Msg("Clock skew probe failed to read database time")
				return
			}
			if !ok {
				// memory 后端没有数据库时钟，避免每分钟刷一条无意义日志。
				cancel()
				return
			}
			// 取查询前后进程时间的中点，抵消一部分网络往返误差。
			mid := before.Add(after.Sub(before) / 2)
			skew := time.Duration(dbMillis-mid.UnixMilli()) * time.Millisecond
			metrics.RecordClockSkew(skew)
			if skew > clockSkewThreshold || skew < -clockSkewThreshold {
				metrics.IncClockSkewAlert()
				log.Warn().
					Dur("skew", skew).
					Dur("threshold", clockSkewThreshold).
					Msg("Clock skew exceeds threshold; check NTP/chrony on this host")
			}
		}
		probe()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probe()
			}
		}
	})
}
