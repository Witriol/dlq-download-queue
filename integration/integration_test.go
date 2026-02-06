package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	imageOnce sync.Once
	imageErr  error
)

func TestRunDevScriptAPIOnlyKeepsContainerRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in -short mode")
	}
	requireDocker(t)
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, ".env.dev")); err != nil {
		t.Skip(".env.dev not found; integration test requires local dev env file")
	}
	exists, err := dockerContainerExists("dlq")
	if err != nil {
		t.Fatalf("check existing dlq container: %v", err)
	}
	backupName := ""
	if exists {
		backupName = fmt.Sprintf("dlq-pretest-%d", time.Now().UnixNano())
		if err := dockerRenameContainer(context.Background(), "dlq", backupName); err != nil {
			t.Fatalf("rename existing dlq container: %v", err)
		}
		t.Logf("temporarily renamed existing dlq container to %s", backupName)
	}
	t.Cleanup(func() {
		_ = dockerRemoveContainer(context.Background(), "dlq")
		if backupName != "" {
			_ = dockerRenameContainer(context.Background(), backupName, "dlq")
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	output, err := runCmd(ctx, root, []string{"RUN_UI=0"}, "bash", "scripts/run-dev.sh")
	if err != nil {
		t.Fatalf("run-dev failed: %v\n%s", err, output)
	}

	re := regexp.MustCompile(`DLQ API running at (http://127\.0\.0\.1:\d+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) != 2 {
		t.Fatalf("could not parse API URL from run-dev output:\n%s", output)
	}
	apiBase := matches[1]
	t.Logf("run-dev reported API: %s", apiBase)

	running, err := dockerContainerRunning("dlq")
	if err != nil {
		t.Fatalf("check running dlq container: %v", err)
	}
	if !running {
		t.Fatalf("expected dlq container to stay running after RUN_UI=0")
	}

	if err := waitForEndpoint(apiBase+"/meta", 45*time.Second); err != nil {
		t.Fatalf("api did not become ready: %v", err)
	}
	t.Logf("validated API endpoint: %s/meta", apiBase)
}

func TestPauseResumeHappyPathWithLiveAria2(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test in -short mode")
	}
	requireDocker(t)
	root := repoRoot(t)
	ensureLocalImage(t, root)

	stateDir := t.TempDir()
	dataDir := t.TempDir()
	hostPort := freeTCPPort(t)
	containerName := fmt.Sprintf("dlq-itest-%d", time.Now().UnixNano())
	t.Logf("starting integration container: %s", containerName)
	t.Cleanup(func() {
		_ = dockerRemoveContainer(context.Background(), containerName)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := runCmd(ctx, root, nil, "docker",
		"run", "-d",
		"--name", containerName,
		"-v", dataDir+":/data",
		"-v", stateDir+":/state",
		"-e", "DLQ_HTTP_ADDR=0.0.0.0:8099",
		"-e", "DLQ_HTTP_PORT=8099",
		"-e", "DATA_DOWNLOADS=/tmp:/data",
		"-e", "ARIA2_MAX_CONNECTION_PER_SERVER=1",
		"-p", fmt.Sprintf("%d:8099", hostPort),
		"dlq:local",
	); err != nil {
		t.Fatalf("start test container: %v", err)
	}

	apiBase := fmt.Sprintf("http://127.0.0.1:%d", hostPort)
	t.Logf("container API endpoint: %s", apiBase)
	if err := waitForEndpoint(apiBase+"/meta", 45*time.Second); err != nil {
		t.Fatalf("api not ready: %v", err)
	}

	slowPort, stopSlowServer := startSlowHTTPServer(t)
	defer stopSlowServer()

	urlCandidates := []string{
		fmt.Sprintf("http://host.docker.internal:%d/slow.bin", slowPort),
	}
	if gw, err := dockerBridgeGateway(); err == nil && gw != "" {
		urlCandidates = append(urlCandidates, fmt.Sprintf("http://%s:%d/slow.bin", gw, slowPort))
	}

	var jobID int64
	var started bool
	for _, candidate := range urlCandidates {
		t.Logf("trying download source: %s", candidate)
		id, err := createJob(apiBase, candidate, "/data")
		if err != nil {
			t.Fatalf("create job for %s: %v", candidate, err)
		}
		t.Logf("created job id=%d", id)
		st, _, err := waitForStatuses(apiBase, id, 60*time.Second, "downloading", "failed", "completed")
		if err != nil {
			t.Fatalf("wait for job %d (%s): %v", id, candidate, err)
		}
		if st == "downloading" {
			jobID = id
			started = true
			t.Logf("job %d reached downloading", id)
			break
		}
		t.Logf("job %d ended in status=%s; trying next source", id, st)
	}
	if !started {
		t.Skip("could not establish a reachable local test file URL from inside container")
	}

	pauseURL := fmt.Sprintf("%s/jobs/%d/pause", apiBase, jobID)
	pauseOK := false
	for i := 0; i < 10; i++ {
		status, body, err := postJSON(pauseURL, map[string]any{})
		if err != nil {
			t.Fatalf("pause request failed: %v", err)
		}
		if status == http.StatusOK {
			pauseOK = true
			t.Logf("pause accepted on attempt %d", i+1)
			break
		}
		if status != http.StatusConflict {
			t.Fatalf("pause returned unexpected status %d body=%s", status, strings.TrimSpace(string(body)))
		}
		t.Logf("pause attempt %d returned conflict; retrying", i+1)
		time.Sleep(750 * time.Millisecond)
	}
	if !pauseOK {
		t.Fatalf("pause never succeeded after retries")
	}

	st, body, err := waitForStatuses(apiBase, jobID, 30*time.Second, "paused")
	if err != nil {
		t.Fatalf("wait paused: %v", err)
	}
	if st != "paused" {
		t.Fatalf("expected paused status, got %s body=%s", st, strings.TrimSpace(string(body)))
	}
	t.Logf("job %d is paused", jobID)

	resumeURL := fmt.Sprintf("%s/jobs/%d/resume", apiBase, jobID)
	status, body, err := postJSON(resumeURL, map[string]any{})
	if err != nil {
		t.Fatalf("resume request failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("resume expected 200, got %d body=%s", status, strings.TrimSpace(string(body)))
	}
	t.Logf("resume accepted for job %d", jobID)

	st, body, err = waitForStatuses(apiBase, jobID, 30*time.Second, "downloading", "completed")
	if err != nil {
		t.Fatalf("wait resume progress: %v", err)
	}
	if st != "downloading" && st != "completed" {
		t.Fatalf("expected downloading/completed after resume, got %s body=%s", st, strings.TrimSpace(string(body)))
	}
	t.Logf("job %d progressed after resume with status=%s", jobID, st)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("cannot determine caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func requireDocker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := runCmd(ctx, "", nil, "docker", "info"); err != nil {
		t.Skipf("docker is unavailable: %v", err)
	}
}

func ensureLocalImage(t *testing.T, root string) {
	t.Helper()
	imageOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := runCmd(ctx, "", nil, "docker", "image", "inspect", "dlq:local"); err == nil {
			return
		}
		buildCtx, buildCancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer buildCancel()
		_, imageErr = runCmd(buildCtx, root, nil, "docker", "build", "-t", "dlq:local", ".")
	})
	if imageErr != nil {
		t.Fatalf("prepare dlq:local image: %v", imageErr)
	}
}

func runCmd(ctx context.Context, dir string, extraEnv []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = os.Environ()
	if len(extraEnv) > 0 {
		cmd.Env = append(cmd.Env, extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func dockerContainerExists(name string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "", nil, "docker", "ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == name, nil
}

func dockerContainerRunning(name string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "", nil, "docker", "ps", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == name, nil
}

func dockerRemoveContainer(ctx context.Context, name string) error {
	_, err := runCmd(ctx, "", nil, "docker", "rm", "-f", name)
	return err
}

func dockerRenameContainer(ctx context.Context, from, to string) error {
	_, err := runCmd(ctx, "", nil, "docker", "rename", from, to)
	return err
}

func dockerBridgeGateway() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "", nil, "docker", "network", "inspect", "bridge", "--format", "{{(index .IPAM.Config 0).Gateway}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func waitForEndpoint(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout waiting for %s", url)
	}
	return lastErr
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("get free tcp port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func createJob(apiBase, url, outDir string) (int64, error) {
	status, body, err := postJSON(apiBase+"/jobs", map[string]any{
		"url":     url,
		"out_dir": outDir,
	})
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("create job returned %d: %s", status, strings.TrimSpace(string(body)))
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	if resp.ID <= 0 {
		return 0, fmt.Errorf("invalid id in create response: %s", strings.TrimSpace(string(body)))
	}
	return resp.ID, nil
}

func postJSON(url string, payload any) (status int, body []byte, err error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("content-type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

func waitForStatuses(apiBase string, jobID int64, timeout time.Duration, allowed ...string) (status string, body []byte, err error) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, s := range allowed {
		allowedSet[s] = struct{}{}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(timeout)
	lastBody := []byte{}
	for time.Now().Before(deadline) {
		resp, reqErr := client.Get(fmt.Sprintf("%s/jobs/%d", apiBase, jobID))
		if reqErr != nil {
			err = reqErr
			time.Sleep(500 * time.Millisecond)
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		lastBody = raw
		if resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("status %d", resp.StatusCode)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		var job struct {
			Status string `json:"status"`
		}
		if unmarshalErr := json.Unmarshal(raw, &job); unmarshalErr != nil {
			err = unmarshalErr
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if _, ok := allowedSet[job.Status]; ok {
			return job.Status, raw, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err == nil {
		err = fmt.Errorf("timeout waiting for job status %v", allowed)
	}
	return "", lastBody, err
}

func startSlowHTTPServer(t *testing.T) (port int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen slow server: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/slow.bin", func(w http.ResponseWriter, r *http.Request) {
		const total = int64(64 * 1024 * 1024)
		start := int64(0)
		end := total - 1
		if rg := r.Header.Get("Range"); rg != "" {
			if strings.HasPrefix(rg, "bytes=") {
				spec := strings.TrimPrefix(rg, "bytes=")
				parts := strings.SplitN(spec, "-", 2)
				if len(parts) == 2 {
					if parts[0] != "" {
						if v, parseErr := strconv.ParseInt(parts[0], 10, 64); parseErr == nil && v >= 0 && v < total {
							start = v
						}
					}
					if parts[1] != "" {
						if v, parseErr := strconv.ParseInt(parts[1], 10, 64); parseErr == nil && v >= start && v < total {
							end = v
						}
					}
				}
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		}
		if end < start {
			end = start
		}
		length := end - start + 1
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusPartialContent)
		}

		chunk := bytes.Repeat([]byte("x"), 64*1024)
		remaining := length
		for remaining > 0 {
			n := len(chunk)
			if remaining < int64(n) {
				n = int(remaining)
			}
			if _, writeErr := w.Write(chunk[:n]); writeErr != nil {
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			remaining -= int64(n)
			time.Sleep(100 * time.Millisecond)
		}
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = server.Serve(ln)
	}()

	stop = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return ln.Addr().(*net.TCPAddr).Port, stop
}
