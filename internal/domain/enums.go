// Package domain 定义跨协议、持久化与管理面共用的领域实体。
//
// 包内类型不依赖具体报文结构：协议报文在 internal/protocol/model，
// 二者通过显式适配构造器衔接，避免领域实体与线格式相互绑死。
package domain

// VersionType 是 JT/T 808 协议版本描述，区分 2011 / 2013 / 2019。
type VersionType int8

const (
	Version2011 VersionType = 0
	Version2013 VersionType = 1
	Version2019 VersionType = 2
)

// TransportProtocol 是终端接入使用的传输层协议。
type TransportProtocol string

const (
	TCPProto TransportProtocol = "TCP"
	UDPProto TransportProtocol = "UDP"
)
