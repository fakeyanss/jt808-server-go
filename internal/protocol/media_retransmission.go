package protocol

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

const (
	mediaRetransmissionIdleTimeout = 300 * time.Millisecond
	mediaRetransmissionMaxAttempts = 3
)

type mediaRetransmissionState struct {
	session     *model.Session
	segment     *model.Segment
	firstSerial uint16
	mediaID     uint32
	header      model.MsgHeader
	timer       *time.Timer
	attempts    int
}

var mediaRetransmissions = struct {
	sync.Mutex
	states map[string]*mediaRetransmissionState
}{states: make(map[string]*mediaRetransmissionState)}

func mediaRetransmissionKey(sessionID, phone string, msgID, firstSerial uint16) string {
	return fmt.Sprintf("%s/%s/%04x/%04x", sessionID, phone, msgID, firstSerial)
}

// scheduleMediaRetransmission registers an in-progress media upload (0x0801)
// for missing-packet detection. The first fragment's body carries the
// MultiMediaID; later fragments rely on the state cached by the first one.
func scheduleMediaRetransmission(session *model.Session, packet *model.PacketData) {
	if session == nil || packet == nil || packet.Header == nil || packet.Header.Frag == nil {
		return
	}
	segment := model.NewSegment(packet)
	firstSerial := runtime.FirstFragmentSerial(packet.Header.SerialNumber, segment.SegNo)
	segment.SegNo = 1
	segment.Data = nil
	key := mediaRetransmissionKey(session.ID, segment.Phone, segment.MsgID, firstSerial)

	mediaID := uint32(0)
	if packet.Header.Frag.Index == 1 && len(packet.Body) >= 4 {
		idx := 0
		mediaID = hex.ReadDoubleWord(packet.Body, &idx)
	}

	header := *packet.Header
	attr := *packet.Header.Attr
	header.Attr = &attr

	mediaRetransmissions.Lock()
	state := mediaRetransmissions.states[key]
	if state == nil {
		state = &mediaRetransmissionState{
			session:     session,
			segment:     segment,
			firstSerial: firstSerial,
			mediaID:     mediaID,
			header:      header,
		}
		mediaRetransmissions.states[key] = state
	}
	if mediaID != 0 {
		state.mediaID = mediaID
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	state.timer = time.AfterFunc(mediaRetransmissionIdleTimeout, func() {
		requestMissingMediaPackets(key, state)
	})
	mediaRetransmissions.Unlock()
}

func cancelMediaRetransmission(session *model.Session, phone string, msgID, firstSerial uint16) {
	if session == nil {
		return
	}
	key := mediaRetransmissionKey(session.ID, phone, msgID, firstSerial)
	mediaRetransmissions.Lock()
	if state := mediaRetransmissions.states[key]; state != nil {
		if state.timer != nil {
			state.timer.Stop()
		}
		delete(mediaRetransmissions.states, key)
	}
	mediaRetransmissions.Unlock()
}

func requestMissingMediaPackets(key string, state *mediaRetransmissionState) {
	mediaRetransmissions.Lock()
	if mediaRetransmissions.states[key] != state {
		mediaRetransmissions.Unlock()
		return
	}
	state.timer = nil
	mediaRetransmissions.Unlock()

	missing := runtime.Default().Fragment.MissingIndexes(context.Background(), state.segment, state.firstSerial)
	if len(missing) == 0 {
		cancelMediaRetransmission(state.session, state.segment.Phone, state.segment.MsgID, state.firstSerial)
		return
	}

	mediaRetransmissions.Lock()
	if mediaRetransmissions.states[key] != state {
		mediaRetransmissions.Unlock()
		return
	}
	if state.attempts >= mediaRetransmissionMaxAttempts {
		delete(mediaRetransmissions.states, key)
		mediaRetransmissions.Unlock()
		log.Warn().Str("device", state.segment.Phone).Uint32("media_id", state.mediaID).Msg("Media retransmission attempt limit reached")
		return
	}
	state.attempts++
	state.timer = time.AfterFunc(mediaRetransmissionIdleTimeout, func() {
		requestMissingMediaPackets(key, state)
	})
	mediaRetransmissions.Unlock()

	sendMediaRetransmissionRequest(state, missing)
}

// sendMediaRetransmissionRequest prefers 0x8800 (media-specific) when the
// MultiMediaID is known. If the first fragment (which carries the ID) has not
// arrived yet, it falls back to 0x8003 so the missing packet can still be
// recovered without mixing the two mechanisms on the same media ID.
func sendMediaRetransmissionRequest(state *mediaRetransmissionState, missing []uint16) {
	header := state.header
	attr := *state.header.Attr
	attr.PacketFragmented = 0
	attr.PacketFragmentedDesc = model.PacketFragmentedFalse
	header.Attr = &attr
	header.Frag = nil
	header.SerialNumber = state.session.GetNextSerialNum()

	var outgoing model.JT808Msg
	if state.mediaID != 0 {
		header.MsgID = 0x8800
		outgoing = &model.Msg8800{
			Header:        &header,
			MultiMediaID:  state.mediaID,
			RetransmitIDs: missing,
		}
	} else {
		header.MsgID = 0x8003
		outgoing = &model.Msg8003{
			Header:               &header,
			OriginalSerialNumber: state.firstSerial,
			RetransmitCount:      uint16(len(missing)),
			PacketIDs:            missing,
		}
	}

	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, state.session)
	ctx = context.WithValue(ctx, model.ProcessDataCtxKey{}, &model.ProcessData{Outgoing: outgoing})
	if err := NewPipeline(state.session.Conn).ProcessConnWrite(ctx); err != nil {
		log.Error().Err(err).Str("device", state.segment.Phone).Uint32("media_id", state.mediaID).Msg("Failed to send media retransmission request")
	}
}
