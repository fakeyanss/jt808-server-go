package model

// 定义Packet Data
type Packet struct {
	Header     *MsgHeader // 消息头
	Body       []byte     // 消息体
	VerifyCode byte       // 校验码
}
