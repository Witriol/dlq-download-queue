package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

func printJobs(jobs []jobView) {
	if len(jobs) == 0 {
		fmt.Println("No jobs.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tPROGRESS\tSPEED\tETA\tOUT\tNAME/URL")
	for _, j := range jobs {
		done := j.BytesDone
		if done == 0 && (j.Status == "completed" || j.Status == "decrypting" || j.Status == "decrypt_failed") && j.SizeBytes > 0 {
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
		case "queued", "resolving", "downloading", "paused", "decrypting":
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
