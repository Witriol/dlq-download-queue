package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Witriol/dlq-download-queue/internal/api"
	"github.com/Witriol/dlq-download-queue/internal/db"
	"github.com/Witriol/dlq-download-queue/internal/downloader"
	"github.com/Witriol/dlq-download-queue/internal/queue"
	"github.com/Witriol/dlq-download-queue/internal/resolver"
)

func main() {
	log.Printf("dlqd %s starting", versionString())
	stateDir := getenv("DLQ_STATE_DIR", "/state")
	dbPath := getenv("DLQ_DB", stateDir+"/dlq.db")
	listen := httpListenAddr()
	aria2RPC := getenv("ARIA2_RPC", "http://127.0.0.1:6800/jsonrpc")
	aria2Secret := getenv("ARIA2_SECRET", "")
	outDirPresets := outDirPresetsFromEnv()

	dbConn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	store := queue.NewStore(dbConn)
	service := queue.NewService(store, downloader.NewAria2Client(aria2RPC, aria2Secret), outDirPresets)

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

	settings, err := api.NewSettings(stateDir)
	if err != nil {
		log.Fatalf("settings init: %v", err)
	}

	runner := &queue.Runner{
		Store:            store,
		Resolvers:        resRegistry,
		Downloader:       downloader.NewAria2Client(aria2RPC, aria2Secret),
		ArchiveDecryptor: queue.NewArchiveDecryptor(),
		GetConcurrency:   settings.GetConcurrency,
		GetAutoDecrypt:   settings.GetAutoDecrypt,
		PollEvery:        2 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Start(ctx)

	server := &api.Server{
		Queue:    service,
		Meta:     &api.Meta{OutDirPresets: outDirPresets, Version: versionString()},
		Settings: settings,
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

func httpListenAddr() string {
	if v := os.Getenv("DLQ_HTTP_ADDR"); v != "" {
		return v
	}
	host := os.Getenv("DLQ_HTTP_HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("DLQ_HTTP_PORT")
	if port == "" {
		port = "8099"
	}
	return host + ":" + port
}

func outDirPresetsFromEnv() []string {
	const prefix = "DATA_"
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, prefix) {
			continue
		}
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		mount := strings.TrimSpace(parts[1])
		if mount == "" {
			continue
		}
		containerPath := containerPathFromMount(mount)
		if containerPath == "" {
			continue
		}
		if _, ok := seen[containerPath]; ok {
			continue
		}
		seen[containerPath] = struct{}{}
		out = append(out, containerPath)
	}
	sort.Strings(out)
	return out
}

func containerPathFromMount(mount string) string {
	if mount == "" {
		return ""
	}
	if !strings.Contains(mount, ":") {
		return mount
	}
	parts := strings.Split(mount, ":")
	if len(parts) < 2 {
		return ""
	}
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if strings.Contains(last, "/") {
		return last
	}
	return strings.TrimSpace(parts[len(parts)-2])
}
