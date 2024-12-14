package main

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/__OOOOLD/component/server/http"
	"github.com/ponyruntime/pony/components/config/json"
	"github.com/ponyruntime/pony/components/exec"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	pctx "github.com/ponyruntime/pony/api/context"
	"go.uber.org/zap/zapcore"

	"go.uber.org/zap"
)

func main() {
	app := &cli.App{
		Name:  "Pony",
		Usage: "pony run -c <chart.json>",
		Commands: []*cli.Command{
			{
				Name:    "run",
				Aliases: []string{"r"},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "chart",
						Usage:   "Name to the chart file",
						Aliases: []string{"c"},
						Action: func(ctx *cli.Context, cfgFile string) error {
							// init logger and put it into the context,
							// here we should read the chart file and init logger with it
							absPath, err := filepath.Abs(cfgFile)
							if err != nil {
								return err
							}

							// save the logger
							zlog := initDevelopmentLogger()
							ctx.Context = context.WithValue(ctx.Context, pctx.LoggerKey, zlog)
							// safe the chart file
							ctx.Context = context.WithValue(ctx.Context, pctx.CfgFilenameKey, absPath)
							return nil
						},
					},
				},
				Usage:  "start the Pony web_server",
				Action: run,
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func run(ctx *cli.Context) error {
	// TODO: chart for logger
	// TODO: setup tracing in the context
	zlogctx := ctx.Context.Value(pctx.LoggerKey)

	// parse logger
	var zlog *zap.Logger
	switch tt := zlogctx.(type) {
	case *zap.Logger:
		zlog = tt
	default:
		zlog = initDevelopmentLogger()
	}

	// primary execution queue sub-core
	queue := exec.NewQueue()

	// web_server and all the ingress plugins and endpoints
	endpoints := component.NewHub(
		zlog.Named("web_server"),
		queue,
		component.Declaration{
			ID:        http.Component,
			Component: http.NewComponent(zlog.Named("web_server")),
		},
	)

	// wait for all endpoints to init
	endpoints.Boot(context.Background())
	defer endpoints.Close(context.Background())

	// wait for all runtime to init

	// Loading application configuration

	// todo: fix this
	cfgFilePath := ctx.Context.Value(pctx.CfgFilenameKey).(string)
	zlog.Named("root").Info("Pony web_server is starting ", zap.String("chart", cfgFilePath))
	_, err := json.LoadChangelogFile(cfgFilePath)
	if err != nil {
		return err
	}

	// writing setup

	// single pass configuration via change group

	//// runtime also composite
	//rnt := runtime.NewHub(
	//	zlog.Named("runtime"),
	//	queue,
	//	// todo: lua subsystem
	//)
	//rnt.ListenEvents()

	// at this step, we're reading the chart file and send events to subsystems via events
	// e.g.: when we have an endpoint chart - we send it to an endpoint subsystem

	//chart := jsonCfgProvider.NewProvider(zlog.Named("json"))
	//err := chart.Parse(cfgFilePath)
	//if err != nil {
	//	// send the error across the core
	//	// TODO: wait for the error to be processed
	//	bus.Send(context.Background(), eb.NewEvent(api.EventFatalError, api.SubSystemAll, payload.NewString(err.Error())))
	//	return err
	//}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		zlog.Info("received a signal to stop the web_server")
		return nil
	}
}

func initDevelopmentLogger() *zap.Logger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.EncoderConfig.EncodeCaller = nil
	config.EncoderConfig.EncodeName = func(loggerName string, enc zapcore.PrimitiveArrayEncoder) {
		// Simple hash function - sum ASCII values
		hash := 0
		for _, char := range loggerName {
			hash += int(char)
		}

		// cmap hash to one of 6 colors (31-36: red, green, yellow, blue, magenta, cyan)
		colorCode := 31 + (hash % 6)

		// Wrap name in ANSI color codes
		coloredName := fmt.Sprintf("\x1b[%dm%s\x1b[0m", colorCode, loggerName)
		enc.AppendString(coloredName)
	}

	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)
	zlog, _ := config.Build()
	return zlog
}
