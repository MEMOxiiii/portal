package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/paroxity/portal"
	"github.com/paroxity/portal/internal"
	portallog "github.com/paroxity/portal/log"
	"github.com/paroxity/portal/metrics"
	"github.com/paroxity/portal/session"
	"github.com/paroxity/portal/socket"
	socketpacket "github.com/paroxity/portal/socket/packet"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/text"
	"github.com/sirupsen/logrus"
)

func main() {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		ForceColors:     true,
		FullTimestamp:   true,
		TimestampFormat: "15:04:05",
	})
	conf := readConfig(logger)
	if conf.Logger.File != "" {
		fileLogger, err := portallog.New(conf.Logger.File)
		if err != nil {
			logger.Fatalf("unable to create file logger: %v", err)
		}
		logger.SetOutput(fileLogger)
	}
	level, err := logrus.ParseLevel(conf.Logger.Level)
	if err != nil {
		logger.Errorf("unable to parse log level '%s': %v", conf.Logger.Level, err)
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	resourcePackManager, err := portal.NewResourcePackManager(conf.ResourcePacks.Directory, conf.ResourcePacks.EncryptionKeys)
	if err != nil {
		logger.Fatalf("unable to load resource packs: %v", err)
	}
	if conf.ResourcePacks.HotReload.Enabled {
		interval := time.Duration(conf.ResourcePacks.HotReload.Interval) * time.Second
		if interval <= 0 {
			interval = 30 * time.Second
		}
		go resourcePackManager.StartHotReload(context.Background(), interval, logger)
		logger.Infof("resource pack hot reload enabled with %s interval", interval)
	}

	p := portal.New(portal.Options{
		Logger: logger,

		Address: conf.Network.Address,
		ListenConfig: minecraft.ListenConfig{
			StatusProvider: portal.NewMOTDStatusProvider(conf.MOTD).SubMOTD(conf.SubMOTD),

			ResourcePacks:        resourcePackManager.ResourcePacks(),
			FetchResourcePacks:   resourcePackManager.FetchResourcePacks,
			TexturePacksRequired: conf.ResourcePacks.Required,
		},

		Whitelist: session.NewSimpleWhitelist(conf.Whitelist.Enabled, conf.Whitelist.Players),
		IPGuard: session.NewSimpleIPGuard(
			conf.Security.BannedIPs,
			conf.Security.RateLimit.Enabled,
			time.Duration(conf.Security.RateLimit.WindowSeconds)*time.Second,
			conf.Security.RateLimit.MaxAttempts,
		),
	})
	if conf.Routing.DefaultGroup != "" {
		p.SetLoadBalancer(session.NewGroupedLoadBalancer(p.ServerRegistry(), conf.Routing.DefaultGroup, conf.Routing.FallbackGroups...))
	}
	if err := p.Listen(); err != nil {
		logger.Fatalf("failed to listen on %s: %v", conf.Network.Address, err)
	}

	var socketServer *socket.DefaultServer
	if conf.Network.Communication.TLS.Enabled {
		cert, err := tls.LoadX509KeyPair(conf.Network.Communication.TLS.CertFile, conf.Network.Communication.TLS.KeyFile)
		if err != nil {
			logger.Fatalf("unable to load communication TLS certificate: %v", err)
		}
		socketServer = socket.NewDefaultTLSServer(conf.Network.Communication.Address, conf.Network.Communication.Secret, p.SessionStore(), p.ServerRegistry(), logger, conf.Network.ReaderLimits, &tls.Config{Certificates: []tls.Certificate{cert}}, p.Events())
	} else {
		socketServer = socket.NewDefaultServer(conf.Network.Communication.Address, conf.Network.Communication.Secret, p.SessionStore(), p.ServerRegistry(), logger, conf.Network.ReaderLimits, p.Events())
	}
	if err := socketServer.Listen(); err != nil {
		p.Logger().Fatalf("socket server failed to listen: %v", err)
	}

	// Set up pre-transfer hook to notify target servers to disconnect stale sessions.
	p.SessionStore().PreTransfer = func(serverName, playerName string) {
		client, ok := socketServer.Client(serverName)
		if !ok {
			return
		}
		_ = client.WritePacket(&socketpacket.DisconnectPlayer{PlayerName: playerName})
	}

	if conf.PlayerLatency.Report {
		go socketServer.ReportPlayerLatency(time.Second * time.Duration(conf.PlayerLatency.UpdateInterval))
	}

	if conf.Metrics.Enabled {
		metrics.Default.RegisterGauge("portal_players_online", func() float64 { return float64(len(p.SessionStore().All())) })
		metrics.Default.RegisterGauge("portal_servers_registered", func() float64 { return float64(len(p.ServerRegistry().Servers())) })
		metrics.Default.RegisterGauge("portal_socket_clients_connected", func() float64 { return float64(len(socketServer.Clients())) })

		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Default.Handler())
		go func() {
			logger.Infof("metrics endpoint listening on %s", conf.Metrics.Address)
			if err := http.ListenAndServe(conf.Metrics.Address, mux); err != nil {
				logger.Errorf("metrics server failed: %v", err)
			}
		}()
	}

	go waitForShutdown(p, socketServer, logger)

	for {
		s, err := p.Accept()
		if err != nil {
			if s != nil {
				s.Disconnect(text.Colourf("<red>%v</red>", err))
			}
			p.Logger().Errorf("failed to accept connection: %v", err)
			continue
		}
		_ = s
	}
}

// waitForShutdown blocks until an interrupt or termination signal is received, then gracefully disconnects
// every connected session and closes the proxy's listeners before exiting the process.
func waitForShutdown(p *portal.Portal, socketServer *socket.DefaultServer, logger internal.Logger) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	logger.Infof("shutting down...")
	for _, s := range p.SessionStore().All() {
		s.Disconnect(text.Colourf("<yellow>Proxy is shutting down.</yellow>"))
	}
	if err := socketServer.Close(); err != nil {
		logger.Errorf("failed to close socket server: %v", err)
	}
	if err := p.Close(); err != nil {
		logger.Errorf("failed to close proxy listener: %v", err)
	}
	logger.Infof("shutdown complete")
	os.Exit(0)
}

func readConfig(logger internal.Logger) portal.Config {
	c := portal.DefaultConfig()
	if _, err := os.Stat("config.json"); os.IsNotExist(err) {
		f, err := os.Create("config.json")
		if err != nil {
			logger.Fatalf("error creating config: %v", err)
		}
		data, err := json.MarshalIndent(c, "", "\t")
		if err != nil {
			logger.Fatalf("error encoding default config: %v", err)
		}
		if _, err := f.Write(data); err != nil {
			logger.Fatalf("error writing encoded default config: %v", err)
		}
		_ = f.Close()
	}
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		logger.Fatalf("error reading config: %v", err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		logger.Fatalf("error decoding config: %v", err)
	}
	return c
}
