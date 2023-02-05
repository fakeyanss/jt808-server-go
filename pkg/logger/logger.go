package logger

import (
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
	"gopkg.in/natefinch/lumberjack.v2"
)

// 全局变量，避免gc
var stdErrFileHandler *os.File

// Configuration for logging
type Config struct {
	// Enable console logging
	ConsoleLoggingEnabled bool

	// EncodeLogsAsJSON makes the framework log JSON
	EncodeLogsAsJSON bool

	// FileLoggingEnabled makes the framework log to a file
	// the fields below can be skipped if this value is false!
	FileLoggingEnabled bool

	// LogLevel uses the zerolog.Level definition
	LogLevel int

	// Directory to log to to when filelogging is enabled
	Directory string

	// Filename is the name of the logfile which will be placed inside the directory
	Filename string

	// MaxSize the max size in MB of the logfile before it's rolled
	MaxSize int

	// MaxBackups the max number of rolled files to keep
	MaxBackups int

	// MaxAge the max age in days to keep a logfile
	MaxAge int
}

type Logger struct {
	*zerolog.Logger
}

// Configure sets up the logging framework
//
// In production, the container logs will be collected and file logging should be disabled. However,
// during development it's nicer to see logs as text and optionally write to a file when debugging
// problems in the containerized pipeline
//
// The output log file will be located at /var/log/service-xyz/service-xyz.log and
// will be rolled according to configuration set.
func Configure(config *Config) *Logger {
	var writers []io.Writer

	if config.ConsoleLoggingEnabled {
		writers = append(writers, newConsoleWriter(config, os.Stderr))
	}
	if config.FileLoggingEnabled {
		fw := newRollingFile(config)
		if config.EncodeLogsAsJSON {
			writers = append(writers, fw)
		} else {
			writers = append(writers, newConsoleWriter(config, fw))
		}
	}
	mw := io.MultiWriter(writers...)

	logger := zerolog.New(mw).
		Level(zerolog.Level(config.LogLevel)).
		With().
		Caller().
		Timestamp().
		Logger()

	zerologGlobalConfiguration()

	// err := rewriteStderrFile(path.Join(config.Directory, config.Filename))
	// if err != nil {
	// 	log.Error().Err(err).Msg("Fail to rewite stderr.")
	// }

	logger.Info().
		Bool("fileLogging", config.FileLoggingEnabled).
		Bool("jsonLogOutput", config.EncodeLogsAsJSON).
		Str("logDirectory", config.Directory).
		Str("fileName", config.Filename).
		Int("maxSizeMB", config.MaxSize).
		Int("maxBackups", config.MaxBackups).
		Int("maxAgeInDays", config.MaxAge).
		Msg("logging configured")

	return &Logger{
		Logger: &logger,
	}
}

func newConsoleWriter(config *Config, writer io.Writer) *zerolog.ConsoleWriter {
	// log output should be stderr, and stdout should be program output
	consoleWriter := zerolog.ConsoleWriter{
		Out:        writer,
		NoColor:    true,
		TimeFormat: time.RFC3339Nano,
	}
	consoleWriter.FormatLevel = func(i any) string {
		return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
	}
	consoleWriter.FormatMessage = func(i any) string {
		return fmt.Sprintf("%s |", i)
	}
	consoleWriter.FormatFieldName = func(i any) string {
		return fmt.Sprintf("%s=", i)
	}
	consoleWriter.FormatFieldValue = func(i any) string {
		return fmt.Sprintf("%s", i)
	}
	return &consoleWriter
}

func newRollingFile(config *Config) io.Writer {
	if err := os.MkdirAll(config.Directory, 0744); err != nil {
		log.Error().Err(err).Str("path", config.Directory).Msg("can't create log directory")
		return nil
	}

	return &lumberjack.Logger{
		Filename:   path.Join(config.Directory, config.Filename),
		MaxBackups: config.MaxBackups, // files
		MaxSize:    config.MaxSize,    // megabytes
		MaxAge:     config.MaxAge,     // days
		LocalTime:  true,
	}
}

func zerologGlobalConfiguration() {
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

	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.MessageFieldName = "m"
	zerolog.CallerFieldName = "c"
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
}

func rewriteStderrFile(filename string) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println(err)
		return err
	}
	stdErrFileHandler = file // 把文件句柄保存到全局变量，避免被GC回收

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
