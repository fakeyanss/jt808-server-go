package media

import "time"

// H.264 NAL 类型。
const (
	nalTypeIDR = 5
	nalTypeSPS = 7
	nalTypePPS = 8
)

// annexBStartCode 是补写参数集时统一使用的 4 字节起始码。
var annexBStartCode = []byte{0x00, 0x00, 0x00, 0x01}

// cachedParamSet 是服务级“最后参数集”缓存，用于暂停/重连后终端不重发参数集的场景。
type cachedParamSet struct {
	sps       []byte
	pps       []byte
	updatedAt time.Time
}

// paramSetCache 缓存单路流当前生效的 SPS/PPS。
// 参数集变化时直接替换；每个 IDR 前补齐当前集合，保证 GOP 可独立解码。
type paramSetCache struct {
	sps []byte
	pps []byte
}

// apply 在必要时为 IDR 帧前置 SPS/PPS，并更新缓存。
//
// 返回的 changed=true 表示本次观察到参数集变化。
// 当帧内只带 SPS 或只带 PPS 时，会整体前置缓存中的两组参数集，
// 以保持 SPS 在 PPS 之前的顺序；重复参数集对解码器是合法的。
func (c *paramSetCache) apply(frame []byte) (out []byte, changed bool) {
	hasIDR, hasSPS, hasPPS, changed := c.observe(frame)
	if !hasIDR || (hasSPS && hasPPS) {
		return frame, changed
	}
	var prefix []byte
	if !hasSPS {
		prefix = append(prefix, c.sps...)
	}
	if !hasPPS && len(c.pps) > 0 {
		// 帧内已有 SPS、只缺 PPS 时，为保证 SPS 在 PPS 之前，重复前置缓存 SPS。
		if hasSPS {
			prefix = append(prefix, c.sps...)
		}
		prefix = append(prefix, c.pps...)
	}
	if len(prefix) == 0 {
		return frame, changed
	}
	out = make([]byte, 0, len(prefix)+len(frame))
	out = append(out, prefix...)
	out = append(out, frame...)
	return out, changed
}

// observe 扫描帧内 NAL，更新缓存并返回是否包含 IDR/SPS/PPS 以及参数集是否变化。
func (c *paramSetCache) observe(frame []byte) (hasIDR, hasSPS, hasPPS, changed bool) {
	for _, nal := range splitAnnexB(frame) {
		switch nal.typ {
		case nalTypeIDR:
			hasIDR = true
		case nalTypeSPS:
			hasSPS = true
			if raw := withStartCode(nal.body); !equalBytes(c.sps, raw) {
				c.sps = raw
				changed = true
			}
		case nalTypePPS:
			hasPPS = true
			if raw := withStartCode(nal.body); !equalBytes(c.pps, raw) {
				c.pps = raw
				changed = true
			}
		}
	}
	return hasIDR, hasSPS, hasPPS, changed
}

// empty 表示缓存中没有任何参数集。
func (c *paramSetCache) empty() bool {
	return len(c.sps) == 0 && len(c.pps) == 0
}

// snapshot 返回参数集副本，供服务级缓存保存。
func (c *paramSetCache) snapshot() (sps, pps []byte) {
	return append([]byte(nil), c.sps...), append([]byte(nil), c.pps...)
}

// nalUnit 是一个 Annex B NAL 单元。
// body 引用原始帧数据，不复制；只有需要长期缓存的 SPS/PPS 才会被复制。
type nalUnit struct {
	typ  uint8
	body []byte
}

// splitAnnexB 按 3/4 字节起始码切分 Annex B 码流。
// 数据不以起始码开头时返回 nil，调用方按原样处理。
func splitAnnexB(data []byte) []nalUnit {
	start, scLen := nextAnnexBStart(data, 0)
	if start < 0 {
		return nil
	}
	units := make([]nalUnit, 0, 8)
	for start >= 0 {
		bodyStart := start + scLen
		next, nextLen := nextAnnexBStart(data, bodyStart)
		end := len(data)
		if next >= 0 {
			end = next
		}
		if end > bodyStart {
			units = append(units, nalUnit{typ: data[bodyStart] & 0x1f, body: data[bodyStart:end]})
		}
		if next < 0 {
			break
		}
		start, scLen = next, nextLen
	}
	return units
}

// withStartCode 把 NAL 体复制成带 4 字节起始码的独立切片。
func withStartCode(body []byte) []byte {
	out := make([]byte, 0, len(annexBStartCode)+len(body))
	out = append(out, annexBStartCode...)
	return append(out, body...)
}

// nextAnnexBStart 从 from 开始查找下一个 Annex B 起始码。
// 返回起始位置与起始码长度；紧邻的前导 0 视为 4 字节起始码的一部分。
func nextAnnexBStart(data []byte, from int) (pos, length int) {
	for i := from; i+3 <= len(data); i++ {
		if data[i] != 0x00 || data[i+1] != 0x00 || data[i+2] != 0x01 {
			continue
		}
		if i > 0 && data[i-1] == 0x00 {
			return i - 1, 4
		}
		return i, 3
	}
	return -1, 0
}

// isKeyframe 判断 Annex B 帧内是否含 IDR。
func isKeyframe(frame []byte) bool {
	for _, nal := range splitAnnexB(frame) {
		if nal.typ == nalTypeIDR {
			return true
		}
	}
	return false
}

// equalBytes 比较两段字节是否相同。
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
