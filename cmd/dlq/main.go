package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
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
	fmt.Println("  dlq add <url> [<url2> ...] --out /data/downloads [--name optional] [--site mega|webshare|http|https]")
	fmt.Println("  dlq add --file urls.txt --out /data/downloads")
	fmt.Println("  dlq add --stdin --out /data/downloads")
	fmt.Println("  dlq status [--watch] [--interval 1] [--status <state>]")
	fmt.Println("  dlq files")
	fmt.Println("  dlq logs <job_id> [--tail 50]")
	fmt.Println("  dlq retry <job_id>")
	fmt.Println("  dlq pause <job_id>")
	fmt.Println("  dlq resume <job_id>")
	fmt.Println("  dlq remove <job_id>")
	fmt.Println("  dlq clear")
	fmt.Println("  dlq settings [--concurrency <1-10>]")
	fmt.Println("")
	fmt.Println("Env:")
	fmt.Println("  DLQ_API=http://127.0.0.1:8099")
}

func apiBase() string {
	if v := os.Getenv("DLQ_API"); v != "" {
		return v
	}
	return defaultAPI
}

type jobView struct {
	ID            int64  `json:"id"`
	URL           string `json:"url"`
	Site          string `json:"site"`
	OutDir        string `json:"out_dir"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Filename      string `json:"filename"`
	SizeBytes     int64  `json:"size_bytes"`
	BytesDone     int64  `json:"bytes_done"`
	DownloadSpeed int64  `json:"download_speed"`
	EtaSeconds    int64  `json:"eta_seconds"`
	Error         string `json:"error"`
	ErrorCode     string `json:"error_code"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func cmdAdd(args []string) {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			usage()
			return
		}
	}
	urls, outDir, name, site, maxAttempts, api, err := parseAddArgs(args)
	if err != nil {
		fmt.Println("error:", err)
		fmt.Println("usage: dlq add <url> [<url2> ...] --out /data/downloads [--name optional]")
		return
	}
	if len(urls) == 0 || outDir == "" {
		fmt.Println("usage: dlq add <url> [<url2> ...] --out /data/downloads [--name optional]")
		return
	}
	if len(urls) > 1 && name != "" {
		fmt.Println("error: --name can only be used with a single URL")
		return
	}
	hadErr := false
	for _, urlStr := range urls {
		payload := map[string]any{
			"url":          urlStr,
			"out_dir":      outDir,
			"name":         name,
			"site":         site,
			"max_attempts": maxAttempts,
		}
		var resp map[string]any
		if err := postJSON(api+"/jobs", payload, &resp); err != nil {
			fmt.Printf("error for %s: %v\n", urlStr, err)
			hadErr = true
			continue
		}
		fmt.Printf("queued job id %v (%s)\n", resp["id"], urlStr)
	}
	if hadErr {
		os.Exit(1)
	}
}

func parseAddArgs(args []string) (urls []string, outDir, name, site string, maxAttempts int, api string, err error) {
	maxAttempts = 5
	api = apiBase()
	var files []string
	useStdin := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			key := arg
			val := ""
			if strings.Contains(arg, "=") {
				parts := strings.SplitN(arg, "=", 2)
				key = parts[0]
				val = parts[1]
			} else if key == "--stdin" {
				useStdin = true
				continue
			} else if i+1 < len(args) {
				val = args[i+1]
				i++
			} else {
				return nil, "", "", "", 0, "", fmt.Errorf("missing value for %s", key)
			}
			switch key {
			case "--out":
				outDir = val
			case "--name":
				name = val
			case "--site":
				site = val
			case "--max-attempts":
				parsed, convErr := strconv.Atoi(val)
				if convErr != nil {
					return nil, "", "", "", 0, "", fmt.Errorf("invalid --max-attempts")
				}
				maxAttempts = parsed
			case "--api":
				api = val
			case "--file":
				files = append(files, val)
			default:
				return nil, "", "", "", 0, "", fmt.Errorf("unknown flag %s", key)
			}
			continue
		}
		urls = append(urls, arg)
	}
	if useStdin {
		stdinURLs, err := readURLs(os.Stdin)
		if err != nil {
			return nil, "", "", "", 0, "", err
		}
		urls = append(urls, stdinURLs...)
	}
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, "", "", "", 0, "", err
		}
		fileURLs, err := readURLs(f)
		_ = f.Close()
		if err != nil {
			return nil, "", "", "", 0, "", err
		}
		urls = append(urls, fileURLs...)
	}
	return urls, outDir, name, site, maxAttempts, api, nil
}

func readURLs(r *os.File) ([]string, error) {
	var out []string
	scanner := bufio.NewScanner(r)
	// Allow for long URLs in batch files.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, scanner.Err()
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	status := fs.String("status", "", "filter by status")
	api := fs.String("api", apiBase(), "api base URL")
	watch := fs.Bool("watch", false, "refresh every second")
	interval := fs.Int("interval", 1, "refresh interval in seconds")
	fs.Parse(args)
	if *interval <= 0 {
		*interval = 1
	}
	for {
		if *watch {
			fmt.Print("\033[H\033[2J")
		}
		var jobs []jobView
		url := *api + "/jobs"
		if *status != "" {
			url += "?status=" + *status
			if *status == "deleted" {
				url += "&include_deleted=1"
			}
		}
		if err := getJSON(url, &jobs); err != nil {
			fmt.Println("error:", err)
			return
		}
		counts := map[string]int{}
		for _, j := range jobs {
			counts[j.Status]++
		}
		active := counts["queued"] + counts["resolving"] + counts["downloading"] + counts["paused"]
		done := counts["completed"] + counts["failed"]
		fmt.Printf("Jobs: %d total | active: %d (queued %d, resolving %d, downloading %d, paused %d) | done: %d (completed %d, failed %d)\n",
			len(jobs), active, counts["queued"], counts["resolving"], counts["downloading"], counts["paused"], done, counts["completed"], counts["failed"])
		printJobs(jobs)
		if !*watch || !hasActiveJobs(jobs) {
			return
		}
		time.Sleep(time.Duration(*interval) * time.Second)
	}
}

func cmdFiles(args []string) {
	fs := flag.NewFlagSet("files", flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)
	var jobs []jobView
	if err := getJSON(*api+"/jobs?include_deleted=1", &jobs); err != nil {
		fmt.Println("error:", err)
		return
	}
	if len(jobs) == 0 {
		fmt.Println("No jobs.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tPATH\tURL")
	for _, j := range jobs {
		path := filePath(j)
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", j.ID, j.Status, path, j.URL)
	}
	_ = tw.Flush()
}

func cmdLogs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	limit := fs.Int("tail", 50, "number of log lines")
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Println("usage: dlq logs <job_id>")
		return
	}
	id := fs.Arg(0)
	var lines []string
	if err := getJSON(fmt.Sprintf("%s/jobs/%s/events?limit=%d", *api, id, *limit), &lines); err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, line := range lines {
		fmt.Println(line)
	}
}

func cmdRetry(args []string) {
	fs := flag.NewFlagSet("retry", flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Println("usage: dlq retry <job_id>")
		return
	}
	id := fs.Arg(0)
	if err := postJSON(fmt.Sprintf("%s/jobs/%s/retry", *api, id), map[string]any{}, nil); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("ok")
}

func cmdRemove(args []string) {
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Println("usage: dlq remove <job_id>")
		return
	}
	id := fs.Arg(0)
	if err := postJSON(fmt.Sprintf("%s/jobs/%s/remove", *api, id), map[string]any{}, nil); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("ok")
}

func cmdClear(args []string) {
	fs := flag.NewFlagSet("clear", flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)
	if err := postJSON(*api+"/jobs/clear", map[string]any{}, nil); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("ok")
}

func cmdPause(args []string) {
	fs := flag.NewFlagSet("pause", flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Println("usage: dlq pause <job_id>")
		return
	}
	id := fs.Arg(0)
	if err := postJSON(fmt.Sprintf("%s/jobs/%s/pause", *api, id), map[string]any{}, nil); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("ok")
}

func cmdResume(args []string) {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Println("usage: dlq resume <job_id>")
		return
	}
	id := fs.Arg(0)
	if err := postJSON(fmt.Sprintf("%s/jobs/%s/resume", *api, id), map[string]any{}, nil); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("ok")
}

func cmdSettings(args []string) {
	fs := flag.NewFlagSet("settings", flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	concurrency := fs.Int("concurrency", 0, "set concurrency (1-10)")
	fs.Parse(args)

	// If no flags set, just show current settings
	if *concurrency == 0 {
		var settings map[string]interface{}
		if err := getJSON(*api+"/api/settings", &settings); err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println("Current settings:")
		for k, v := range settings {
			fmt.Printf("  %s: %v\n", k, v)
		}
		return
	}

	// Update settings
	updates := make(map[string]interface{})
	if *concurrency > 0 {
		updates["concurrency"] = *concurrency
	}

	var result map[string]interface{}
	if err := postJSON(*api+"/api/settings", updates, &result); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("Settings updated:")
	for k, v := range result {
		fmt.Printf("  %s: %v\n", k, v)
	}
}

func getJSON(url string, out interface{}) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readHTTPError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func postJSON(url string, payload interface{}, out interface{}) error {
	client := &http.Client{Timeout: 10 * time.Second}
	body, _ := json.Marshal(payload)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readHTTPError(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func readHTTPError(resp *http.Response) error {
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		if msg, ok := body["error"]; ok && msg != "" {
			return fmt.Errorf("http %d: %s", resp.StatusCode, msg)
		}
	}
	return fmt.Errorf("http %d", resp.StatusCode)
}

func printJobs(jobs []jobView) {
	if len(jobs) == 0 {
		fmt.Println("No jobs.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tPROGRESS\tSPEED\tETA\tOUT\tNAME/URL")
	for _, j := range jobs {
		done := j.BytesDone
		if done == 0 && j.Status == "completed" && j.SizeBytes > 0 {
			done = j.SizeBytes
		}
		progress := formatProgress(done, j.SizeBytes)
		name := displayName(j)
		speed := formatSpeed(j)
		eta := formatETA(j)
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", j.ID, j.Status, progress, speed, eta, j.OutDir, name)
		if j.ErrorCode != "" {
			fmt.Fprintf(tw, " \t \t \t \t \t \t  error: %s (%s)\n", j.ErrorCode, j.Error)
		}
	}
	_ = tw.Flush()
}

func shortURL(u string) string {
	if len(u) > 64 {
		return u[:61] + "..."
	}
	return u
}

func displayName(j jobView) string {
	name := j.Filename
	if name == "" {
		name = j.Name
	}
	if name == "" {
		name = shortURL(j.URL)
	}
	return name
}

func filePath(j jobView) string {
	name := j.Filename
	if name == "" {
		name = j.Name
	}
	if name == "" {
		return shortURL(j.URL)
	}
	return strings.TrimRight(j.OutDir, "/") + "/" + name
}

func formatProgress(done, total int64) string {
	if total <= 0 {
		return humanBytes(done)
	}
	pct := float64(done) / float64(total) * 100
	return fmt.Sprintf("%s / %s (%.1f%%)", humanBytes(done), humanBytes(total), pct)
}

func hasActiveJobs(jobs []jobView) bool {
	for _, j := range jobs {
		switch j.Status {
		case "queued", "resolving", "downloading", "paused":
			return true
		}
	}
	return false
}

func humanBytes(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	val := float64(n)
	idx := 0
	for val >= 1024 && idx < len(units)-1 {
		val /= 1024
		idx++
	}
	return fmt.Sprintf("%.1f%s", val, units[idx])
}

func formatSpeed(j jobView) string {
	if j.Status != "downloading" || j.DownloadSpeed <= 0 {
		return "-"
	}
	return fmt.Sprintf("%s/s", humanBytes(j.DownloadSpeed))
}

func formatETA(j jobView) string {
	if j.Status != "downloading" || j.EtaSeconds <= 0 {
		return "-"
	}
	return humanDuration(time.Duration(j.EtaSeconds) * time.Second)
}

func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	seconds := int64(d.Seconds())
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
