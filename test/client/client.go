package main

import "github.com/fakeYanss/jt808-server-go/pkg/logger"

const (
	LogDir  = "./logs/" // todo: read from configuration
	LogFile = "jt808-client-go.log"
)

func main() {
	logger.Init(LogDir, LogFile)
}
