package server

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// recordReliableResult 把可靠下行报文的最终投递结果回填到 outbox。
// 由会话回调异步调用；对非 outbox 报文是无副作用的 no-op。
func recordReliableResult(commands repository.CommandRepository, result model.ReliableResult) {
	if commands == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var (
		updated bool
		err     error
	)
	switch result.Status {
	case model.ReliableStatusAcked:
		updated, err = commands.MarkAcked(ctx, result.Phone, result.MsgID, result.Serial)
	case model.ReliableStatusFailed:
		updated, err = commands.MarkFailed(ctx, result.Phone, result.MsgID, result.Serial, result.Reason)
	default:
		return
	}
	if err != nil {
		log.Error().Err(err).
			Str("phone", result.Phone).
			Uint16("msg_id", result.MsgID).
			Uint16("serial", result.Serial).
			Str("status", string(result.Status)).
			Msg("Failed to update command outbox state")
		return
	}
	if !updated {
		log.Debug().
			Str("phone", result.Phone).
			Uint16("msg_id", result.MsgID).
			Uint16("serial", result.Serial).
			Msg("Reliable result did not match a pending command")
	}
}
