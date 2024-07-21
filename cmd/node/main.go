package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfave/cli/v2"
)

var log = slog.New(slog.NewJSONHandler(os.Stdout, nil))

var (
	netURL = &cli.StringFlag{
		Name:    "net-url",
		EnvVars: []string{"NET_URL"},
	}
	targetAddr = &cli.StringFlag{
		Name:    "target",
		EnvVars: []string{"TARGET_ADDR"},
	}
)

func main() {
	app := &cli.App{
		Name: "RC4337analytics",
		Flags: []cli.Flag{
			netURL, targetAddr,
		},
		Action: App,
	}

	if err := app.Run(os.Args); err != nil {
		log.Error("failed to run app", "error", err)
	}
}

func App(cctx *cli.Context) error {
	client, err := ethclient.DialContext(cctx.Context, netURL.Get(cctx))
	if err != nil {
		return fmt.Errorf("failed to dial eth network: %w", err)
	}

	query := ethereum.FilterQuery{
		Addresses: []common.Address{common.HexToAddress(targetAddr.Get(cctx))},
	}

	targetEvents := make(chan types.Log)
	sub, err := client.SubscribeFilterLogs(cctx.Context, query, targetEvents)
	if err != nil {
		return fmt.Errorf("failed to subscribe to logs: %w", err)
	}

	go subscribe(sub, targetEvents)

	s := http.Server{
		Addr: ":80",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		ReadTimeout:       time.Second * 30,
		ReadHeaderTimeout: time.Second * 5,
	}
	log.InfoContext(cctx.Context, "server started", "addr", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

func subscribe(sub ethereum.Subscription, targetEvents chan types.Log) {
	ctx := context.Background()
	for {
		select {
		case err := <-sub.Err():
			log.ErrorContext(ctx, "error in log subscription", "error", err)
		case targetEvent := <-targetEvents:
			log.InfoContext(ctx, fmt.Sprintf("new event: %v\n", targetEvent))
		case <-time.After(time.Second * 3):
			log.InfoContext(ctx, "no events")
		}
	}
}