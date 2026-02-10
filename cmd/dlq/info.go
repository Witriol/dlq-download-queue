package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
)

type metaView struct {
	OutDirPresets []string `json:"out_dir_presets"`
	Version       string   `json:"version"`
}

func cmdInfo(args []string) {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	api := fs.String("api", apiBase(), "api base URL")
	fs.Parse(args)

	fmt.Println("CLI:")
	fmt.Printf("  version: %s\n", versionString())
	fmt.Printf("  api: %s\n", *api)
	if v, ok := os.LookupEnv("DLQ_API"); ok {
		fmt.Printf("  env.DLQ_API: %s\n", v)
	} else {
		fmt.Println("  env.DLQ_API: (unset)")
	}

	var meta metaView
	if err := getJSON(*api+"/meta", &meta); err != nil {
		fmt.Println("")
		fmt.Println("Server:")
		fmt.Printf("  status: error (%v)\n", err)
		return
	}

	fmt.Println("")
	fmt.Println("Server:")
	fmt.Println("  status: ok")
	if meta.Version != "" {
		fmt.Printf("  version: %s\n", meta.Version)
	}
	fmt.Printf("  out_dir_presets: %d\n", len(meta.OutDirPresets))
	for _, p := range meta.OutDirPresets {
		fmt.Printf("    - %s\n", p)
	}

	var settings map[string]interface{}
	if err := getJSON(*api+"/api/settings", &settings); err == nil && len(settings) > 0 {
		fmt.Println("  settings:")
		keys := make([]string, 0, len(settings))
		for k := range settings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("    %s: %v\n", k, settings[k])
		}
	}
}
