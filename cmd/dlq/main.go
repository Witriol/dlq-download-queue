package main

import (
	"fmt"
	"os"
)

const defaultAPI = "http://127.0.0.1:8099"

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	switch os.Args[1] {
	case "--version", "version":
		fmt.Println(versionString())
		return
	case "info":
		cmdInfo(os.Args[2:])
	case "add":
		cmdAdd(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "files":
		cmdFiles(os.Args[2:])
	case "logs":
		cmdLogs(os.Args[2:])
	case "retry":
		cmdRetry(os.Args[2:])
	case "remove":
		cmdRemove(os.Args[2:])
	case "clear":
		cmdClear(os.Args[2:])
	case "purge":
		cmdPurge(os.Args[2:])
	case "pause":
		cmdPause(os.Args[2:])
	case "resume":
		cmdResume(os.Args[2:])
	case "settings":
		cmdSettings(os.Args[2:])
	case "help":
		usage()
	default:
		usage()
	}
}

func versionString() string {
	if version == "" {
		return "dlq (dev)"
	}
	return "dlq " + version
}

func usage() {
	fmt.Println("DLQ - headless download queue CLI")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  dlq <command> [options]")
	fmt.Println("")
	fmt.Println("Core:")
	fmt.Println("  dlq add <url> [<url2> ...] --out /data/downloads [--name optional] [--site mega|webshare|http|https] [--archive-password batch-pass]")
	fmt.Println("  dlq add --file urls.txt --out /data/downloads")
	fmt.Println("  dlq add --stdin --out /data/downloads")
	fmt.Println("  dlq status [--watch] [--interval 1] [--status queued|resolving|downloading|paused|decrypting|completed|failed|decrypt_failed|deleted]")
	fmt.Println("  dlq files")
	fmt.Println("  dlq logs <job_id> [--tail 50]")
	fmt.Println("  dlq info [--api http://127.0.0.1:8099]")
	fmt.Println("  dlq help")
	fmt.Println("  dlq version | dlq --version")
	fmt.Println("")
	fmt.Println("Job Control:")
	fmt.Println("  dlq pause <job_id>")
	fmt.Println("  dlq resume <job_id>")
	fmt.Println("  dlq retry <job_id>")
	fmt.Println("  dlq remove <job_id>")
	fmt.Println("")
	fmt.Println("Maintenance:")
	fmt.Println("  dlq clear      (clear completed jobs)")
	fmt.Println("  dlq purge      (delete all jobs and events)")
	fmt.Println("")
	fmt.Println("Configuration:")
	fmt.Println("  dlq settings [--concurrency <1-10>] [--auto-decrypt <true|false>]")
}

func apiBase() string {
	if v := os.Getenv("DLQ_API"); v != "" {
		return v
	}
	return defaultAPI
}
