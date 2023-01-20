package logger

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"strconv"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	LogDir  = "./logs/" // todo: read from configuration
	LogFile = LogDir + "jt808-server-go.log"
)

var stdErrFileHandler *os.File

func Init() {
	// ConosleLogger
	consoleLogger := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	zerologFormat(&consoleLogger)

	// FileLogger
	makeLogDir()
	rotateOut := rotatePolicy()
	fileLogger := zerolog.ConsoleWriter{Out: &rotateOut, TimeFormat: time.RFC3339}
	zerologFormat(&fileLogger)

	multi := zerolog.MultiLevelWriter(consoleLogger, fileLogger)

	zerologConfiguration()

	log.Logger = zerolog.New(multi).
		Level(zerolog.DebugLevel).
		With().
		Timestamp().
		Caller().
		Logger()

	err := rewriteStderrFile()
	if err != nil {
		log.Error().Err(err).Msg("Failed to rewite stderr")
	}
}

func makeLogDir() {
	err := os.MkdirAll(LogDir, os.ModePerm)
	if err != nil {
		fmt.Println("Failed to create log dir")
		os.Exit(1)
	}
}

func rotatePolicy() lumberjack.Logger {
	fileLogger := &lumberjack.Logger{
		Filename:   LogFile,
		MaxSize:    1, //
		MaxBackups: 10,
		MaxAge:     14,
		LocalTime:  true,
		Compress:   false,
	}
	return *fileLogger
}

func zerologConfiguration() {
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
	// 定义时间精度
	zerolog.TimeFieldFormat = time.RFC3339Nano

	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
}

func zerologFormat(logger *zerolog.ConsoleWriter) {
	logger.FormatLevel = func(i interface{}) string {
		return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
	}
	logger.FormatMessage = func(i interface{}) string {
		return fmt.Sprintf("%s |", i)
	}
	logger.FormatFieldName = func(i interface{}) string {
		return fmt.Sprintf("%s=", i)
	}
	logger.FormatFieldValue = func(i interface{}) string {
		return fmt.Sprintf("%s", i)
	}
}

func rewriteStderrFile() error {
	if runtime.GOOS == "windows" {
		return nil
	}

	file, err := os.OpenFile(LogFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println(err)
		return err
	}
	stdErrFileHandler = file //把文件句柄保存到全局变量，避免被GC回收

	if err = syscall.Dup2(int(file.Fd()), int(os.Stderr.Fd())); err != nil {
		fmt.Println(err)
		return err
	}
	// 内存回收前关闭文件描述符
	runtime.SetFinalizer(stdErrFileHandler, func(fd *os.File) {
		fd.Close()
	})

	return nil
}
