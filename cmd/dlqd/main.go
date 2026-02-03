package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	outDirPresets := splitCSV(getenvOptional("DLQ_OUT_DIR_PRESETS", "/data/tvshows,/data/movies"))

	dbConn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	store := queue.NewStore(dbConn)
	service := queue.NewService(store, downloader.NewAria2Client(aria2RPC, aria2Secret))

	webshareResolver := resolver.NewWebshareResolver()
	megaResolver := resolver.NewMegaResolver()
	httpResolver := resolver.NewHTTPResolver()
	resRegistry := resolver.NewRegistry(
		webshareResolver,
		megaResolver,
		httpResolver,
	)
	resRegistry.RegisterSite("webshare", webshareResolver)
	resRegistry.RegisterSite("mega", megaResolver)
	resRegistry.RegisterSite("http", httpResolver)
	resRegistry.RegisterSite("https", httpResolver)

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

	server := &api.Server{
		Queue: service,
		Meta:  &api.Meta{OutDirPresets: outDirPresets},
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("dlqd listening on %s", listen)
	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if err := httpServer.Serve(ln); err != nil {
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

func getenvOptional(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
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

func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
