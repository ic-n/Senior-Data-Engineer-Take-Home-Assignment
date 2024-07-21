package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ic-n/ERC4337analytics/pkg/contracts"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	bundlers = &cli.StringFlag{
		Name:    "bundlers",
		EnvVars: []string{"BUNDLERS"},
	}
)

var (
	opsHourly = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "user_ops_hourly",
		Help: "The number of processed events from bundlers per hour",
	}, []string{"success", "bundler", "hour"})
	opsDaily = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "user_ops_daily",
		Help: "The number of processed events from bundlers per day",
	}, []string{"success", "bundler", "day"})
	opsWeekly = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "user_ops_weekly",
		Help: "The number of processed events from bundlers per week",
	}, []string{"success", "bundler", "week"})
)

func main() {
	app := &cli.App{
		Name: "RC4337analytics",
		Flags: []cli.Flag{
			netURL, targetAddr, bundlers,
		},
		Action: App,
	}

	if err := app.Run(os.Args); err != nil {
		log.Error("failed to run app", "error", err)
	}
}

func App(cctx *cli.Context) error {
	subscribe, err := subscriber(cctx)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	go subscribe()

	m := http.NewServeMux()
	m.Handle("/metrics", promhttp.Handler())

	s := http.Server{
		Addr:              ":2112",
		Handler:           m,
		ReadTimeout:       time.Second * 30,
		ReadHeaderTimeout: time.Second * 5,
	}
	log.InfoContext(cctx.Context, "server started", "addr", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

func subscriber(cctx *cli.Context) (func(), error) {
	client, err := ethclient.DialContext(cctx.Context, netURL.Get(cctx))
	if err != nil {
		return nil, fmt.Errorf("failed to dial eth network: %w", err)
	}

	targetAddr := common.HexToAddress(targetAddr.Get(cctx))

	query := ethereum.FilterQuery{
		Addresses: []common.Address{targetAddr},
	}

	contract, err := contracts.NewEntryPoint(targetAddr, client)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate EntryPoint contract: %w", err)
	}

	targetEvents := make(chan types.Log)
	sub, err := client.SubscribeFilterLogs(cctx.Context, query, targetEvents)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to logs: %w", err)
	}

	ctx := context.Background()
	analyse := analyser(cctx)

	return func() {
		for {
			select {
			case err := <-sub.Err():
				log.ErrorContext(ctx, "error in log subscription", "error", err)
			case targetEvent := <-targetEvents:
				userOperation, err := contract.ParseUserOperationEvent(targetEvent)
				if err != nil {
					log.ErrorContext(ctx, "failed to unpack data", "error", err)
					continue
				}

				log.InfoContext(ctx, fmt.Sprintf("new event: %v\n", userOperation))

				go analyse(targetEvent.Address, userOperation)
			case <-time.After(time.Second * 3):
				log.InfoContext(ctx, "no events")
			}
		}
	}, nil
}

func analyser(cctx *cli.Context) func(common.Address, *contracts.EntryPointUserOperationEvent) {
	bundlersList := strings.Split(bundlers.Get(cctx), ",")
	bundlers := make(map[string]struct{})
	for _, bx := range bundlersList {
		bundlers[bx] = struct{}{}
	}

	return func(address common.Address, userOperation *contracts.EntryPointUserOperationEvent) {
		success := "0"
		if userOperation.Success {
			success = "1"
		}

		bundler := "0"
		if _, ok := bundlers[address.Hex()]; ok {
			bundler = "1"
		}

		log.InfoContext(cctx.Context, "processing event", "bundler", bundler, "success", success, "userOp", userOperation)

		n := time.Now()
		hour := n.Format("2006-01-02 15")
		day := n.Format("2006-01-02")
		week := n.Format("2006-W01")
		opsHourly.WithLabelValues(success, bundler, hour).Inc()
		opsDaily.WithLabelValues(success, bundler, day).Inc()
		opsWeekly.WithLabelValues(success, bundler, week).Inc()
	}
}
