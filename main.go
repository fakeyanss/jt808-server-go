package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fakeYanss/jt808-server-go/internal/server"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	TCP_PORT = "8080"
	UDP_PORT = "8081"
)

func main() {
	// 定义日志打印调用行
	zerolog.CallerMarshalFunc = func(pc uintptr, file string, line int) string {
		short := file
		for i := len(file) - 1; i > 0; i-- {
			if file[i] == '/' {
				short = file[i+1:]
				break
			}
		}
		file = short
		return file + ":" + strconv.Itoa(line)
	}
	// 定义输出格式
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	output.FormatLevel = func(i interface{}) string {
		return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
	}
	output.FormatMessage = func(i interface{}) string {
		return fmt.Sprintf("%s |", i)
	}
	output.FormatFieldName = func(i interface{}) string {
		return fmt.Sprintf("%s=", i)
	}
	output.FormatFieldValue = func(i interface{}) string {
		return strings.ToUpper(fmt.Sprintf("%s", i))
	}

	log.Logger = zerolog.New(output).With().Timestamp().Caller().Logger()

	serv := server.NewTcpServer()
	serv.Listen(":" + TCP_PORT)
	serv.Start()
}

// func ListenTCP() {
// 	tcpServer, err := net.Listen("tcp", HOST+":"+TCP_PORT)
// 	if err != nil {
// 		log.Fatal(err)
// 		os.Exit(1)
// 	}
// 	// close listener
// 	defer tcpServer.Close()
// 	session_cache := make(map[string]net.Conn)
// 	for {
// 		conn, err := tcpServer.Accept()
// 		id := uuid.NewString()
// 		session_cache[id] = conn

// 		if err != nil {
// 			log.Fatal(err)
// 			os.Exit(1)
// 		}
// 		go handleTCPConn(id, conn)
// 		go func() {
// 			fmt.Printf("monitor conn cache: %v\n", session_cache)
// 		}()
// 	}
// }

// func handleTCPConn(id string, conn net.Conn) {
// 	// close conn
// 	defer conn.Close()
// 	delete(session_cache, id)

// 	for {
// 		// incoming request
// 		buffer := make([]byte, 1024)
// 		_, err := conn.Read(buffer)
// 		if err != nil {
// 			log.Fatal(err)
// 		}

// 		// write data to response
// 		time := time.Now().Format(time.ANSIC)
// 		fmt.Printf("Receive message: %v", string(buffer))

// 		responseStr := fmt.Sprintf("Your message is: %v. Received time: %v\n", string(buffer), time)
// 		conn.Write([]byte(responseStr))
// 	}
// }

// func ListenUDP() {
// 	// listen to incoming udp packets
// 	udpServer, err := net.ListenPacket("udp", ":"+UDP_PORT)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer udpServer.Close()

// 	for {
// 		buf := make([]byte, 1024)
// 		_, addr, err := udpServer.ReadFrom(buf)
// 		if err != nil {
// 			continue
// 		}
// 		go handleUDPMsg(udpServer, addr, buf)
// 	}
// }

// func handleUDPMsg(udpServer net.PacketConn, addr net.Addr, buf []byte) {
// 	time := time.Now().Format(time.ANSIC)
// 	responseStr := fmt.Sprintf("time received: %v. Your message: %v!", time, string(buf))

// 	udpServer.WriteTo([]byte(responseStr), addr)
// }
