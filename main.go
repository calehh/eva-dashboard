package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/Evanesco-Labs/go-evanesco/cmd/utils"
	"github.com/Evanesco-Labs/go-evanesco/log"
	"github.com/Evanesco-Labs/go-evanesco/rpc"
	"github.com/gorilla/mux"
	"github.com/syndtr/goleveldb/leveldb"
	"gopkg.in/urfave/cli.v1"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	OriginCommandHelpTemplate = `{{.Name}}{{if .Subcommands}} command{{end}}{{if .Flags}} [command options]{{end}} {{.ArgsUsage}}
{{if .Description}}{{.Description}}
{{end}}{{if .Subcommands}}
SUBCOMMANDS:
  {{range .Subcommands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}{{end}}{{if .Flags}}
OPTIONS:
{{range $.Flags}}   {{.}}
{{end}}
{{end}}`
)

const QueryWait = time.Minute * 10
const DayTimes = 24 * 6

var LastSubmitCount = uint64(0)

var RpcUrl = []string{"http://seed1.evanesco.org:8546", "http://seed2.evanesco.org:8546", "http://seed3.evanesco.org:8546", "ws://seed4.evanesco.org:7778", "http://seed5.evanesco.org:8546"}

var app *cli.App

var (
	dataPathFlag = cli.StringFlag{
		Name:  "data",
		Usage: "data path",
		Value: "",
	}
	portFlag = cli.StringFlag{
		Name:  "port",
		Usage: "restful rpc port",
		Value: "3333",
	}
)

func init() {
	app = cli.NewApp()
	app.Version = "v1.0.0"
	app.Action = Start
	app.Flags = []cli.Flag{
		dataPathFlag,
		portFlag,
	}

	cli.CommandHelpTemplate = OriginCommandHelpTemplate
}

func main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func Start(ctx *cli.Context) {
	log.Info("starting dashboard")
	dbPath := ""
	if ctx.IsSet(dataPathFlag.Name) {
		dbPath = ctx.String(dataPathFlag.Name) + "/dashboard.db"
	} else {
		dbPath = "./dashboard.db"
	}

	log.Info("open db")
	db, err := leveldb.OpenFile(dbPath, nil)
	defer db.Close()
	if err != nil {
		utils.Fatalf("open db err %v", err)
	}

	log.Info("load submit count")
	go LoadSubmitCount(db)
	log.Info("start restful server")
	r := mux.NewRouter()

	r.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("eva dashboard"))
	})

	r.HandleFunc("/lastcnt", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf("%v", LastSubmitCount)))
	})

	r.HandleFunc("/daycnt/{day}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		day, ok := vars["day"]
		if !ok {
			w.Write([]byte(fmt.Sprintf("%v", -1)))
			return
		}

		num, e := db.Get([]byte(day), nil)
		if e != nil {
			w.Write([]byte(fmt.Sprintf("%v", -2)))
			return
		}
		w.Write([]byte(fmt.Sprintf("%v", binary.BigEndian.Uint64(num))))

	})
	address := ""
	if ctx.IsSet(portFlag.Name) {
		address = "0.0.0.0:" + ctx.String(portFlag.Name)
	} else {
		address = "0.0.0.0:8547"
	}
	log.Info("address", "url", address)
	err = http.ListenAndServe(address, r)
	if err != nil {
		utils.Fatalf("http listen err", "err", err)
	}
	waitToExit()
}

func LoadSubmitCount(db *leveldb.DB) {
	for {
		dayTotal := uint64(0)
		cnt := uint64(0)
		for i := 0; i < DayTimes; i++ {
			total := uint64(0)
			requestErr := false
			for _, url := range RpcUrl {
				c, err := GetLastSubmitCount(url)
				if err != nil {
					log.Error("Get last submit count err", "url", url, "err", err)
					requestErr = true
					continue
				}
				total = total + c
			}
			if requestErr {
				continue
			}
			LastSubmitCount = total
			dayTotal = dayTotal + total
			cnt++
			time.Sleep(QueryWait)
		}
		dayAvr := dayTotal / cnt
		avrBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(avrBytes, dayAvr)

		dayString := fmt.Sprintf("%v-%v-%v", time.Now().Year(), int(time.Now().Month()), time.Now().Day())
		log.Info("submit count day average", "int", dayAvr)
		err := db.Put([]byte(dayString), avrBytes, nil)
		if err != nil {
			log.Error("put day average err", "err", err)
		}
	}
}

func GetLastSubmitCount(url string) (uint64, error) {
	client, err := rpc.Dial(url)
	if err != nil {
		return 0, err
	}
	num := uint64(0)
	err = client.CallContext(context.Background(), &num, "eth_lastSubmitCount")
	if err != nil {
		return 0, err
	}
	return num, nil
}

func waitToExit() {
	exit := make(chan bool, 0)
	sc := make(chan os.Signal, 1)
	if !signal.Ignored(syscall.SIGHUP) {
		signal.Notify(sc, syscall.SIGHUP)
	}
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sc {
			fmt.Printf("received exit signal:%v", sig.String())
			close(exit)
			break
		}
	}()
	<-exit
}
