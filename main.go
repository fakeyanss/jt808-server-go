package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/config"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/server"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/pkg/logger"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

func main() {
	routines.Recover()

	var cfgPath string
	flag.StringVar(&cfgPath, "c", config.DefaultServConfKey, "config file path")
	flag.Parse()
	fmt.Printf("Start with configuration %v\n", cfgPath)
	cfg := config.Load(cfgPath)

	logConfig := cfg.ParseLogConf()
	log.Logger = *logger.Configure(logConfig).Logger

	if cfg.Server.Banner.Enable {
		bannerBytes, err := os.ReadFile(cfg.Server.Banner.BannerPath)
		var banner string
		if err != nil {
			banner = config.BannerText
		} else {
			banner = string(bannerBytes)
		}
		fmt.Println(banner)
	}

	serv := server.NewTCPServer()
	addr := ":" + cfg.Server.Port.TCPPort
	err := serv.Listen(addr)
	if err != nil {
		log.Error().Err(err).Str("addr", addr).Msg("Fail to listen tcp addr")
		os.Exit(1)
	}
	routines.GoSafe(func() { serv.Start() })

	// web server structure
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	cache := storage.GetDeviceCache()
	geoCache := storage.GetGeoCache()

	router.GET("/device", func(c *gin.Context) {
		c.JSON(http.StatusOK, cache.ListDevice())
	})

	router.GET("/device/:phone/geo", func(c *gin.Context) {
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
		gis, err := geoCache.GetGeoLatestByPhone(phone)
		if err != nil {
			return
		}
		res["gis"] = gis

		c.JSON(http.StatusOK, res)
	})

	router.GET("/device/:phone/params", func(c *gin.Context) {
		phone := c.Param("phone")
		device, err := cache.GetDeviceByPhone(phone)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		session, err := storage.GetSession(device.SessionID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
		}
		header := model.GenMsgHeader(device, 0x8104, session.GetNextSerialNum())
		msg := model.Msg8104{
			Header: header,
		}
		serv.Send(session.ID, &msg)
		// todo: read channel from process 0104 msg
	})

	httpAddr := ":" + cfg.Server.Port.HTTPPort
	routines.GoSafe(func() {
		log.Debug().Msgf("Listening and serving HTTP on :%s", cfg.Server.Port.HTTPPort)
		err = router.Run(httpAddr)
		if err != nil {
			log.Error().Err(err).Str("addr", httpAddr).Msg("Fail to run gin router")
			os.Exit(1)
		}
	})

	select {} // block here
}
