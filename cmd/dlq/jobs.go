package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

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
	runJobAction(args, "retry")
}

func cmdRemove(args []string) {
	runJobAction(args, "remove")
}

func cmdPause(args []string) {
	runJobAction(args, "pause")
}

func cmdResume(args []string) {
	runJobAction(args, "resume")
}

func runJobAction(args []string, action string) {
	fs := flag.NewFlagSet(action, flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Printf("usage: dlq %s <job_id>\n", action)
		return
	}
	id := fs.Arg(0)
	if err := postJSON(fmt.Sprintf("%s/jobs/%s/%s", *api, id, action), map[string]any{}, nil); err != nil {
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

func cmdPurge(args []string) {
	fs := flag.NewFlagSet("purge", flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)
	if err := postJSON(*api+"/jobs/purge", map[string]any{}, nil); err != nil {
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

	// If no flags set, just show current settings.
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
