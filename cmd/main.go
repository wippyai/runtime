package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	pctx "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/component"
	"github.com/ponyruntime/pony/config/json"
	"github.com/ponyruntime/pony/server/http"
	"go.uber.org/zap/zapcore"

	"github.com/ponyruntime/pony/exec"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

func main() {
	app := &cli.App{
		Name:  "Pony",
		Usage: "pony run -c <config.json>",
		Commands: []*cli.Command{
			{
				Name:    "run",
				Aliases: []string{"r"},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "config",
						Usage:   "Path to the config file",
						Aliases: []string{"c"},
						Action: func(ctx *cli.Context, cfgFile string) error {
							// init logger and put it into the context,
							// here we should read the config file and init logger with it
							absPath, err := filepath.Abs(cfgFile)
							if err != nil {
								return err
							}

							// save the logger
							zlog := initDevelopmentLogger()
							ctx.Context = context.WithValue(ctx.Context, pctx.LoggerKey, zlog)
							// safe the config file
							ctx.Context = context.WithValue(ctx.Context, pctx.CfgFilenameKey, absPath)
							return nil
						},
					},
				},
				Usage:  "start the Pony server",
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
	// TODO: config for logger
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

	// primary execution queue sub-system
	queue := exec.NewQueue()

	// server and all the ingress plugins and endpoints
	endpoints := component.NewHub(
		zlog.Named("server"),
		queue,
		component.Declaration{
			ID:        http.Component,
			Component: http.NewComponent(zlog.Named("http")),
		},
	)

	// wait for all endpoints to init
	endpoints.Boot(context.Background())
	defer endpoints.Close(context.Background())

	// wait for all runtime to init

	// Loading application configuration

	// todo: fix this
	cfgFilePath := ctx.Context.Value(pctx.CfgFilenameKey).(string)
	zlog.Named("root").Info("Pony server is starting ", zap.String("config", cfgFilePath))
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

	// at this step, we're reading the config file and send events to subsystems via eventbus
	// e.g.: when we have an endpoint config - we send it to an endpoint subsystem

	//cfg := jsonCfgProvider.NewProvider(zlog.Named("json"))
	//err := cfg.Parse(cfgFilePath)
	//if err != nil {
	//	// send the error across the system
	//	// TODO: wait for the error to be processed
	//	bus.Send(context.Background(), eb.NewEvent(api.EventFatalError, api.SubSystemAll, payload.NewString(err.Error())))
	//	return err
	//}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		zlog.Info("received a signal to stop the server")
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
