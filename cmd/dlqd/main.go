package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Witriol/my-downloader/internal/api"
	"github.com/Witriol/my-downloader/internal/db"
	"github.com/Witriol/my-downloader/internal/downloader"
	"github.com/Witriol/my-downloader/internal/queue"
	"github.com/Witriol/my-downloader/internal/resolver"
)

func main() {
	log.Printf("dlqd %s starting", versionString())
	stateDir := getenv("DLQ_STATE_DIR", "/state")
	dbPath := getenv("DLQ_DB", stateDir+"/dlq.db")
	listen := getenv("DLQ_HTTP_ADDR", "0.0.0.0:8080")
	aria2RPC := getenv("ARIA2_RPC", "http://127.0.0.1:6800/jsonrpc")
	aria2Secret := getenv("ARIA2_SECRET", "")
	concurrency := getenvInt("DLQ_CONCURRENCY", 2)

	dbConn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	store := queue.NewStore(dbConn)
	service := queue.NewService(store, downloader.NewAria2Client(aria2RPC, aria2Secret))

	resRegistry := resolver.NewRegistry(
		resolver.NewWebshareResolver(),
		resolver.NewMegaResolver(),
		resolver.NewHTTPResolver(),
	)

	runner := &queue.Runner{
		Store:       store,
		Resolvers:   resRegistry,
		Downloader:  downloader.NewAria2Client(aria2RPC, aria2Secret),
		Concurrency: concurrency,
		PollEvery:   2 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Start(ctx)

	server := &api.Server{Queue: service}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("dlqd listening on %s", listen)
	if err := http.Serve(ln, server.Handler()); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}

func versionString() string {
	if version == "" {
		return "dev"
	}
	return version
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return def
}
