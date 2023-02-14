package client

import (
	"bufio"
	"io"
	"net"

	"github.com/rs/zerolog/log"
)

type TCPClient struct {
	conn   net.Conn
	reader *bufio.Reader
	writer io.Writer
}

func NewTCPClient() *TCPClient {
	return &TCPClient{}
}

func (cli *TCPClient) Dial(addr string) error {
	conn, err := net.Dial("tcp", addr)

	if err == nil {
		cli.conn = conn
		cli.reader = bufio.NewReader(conn)
		cli.writer = conn
		log.Debug().Msgf("Dial to %v", addr)
	}

	return err
}

func (cli *TCPClient) Start() {
	// frameHandler := protocol.NewJT808FrameHandler(cli.conn)
	// packetCodec := protocol.NewJT808PacketCodec()

	for {
		// frame, err := frameHandler.Recv(context.Background())

		// if err != nil {
		// 	c.error <- err
		// 	break // TODO: find a way to recover from this
		// }

		// if cmd != nil {
		// 	switch v := cmd.(type) {
		// 	case protocol.MessageCommand:
		// 		c.incoming <- v
		// 	default:
		// 		log.Printf("Unknown command: %v", v)
		// 	}
		// }
	}
}

func (cli *TCPClient) Stop() {
	cli.conn.Close()
}
