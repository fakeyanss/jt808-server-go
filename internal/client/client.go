package client

type Server interface {
	Dial(addr string) error
	Start()
	Stop()
}
