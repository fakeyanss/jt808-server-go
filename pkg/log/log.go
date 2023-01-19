package log

import (
	"fmt"
	"os"
	"strings"
	"time"

	"strconv"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	LogDir = "./logs/" // todo: read from configuration
)

func Init() {
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
	err := os.MkdirAll(LogDir, os.ModePerm)
	if err != nil {
		fmt.Println("Failed to create log dir")
		os.Exit(1)
	}
	fileLogger := &lumberjack.Logger{
		Filename:   LogDir + "jt808-server-go.log",
		MaxSize:    5, //
		MaxBackups: 10,
		MaxAge:     14,
		LocalTime:  true,
		Compress:   false,
	}

	consoleLogger := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	consoleLogger.FormatLevel = func(i interface{}) string {
		return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
	}
	consoleLogger.FormatMessage = func(i interface{}) string {
		return fmt.Sprintf("%s |", i)
	}
	consoleLogger.FormatFieldName = func(i interface{}) string {
		return fmt.Sprintf("%s=", i)
	}
	consoleLogger.FormatFieldValue = func(i interface{}) string {
		return strings.ToUpper(fmt.Sprintf("%s", i))
	}

	multi := zerolog.MultiLevelWriter(consoleLogger, fileLogger)
	log.Logger = zerolog.New(multi).With().Timestamp().Caller().Logger()

	// zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	// zerolog.TimeFieldFormat = time.RFC3339Nano

	// logLevel, err := strconv.Atoi("1" /*os.Getenv("LOG_LEVEL")*/)
	// if err != nil {
	// 	logLevel = int(zerolog.InfoLevel) // default to INFO
	// }

	// var output io.Writer = zerolog.ConsoleWriter{
	// 	Out:        os.Stdout,
	// 	TimeFormat: time.RFC3339,
	// }

	// if "online" /*os.Getenv("APP_ENV")*/ != "development" {
	// 	fileLogger := &lumberjack.Logger{
	// 		Filename:   "jt808-server-go.log",
	// 		MaxSize:    5, //
	// 		MaxBackups: 10,
	// 		MaxAge:     14,
	// 		Compress:   true,
	// 	}

	// 	output = zerolog.MultiLevelWriter(os.Stderr, fileLogger)
	// }

	// log.Logger = zerolog.New(output).
	// 	Level(zerolog.Level(logLevel)).
	// 	With().
	// 	Timestamp().
	// 	Logger()
}
