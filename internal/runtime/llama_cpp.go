package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modeldock/modeldock/internal/model"
)

const llamaServerIdleTimeout = 10 * time.Minute

var llamaServer = struct {
	sync.Mutex
	cmd      *exec.Cmd
	done     chan struct{}
	baseURL  string
	key      string
	stderr   *bytes.Buffer
	lastUsed time.Time
	idle     *time.Timer
}{}

func llamaCPPGenerate(ctx context.Context, m model.Manifest, input Input) (string, error) {
	server, err := llamaServerPath(os.Getenv("MODELDOCK_LLAMA_CPP_PATH"))
	if err != nil {
		return "", err
	}
	modelPath, err := ggufPath(m)
	if err != nil {
		return "", err
	}
	key := modelPath + "\x00" + server + "\x00" + os.Getenv("MODELDOCK_LLAMA_CPP_MODE")
	llamaServer.Lock()
	defer llamaServer.Unlock()
	if llamaServer.key != key || llamaServer.done == nil || processStopped(llamaServer.done) {
		stopLlamaServerLocked()
		if err = startLlamaServerLocked(ctx, key, server, modelPath); err != nil {
			stopLlamaServerLocked()
			return "", err
		}
	}
	baseURL := llamaServer.baseURL
	client := &http.Client{Timeout: 0}
	if err = waitForLlamaServer(ctx, client, baseURL, llamaServer.done, func() error { return nil }, llamaServer.stderr); err != nil {
		stopLlamaServerLocked()
		return "", err
	}
	messages := input.Messages
	if len(messages) == 0 {
		messages = []Message{{Role: "user", Content: input.Prompt}}
	}
	payload, err := json.Marshal(map[string]any{"model": "local-model", "messages": messages, "stream": false, "max_tokens": 2048, "temperature": 0.7})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		stopLlamaServerLocked()
		return "", fmt.Errorf("llama-server request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		stopLlamaServerLocked()
		return "", fmt.Errorf("llama-server returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		stopLlamaServerLocked()
		return "", fmt.Errorf("decode llama-server response: %w", err)
	}
	if len(result.Choices) == 0 {
		stopLlamaServerLocked()
		return "", errors.New("llama-server returned no response choices")
	}
	scheduleLlamaServerStopLocked()
	return result.Choices[0].Message.Content, nil
}

func startLlamaServerLocked(ctx context.Context, key, server, modelPath string) error {
	port, err := freeTCPPort()
	if err != nil {
		return fmt.Errorf("allocate llama.cpp port: %w", err)
	}
	args := []string{"--model", modelPath, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--ctx-size", "4096"}
	if strings.EqualFold(os.Getenv("MODELDOCK_LLAMA_CPP_MODE"), "cpu") {
		args = append(args, "--n-gpu-layers", "0")
	} else {
		args = append(args, "--n-gpu-layers", "99")
	}
	cmd := exec.Command(server, args...)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 0}
	if err = waitForLlamaServer(ctx, client, baseURL, done, func() error { return waitErr }, stderr); err != nil {
		_ = cmd.Process.Kill()
		<-done
		return err
	}
	llamaServer.cmd, llamaServer.done, llamaServer.baseURL = cmd, done, baseURL
	llamaServer.key, llamaServer.stderr = key, stderr
	return nil
}

func processStopped(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func scheduleLlamaServerStopLocked() {
	llamaServer.lastUsed = time.Now()
	if llamaServer.idle != nil {
		llamaServer.idle.Stop()
	}
	key := llamaServer.key
	llamaServer.idle = time.AfterFunc(llamaServerIdleTimeout, func() {
		llamaServer.Lock()
		defer llamaServer.Unlock()
		if llamaServer.key == key && time.Since(llamaServer.lastUsed) >= llamaServerIdleTimeout {
			stopLlamaServerLocked()
		}
	})
}

func stopLlamaServerLocked() {
	if llamaServer.idle != nil {
		llamaServer.idle.Stop()
		llamaServer.idle = nil
	}
	if llamaServer.cmd != nil && llamaServer.cmd.Process != nil && llamaServer.done != nil && !processStopped(llamaServer.done) {
		_ = llamaServer.cmd.Process.Kill()
		<-llamaServer.done
	}
	llamaServer.cmd, llamaServer.done = nil, nil
	llamaServer.baseURL, llamaServer.key, llamaServer.stderr = "", "", nil
}

func StopLlamaCPP() {
	llamaServer.Lock()
	defer llamaServer.Unlock()
	stopLlamaServerLocked()
}

func waitForLlamaServer(ctx context.Context, client *http.Client, baseURL string, done <-chan struct{}, waitErr func() error, stderr *bytes.Buffer) error {
	timer := time.NewTimer(3 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			err := waitErr()
			if err == nil {
				err = errors.New("llama-server exited before becoming ready")
			}
			return fmt.Errorf("llama-server stopped during model loading: %w: %s", err, strings.TrimSpace(stderr.String()))
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("llama-server did not finish loading the model within 3 minutes")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				return nil
			}
		}
	}
}

func llamaServerPath(configured string) (string, error) {
	if configured == "" {
		return "", errors.New("未找到 llama-server 路径，请在运行时设置中配置 llama-server.exe")
	}
	p := filepath.Clean(configured)
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		p = filepath.Join(p, llamaServerName())
	} else if strings.Contains(strings.ToLower(filepath.Base(p)), "llama-cli") {
		p = filepath.Join(filepath.Dir(p), llamaServerName())
	}
	if info, err := os.Stat(p); err != nil || info.IsDir() {
		return "", fmt.Errorf("找不到 llama-server 可执行文件：%s", p)
	}
	return p, nil
}

func llamaServerName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

func freeTCPPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
