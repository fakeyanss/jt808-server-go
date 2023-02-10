package main

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/internal/protocol"
	"github.com/fakeYanss/jt808-server-go/internal/server"
	"github.com/fakeYanss/jt808-server-go/internal/storage"
	"github.com/fakeYanss/jt808-server-go/pkg/logger"
	"github.com/fakeYanss/jt808-server-go/pkg/routines"
)

const (
	TCPPort  = "8080"
	UDPPort  = "8081"
	HTTPPort = "8008"
)

const (
	LogDir  = "./logs/" // todo: read from configuration
	LogFile = "jt808-server-go.log"
)

func main() {
	routines.Recover()

	logConfig := &logger.Config{
		ConsoleLoggingEnabled: true,
		EncodeLogsAsJSON:      false,
		FileLoggingEnabled:    true,
		LogLevel:              0,
		Directory:             LogDir,
		Filename:              LogFile,
		MaxSize:               5,
		MaxBackups:            128,
		MaxAge:                3,
	}
	log.Logger = *logger.Configure(logConfig).Logger

	serv := server.NewTCPServer()
	addr := ":" + TCPPort
	err := serv.Listen(addr)
	if err != nil {
		log.Error().
			Err(err).
			Str("addr", addr).
			Msg("Fail to listen tcp addr")
		os.Exit(1)
	}
	routines.GoSafe(func() { serv.Start() })

	protocol.CheckDeviceKeepaliveTimer()

	// todo: web server structure
	router := gin.Default()
	cache := storage.GetDeviceCache()
	gisCache := storage.GetGisCache()

	router.GET("/device", func(c *gin.Context) {
		c.JSON(http.StatusOK, cache.ListDevice())
	})

	router.GET("/device/:phone", func(c *gin.Context) {
		phone := c.Param("phone")

		device, err := cache.GetDeviceByPhone(phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}

		res := make(map[string]any)
		err = mapstructure.Decode(device, &res)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		gis, err := gisCache.GetGisLatestByPhone(device.ID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		res["gis"] = gis

		c.JSON(http.StatusOK, res)
	})

	httpAddr := ":" + HTTPPort
	err = router.Run(httpAddr)
	if err != nil {
		log.Error().
			Err(err).
			Str("addr", httpAddr).
			Msg("Fail to run gin router")
		os.Exit(1)
	}
}
