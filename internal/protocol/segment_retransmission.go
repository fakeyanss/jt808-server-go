package protocol

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

const (
	segmentRetransmissionIdleTimeout = 300 * time.Millisecond
	segmentRetransmissionMaxAttempts = 3
)

type segmentRetransmissionState struct {
	session     *model.Session
	segment     *model.Segment
	firstSerial uint16
	header      model.MsgHeader
	timer       *time.Timer
	attempts    int
}

var segmentRetransmissions = struct {
	sync.Mutex
	states map[string]*segmentRetransmissionState
}{states: make(map[string]*segmentRetransmissionState)}

func segmentRetransmissionKey(sessionID, phone string, msgID, firstSerial uint16) string {
	return fmt.Sprintf("%s/%s/%04x/%04x", sessionID, phone, msgID, firstSerial)
}

func scheduleSegmentRetransmission(session *model.Session, packet *model.PacketData) {
	if session == nil || packet == nil || packet.Header == nil || packet.Header.Frag == nil {
		return
	}
	segment := model.NewSegment(packet)
	firstSerial := storage.FirstFragmentSerial(packet.Header.SerialNumber, segment.SegNo)
	segment.SegNo = 1
	segment.Data = nil
	key := segmentRetransmissionKey(session.ID, segment.Phone, segment.MsgID, firstSerial)
	header := *packet.Header
	attr := *packet.Header.Attr
	header.Attr = &attr
	segmentRetransmissions.Lock()
	state := segmentRetransmissions.states[key]
	if state == nil {
		state = &segmentRetransmissionState{session: session, segment: segment, firstSerial: firstSerial, header: header}
		segmentRetransmissions.states[key] = state
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	state.timer = time.AfterFunc(segmentRetransmissionIdleTimeout, func() {
		requestMissingSegments(key, state)
	})
	segmentRetransmissions.Unlock()
}

func cancelSegmentRetransmission(session *model.Session, phone string, msgID, firstSerial uint16) {
	if session == nil {
		return
	}
	key := segmentRetransmissionKey(session.ID, phone, msgID, firstSerial)
	segmentRetransmissions.Lock()
	if state := segmentRetransmissions.states[key]; state != nil {
		if state.timer != nil {
			state.timer.Stop()
		}
		delete(segmentRetransmissions.states, key)
	}
	segmentRetransmissions.Unlock()
}

func requestMissingSegments(key string, state *segmentRetransmissionState) {
	segmentRetransmissions.Lock()
	if segmentRetransmissions.states[key] != state {
		segmentRetransmissions.Unlock()
		return
	}
	state.timer = nil
	segmentRetransmissions.Unlock()

	missing := storage.MissingSegmentIndexes(state.segment, state.firstSerial)
	if len(missing) == 0 {
		cancelSegmentRetransmission(state.session, state.segment.Phone, state.segment.MsgID, state.firstSerial)
		return
	}

	segmentRetransmissions.Lock()
	if segmentRetransmissions.states[key] != state {
		segmentRetransmissions.Unlock()
		return
	}
	if state.attempts >= segmentRetransmissionMaxAttempts {
		delete(segmentRetransmissions.states, key)
		segmentRetransmissions.Unlock()
		log.Warn().Str("device", state.segment.Phone).Uint16("msg_id", state.segment.MsgID).Uint16("original_serial", state.firstSerial).Msg("Fragment retransmission attempt limit reached")
		return
	}
	state.attempts++
	attempt := state.attempts
	state.timer = time.AfterFunc(segmentRetransmissionIdleTimeout, func() {
		requestMissingSegments(key, state)
	})
	segmentRetransmissions.Unlock()

	header := state.header
	attr := *state.header.Attr
	attr.PacketFragmented = 0
	attr.PacketFragmentedDesc = model.PacketFragmentedFalse
	header.Attr = &attr
	header.Frag = nil
	header.MsgID = 0x8003
	header.SerialNumber = state.session.GetNextSerialNum()
	request := &model.Msg8003{
		Header:               &header,
		OriginalSerialNumber: state.firstSerial,
		RetransmitCount:      uint16(len(missing)),
		PacketIDs:            missing,
	}
	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, state.session)
	ctx = context.WithValue(ctx, model.ProcessDataCtxKey{}, &model.ProcessData{Outgoing: request})
	if err := NewPipeline(state.session.Conn).ProcessConnWrite(ctx); err != nil {
		log.Error().Err(err).Str("device", state.segment.Phone).Int("attempt", attempt).Msg("Failed to send fragmented-message retransmission request")
	}
}
