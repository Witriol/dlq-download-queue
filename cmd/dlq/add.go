package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func cmdAdd(args []string) {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			usage()
			return
		}
	}
	urls, outDir, name, site, api, err := parseAddArgs(args)
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
			"url":     urlStr,
			"out_dir": outDir,
			"name":    name,
			"site":    site,
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

func parseAddArgs(args []string) (urls []string, outDir, name, site string, api string, err error) {
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
				return nil, "", "", "", "", fmt.Errorf("missing value for %s", key)
			}
			switch key {
			case "--out":
				outDir = val
			case "--name":
				name = val
			case "--site":
				site = val
			case "--api":
				api = val
			case "--file":
				files = append(files, val)
			default:
				return nil, "", "", "", "", fmt.Errorf("unknown flag %s", key)
			}
			continue
		}
		urls = append(urls, arg)
	}
	if useStdin {
		stdinURLs, err := readURLs(os.Stdin)
		if err != nil {
			return nil, "", "", "", "", err
		}
		urls = append(urls, stdinURLs...)
	}
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, "", "", "", "", err
		}
		fileURLs, err := readURLs(f)
		_ = f.Close()
		if err != nil {
			return nil, "", "", "", "", err
		}
		urls = append(urls, fileURLs...)
	}
	return urls, outDir, name, site, api, nil
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
