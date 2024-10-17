package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ponyruntime/pony/api"
	jsonCfgProvider "github.com/ponyruntime/pony/configuration/providers/json"
	pctx "github.com/ponyruntime/pony/context"
	"github.com/ponyruntime/pony/endpoints"
	eventsbus "github.com/ponyruntime/pony/eventbus"
	"github.com/ponyruntime/pony/futures"
	"github.com/ponyruntime/pony/runtime"
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
						Usage:   "Path to the configuration file",
						Aliases: []string{"c"},
						Action: func(ctx *cli.Context, cfgFile string) error {
							// init logger and put it into the context,
							// here we should read the configuration file and init logger with it
							absPath, err := filepath.Abs(cfgFile)
							if err != nil {
								return err
							}

							// save the logger
							zlog, _ := zap.NewDevelopment()
							ctx.Context = context.WithValue(ctx.Context, pctx.LoggerKey, zlog)
							// safe the config file
							ctx.Context = context.WithValue(ctx.Context, pctx.CfgFilenameKey, absPath)
							return nil
						},
					},
				},
				Usage: "Start the Pony server",
				Action: func(ctx *cli.Context) error {
					// TODO: configuration for logger
					// TODO: setup tracing in the context
					zlogctx := ctx.Context.Value(pctx.LoggerKey)

					// parse logger
					var zlog *zap.Logger
					switch tt := zlogctx.(type) {
					case *zap.Logger:
						zlog = tt
					default:
						zlog, _ = zap.NewDevelopment()
					}

					bus, id := eventsbus.NewEventBus()
					defer bus.Unsubscribe(context.Background(), id)

					// TODO: UNSAFE!!!!!! FIX!!!
					cfgFilePath := ctx.Context.Value(pctx.CfgFilenameKey).(string)

					queue := futures.NewQueue()
					endpoints.NewEndpoints(zlog.Named("endpoints"), queue).ListenEvents()
					runtime.NewRuntime(zlog.Named("runtime"), queue).ListenEvents()

					// at this step, we're reading the configuration file and send events to subsystems via eventbus
					// e.g.: when we have an endpoint configuration - we send it to an endpoint subsystem
					cfg := jsonCfgProvider.NewProvider(zlog.Named("json"))
					err := cfg.Parse(cfgFilePath)
					if err != nil {
						// send the error across the system
						// TODO: wait for the error to be processed
						bus.Send(context.Background(), eventsbus.NewEvent(api.EventFatalError, api.SubSystemAll, err))
						return err
					}

					sigCh := make(chan os.Signal, 1)
					signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

					select {
					case <-sigCh:
						zlog.Info("received a signal to stop the server")
						return nil
					}
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
