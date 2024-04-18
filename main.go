package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mqy/minipush/auth"
	"github.com/mqy/minipush/cluster"
	pb "github.com/mqy/minipush/proto"
	"github.com/mqy/minipush/store"
	"github.com/mqy/minipush/ws"
)

const (
	kafkaGroupId         = "minipush"
	kafkaTopic           = "minipush-events"
	eventPayloadMaxBytes = 4096
)

var (
	flagAddr           = flag.String("addr", "127.0.0.1:8000", "server address, ip:port")
	flagPidFile        = flag.String("pid-file", "minipush.pid", "pid file")
	flagMysqlDsn       = flag.String("mysql-dsn", "root:@tcp(127.0.0.1:3306)/minipush?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci", "mysql server dsn")
	flagKafkaBrokers   = flag.String("kafka-brokers", "127.0.0.1:9092", "comma separated kafka brokers")
	flagSessionQuota   = flag.Uint("session-quota", 5, "cluster level per user session quota, allowed value in [1, 10]")
	flagEventTTLDays   = flag.Uint("event-ttldays", 30, "event TTL in days")
	flagGetEventsLimit = flag.Uint("get-events-limit", 100, "get events: seq range limit")
	flagCleanEvents    = flag.Bool("clean-events", true, "enable leader delete outdated events")

	flagEnableClientMsg = flag.Bool("enable-client-msg", false, "enable end to end message between users, experimental")

	flagPprofDir       = flag.String("pprof-dir", "pprof", "dir to save pprof data files")
	flagDisableMetrics = flag.Bool("disable-metrics", false, "disable prometheus metrics")

	flagStandalone = flag.Bool("standalone", true, "run as standalone server")
	flagLeader     = flag.Bool("leader", false, "cluster: run as leader that act as push/route server")
	flagFollower   = flag.Bool("follower", false, "cluster: run as follower that serve user requests")
	flagJoin       = flag.String("join", "", "cluster: the leader address to join as follower, ip:port")

	flagEnableDemo    = flag.Bool("enable-demo", false, "enable demo")
	flagDemoStaticDir = flag.String("demo-static-dir", "../dev/demo/static", "demo static dir")
)

func main() {
	flag.Parse()

	// NOTE: os.Exit() does not call defers.
	os.Exit(run())
}

func run() int {
	defer glog.Flush()

	if v := validateFlags(); v > 0 {
		return v
	}

	pid := os.Getpid()

	if err := savePid(*flagPidFile, pid); err != nil {
		return errorf("pid file: %v", err)
	}
	defer func() {
		_ = os.Remove(*flagPidFile)
	}()

	pprofDir := filepath.Join(*flagPprofDir, strconv.Itoa(pid))
	if err := os.MkdirAll(pprofDir, 0750); err != nil {
		return errorf("--profiler-data-dir: error create dir `%s`: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(pprofDir)
	}()

	db, err := sql.Open("mysql", *flagMysqlDsn)
	if err != nil {
		return errorf("sql.Open error, dsn: %s, err: %v", *flagMysqlDsn, err)
	}

	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(1)

	glog.Info("minipush server is starting")

	eventStore := store.NewEventStore(db)

	// new hub.
	kafkaBrokers := strings.Split(*flagKafkaBrokers, ",")
	hub := ws.NewHub(newAuthClient(), eventStore, &pb.WsConf{
		EventTtlDays:    int32(*flagEventTTLDays),
		GetEventsLimit:  int32(*flagGetEventsLimit),
		EnableClientMsg: *flagEnableClientMsg,
		MaxMsgSize:      eventPayloadMaxBytes,
	})

	mux := http.DefaultServeMux
	if !*flagDisableMetrics {
		mux.Handle("/metrics", promhttp.HandlerFor(
			prometheus.DefaultGatherer,
			promhttp.HandlerOpts{},
		))
	}
	mux.Handle("/ws", hub)
	if *flagEnableDemo {
		const demoRootPath = "/demo/"
		fs := http.FileServer(http.Dir(*flagDemoStaticDir))
		mux.Handle(demoRootPath, http.StripPrefix(demoRootPath, fs))
		mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join(*flagDemoStaticDir, "favicon.ico"))
		})
	}

	cc := &cluster.ClusterCfg{
		Pid:          pid,
		Addr:         *flagAddr,
		Hub:          hub,
		Mux:          mux,
		EventManager: eventStore,

		KafkaBrokers: kafkaBrokers,
		KafkaTopic:   kafkaTopic,
		KafkaGroupId: kafkaGroupId,

		EnableClientMsg: *flagEnableClientMsg,

		EventPayloadMaxBytes: eventPayloadMaxBytes,
		SessionQuota:         int32(*flagSessionQuota),

		CleanEvents:  *flagCleanEvents,
		EventTTLDays: int32(*flagEventTTLDays),

		RunLeader:   *flagLeader,
		RunFollower: *flagFollower,
		LeaderAddr:  *flagJoin,
	}

	// new clusterImpl.
	var clusterImpl cluster.ICluster
	if *flagStandalone {
		clusterImpl = cluster.NewStandalone(cc)
	} else {
		clusterImpl = cluster.NewCluster(cc)
	}

	stopNotifyChan := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go clusterImpl.Run(ctx, stopNotifyChan)

	glog.Infof("minipush server is starting")
	glog.Infof("`kill -USR1 %d` to dup goroutines; `kill -USR2 %d` to start/stop profiler; `CTRL+c` or `kill %d` to graceful stop", pid, pid, pid)

	var stopping bool

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTERM, syscall.SIGINT)

	var prof *Profiler

	for sig := range sigCh {
		switch sig {
		case syscall.SIGUSR1:
			if prof != nil {
				prof.dumpGoroutines()
			}
		case syscall.SIGUSR2:
			if prof == nil {
				prof = StartProfiler(pprofDir)
			} else {
				prof.Stop()
				prof = nil
			}
		case syscall.SIGTERM, syscall.SIGINT:
			if stopping {
				glog.Infof("minipush server is already in stop")
				continue
			}
			stopping = true
			glog.Infof("received signal `%s` stopping", sig.String())
			go func() {
				if prof != nil {
					prof.Stop()
				}
				cancel()
				<-stopNotifyChan
				close(stopNotifyChan)
				_ = db.Close()
				signal.Stop(sigCh)
				close(sigCh)
			}()
		}
	}

	glog.Info("minipush server exited")
	return 0
}

func newAuthClient() auth.Client {
	// TODO: hook into production auth API.
	return &auth.MockClient{}
}

func validateFlags() int {
	if *flagAddr == "" {
		return errorf("--addr is required")
	}
	if err := validateAddr(*flagAddr); err != nil {
		return errorf("--addr: %v", *flagAddr, err)
	}
	if *flagPidFile == "" {
		return errorf("--pid-file is required")
	}
	if *flagPprofDir == "" {
		return errorf("--pprof-dir is required")
	}

	if *flagCleanEvents {
		if *flagEventTTLDays < ws.MinTTLDays || *flagEventTTLDays > ws.MaxTTLDays {
			return errorf("invalid --events-ttl-days, expect in range [%d, %d]", ws.MinTTLDays, ws.MaxTTLDays)
		}
	}

	if *flagGetEventsLimit < ws.MinGetEventsLimit || *flagGetEventsLimit > ws.MaxGetEventsLimit {
		return errorf("invalid --events-get-limit, expect in range [%d, %d]", ws.MinGetEventsLimit, ws.MaxGetEventsLimit)
	}

	if len(*flagKafkaBrokers) == 0 {
		return errorf("--kafka-endpoints is required.")
	}

	if *flagMysqlDsn == "" {
		return errorf("--mysql-dsn is required.")
	}

	if *flagSessionQuota == 0 {
		return errorf("--session-quota is required positive integer")
	} else if *flagSessionQuota > 10 {
		return errorf("--session-quota MUST in range [1, 10]")
	}

	if !*flagStandalone {
		if !*flagLeader {
			if *flagFollower {
				if *flagJoin == "" {
					return errorf("--join is required")
				} else {
					if err := validateAddr(*flagJoin); err != nil {
						return errorf("--join: %v", *flagAddr, err)
					}
				}
			} else {
				return errorf("--leader and/or --follower are required")
			}
		}

		if *flagStandalone || *flagFollower {
			if *flagEnableDemo {
				if *flagDemoStaticDir == "" {
					return errorf("--demo-static-dir is required.")
				}

				if _, err := os.Stat(*flagDemoStaticDir); err != nil {
					return errorf("error stat demo static dir `%s`: %v", *flagDemoStaticDir, err)
				}
			}
		}
	}

	return 0
}

func validateAddr(s string) error {
	ips, _, err := net.SplitHostPort(*flagAddr)
	if err != nil {
		return fmt.Errorf("error split host port from `%s`: %v", *flagAddr, err)
	}
	ip := net.ParseIP(ips)
	if ip == nil {
		return fmt.Errorf("error parse IP from host `%s`", ips)
	}
	if !ip.IsLoopback() && !ip.IsPrivate() {
		return fmt.Errorf("`%s` is not loopback or private address", ips)
	}
	return nil
}

func errorf(fmt string, args ...interface{}) int {
	glog.Errorf(fmt, args...)
	return 1
}

func savePid(name string, pid int) error {
	if _, err := os.Stat(name); err == nil {
		// Ok, see, if we have a stale lockfile here
		content, err := ioutil.ReadFile(name)
		if err != nil {
			return err
		}
		if len(content) > 0 {
			oldPid, err := strconv.Atoi(string(content))
			if err != nil {
				return err
			}

			proc, err := os.FindProcess(oldPid)
			if err != nil {
				return err
			}
			defer proc.Release()

			if err := proc.Signal(syscall.Signal(0)); err == nil {
				return fmt.Errorf("pid file: exists with pid: %d, the process is running", oldPid)
			} else {
				glog.Infof("pid file exists with pid: %d, but is not running", oldPid)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("pid file: stat error: %v", err)
	}

	if err := ioutil.WriteFile(name, []byte(strconv.Itoa(pid)), 0600); err != nil {
		return fmt.Errorf("pid file: write error: %v", err)
	}
	glog.Infof("pid file: write pid done")
	return nil
}
