package game

import (
	"flag"

	"math/rand"
	"time"

	"os"

	_ "net/http/pprof"

	"runtime"

	"os/signal"

	"syscall"

	"github.com/xiaonanln/goworld/components/binutil"
	"github.com/xiaonanln/goworld/components/dispatcher/dispatcher_client"
	"github.com/xiaonanln/goworld/config"
	"github.com/xiaonanln/goworld/crontab"
	"github.com/xiaonanln/goworld/entity"
	"github.com/xiaonanln/goworld/gwlog"
	"github.com/xiaonanln/goworld/kvdb"
	"github.com/xiaonanln/goworld/netutil"
	"github.com/xiaonanln/goworld/proto"
	"github.com/xiaonanln/goworld/storage"
)

var (
	serverid    uint16
	configFile  string
	logLevel    string
	gameService *GameService
	signalChan  = make(chan os.Signal, 1)
)

func init() {
	parseArgs()
}

func parseArgs() {
	var serveridArg int
	flag.IntVar(&serveridArg, "sid", 0, "set serverid")
	flag.StringVar(&configFile, "configfile", "", "set config file path")
	flag.StringVar(&logLevel, "log", "", "set log level, will override log level in config")
	flag.Parse()
	serverid = uint16(serveridArg)
}

func Run(delegate IServerDelegate) {
	rand.Seed(time.Now().UnixNano())

	if configFile != "" {
		config.SetConfigFile(configFile)
	}

	gameConfig := config.GetServer(serverid)
	if gameConfig.GoMaxProcs > 0 {
		gwlog.Info("SET GOMAXPROCS = %d", gameConfig.GoMaxProcs)
		runtime.GOMAXPROCS(gameConfig.GoMaxProcs)
	}
	if logLevel == "" {
		logLevel = gameConfig.LogLevel
	}
	binutil.SetupGWLog(logLevel, gameConfig.LogFile, gameConfig.LogStderr)

	storage.Initialize()
	kvdb.Initialize()
	crontab.Initialize()

	binutil.SetupPprofServer(gameConfig.PProfIp, gameConfig.PProfPort)

	entity.SetSaveInterval(gameConfig.SaveInterval)

	gameService = newGameService(serverid, delegate)

	dispatcher_client.Initialize(&dispatcherClientDelegate{})

	entity.CreateSpaceLocally(0) // create to be the nil space

	setupSignals()

	gameService.run()
}

func setupSignals() {
	gwlog.Info("Setup signals ...")
	signal.Notify(signalChan, syscall.SIGINT)
	signal.Notify(signalChan, syscall.SIGTERM)

	go func() {
		for {
			sig := <-signalChan
			if sig == syscall.SIGINT || sig == syscall.SIGTERM {
				// terminating server ...
				gwlog.Info("Terminating game service ...")
				gameService.terminate()
				gameService.terminated.Wait()
				gwlog.Info("Server shutdown gracefully.")
				os.Exit(0)
			} else {
				gwlog.Error("unexpected signal: %s", sig)
			}
		}
	}()
}

type dispatcherClientDelegate struct {
}

func (delegate *dispatcherClientDelegate) OnDispatcherClientConnect(dispatcherClient *dispatcher_client.DispatcherClient, isReconnect bool) {
	// called when connected / reconnected to dispatcher (not in main routine)
	dispatcherClient.SendSetServerID(serverid, isReconnect)
}

var lastWarnGateServiceQueueLen = 0

func (delegate *dispatcherClientDelegate) HandleDispatcherClientPacket(msgtype proto.MsgType_t, packet *netutil.Packet) {
	gameService.packetQueue <- packetQueueItem{ // may block the dispatcher client routine
		msgtype: msgtype,
		packet:  packet,
	}
}

func GetServerID() uint16 {
	return serverid
}
