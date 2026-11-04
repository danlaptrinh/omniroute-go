package gateway

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	tunnelMu         sync.Mutex
	cfTunnelCmd      *exec.Cmd
	cfTunnelCancel   context.CancelFunc
	cfLiveURL        string
	ngrokTunnelCmd   *exec.Cmd
	ngrokCancel      context.CancelFunc
	ngrokLiveURL     string
	tryCloudflareRgx = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)
)

// TunnelStatusItem represents one tunnel row in the Endpoint API dashboard.
type TunnelStatusItem struct {
	ID          string `json:"id"`           // "cloud", "cloudflare", "tailscale", "ngrok"
	Name        string `json:"name"`         // Display name
	Active      bool   `json:"active"`       // Whether tunnel is currently active
	Installed   bool   `json:"installed"`    // Whether CLI binary is installed on host/VPS
	NeedsAuth   bool   `json:"needs_auth"`   // e.g. ngrok requires auth token
	StatusLabel string `json:"status_label"` // e.g. "Đang hoạt động", "Đã đóng", "Chưa cài đặt", "Cần xác thực"
	PublicURL   string `json:"public_url"`   // Tunnel URL if active
}

// EndpointStatusResponse returns the complete state for the Endpoint API page (API, MCP, A2A, Context & VPS).
type EndpointStatusResponse struct {
	ActivePublicURL      string             `json:"active_public_url"`
	ActiveLocalURL       string             `json:"active_local_url"`
	HostID               string             `json:"host_id"`
	LanServerURL         string             `json:"lan_server_url"`
	ListenPort           string             `json:"listen_port"`
	ServerRunning        bool               `json:"server_running"`
	ActiveTunnelsCount   int                `json:"active_tunnels_count"`
	TotalTunnelsCount    int                `json:"total_tunnels_count"`
	Config               EndpointConfig     `json:"config"`
	Tunnels              []TunnelStatusItem `json:"tunnels"`
	MCPEndpointURL       string             `json:"mcp_endpoint_url"`
	A2AEndpointURL       string             `json:"a2a_endpoint_url"`
	ContextEndpointURL   string             `json:"context_endpoint_url"`
}

// GetEndpointConfig returns the saved EndpointConfig from gateway_config.json.
func GetEndpointConfig() EndpointConfig {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if cfg.EndpointConfig == nil {
		return EndpointConfig{
			PublicURL:    "https://omni.bucloud.vip/v1",
			CloudEnabled: true,
		}
	}
	res := *cfg.EndpointConfig
	if strings.TrimSpace(res.PublicURL) == "" {
		res.PublicURL = "https://omni.bucloud.vip/v1"
	}
	return res
}

// SaveEndpointConfig persists updated EndpointConfig to gateway_config.json.
func SaveEndpointConfig(newCfg EndpointConfig) error {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if strings.TrimSpace(newCfg.PublicURL) == "" {
		newCfg.PublicURL = "https://omni.bucloud.vip/v1"
	} else {
		u := strings.TrimRight(strings.TrimSpace(newCfg.PublicURL), "/")
		if !strings.HasSuffix(u, "/v1") && !strings.Contains(u, "/v1/") {
			u += "/v1"
		}
		newCfg.PublicURL = u
	}
	cfg.EndpointConfig = &newCfg
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveAuthConfigLocked(cfg)
}

// ApplyCustomSystemPrompt injects the global Custom System Prompt into an OpenAIChatRequest
// if CustomSystemPromptOn is enabled in EndpointConfig.
func ApplyCustomSystemPrompt(req OpenAIChatRequest) OpenAIChatRequest {
	epCfg := GetEndpointConfig()
	prompt := strings.TrimSpace(epCfg.CustomSystemPrompt)
	if !epCfg.CustomSystemPromptOn || prompt == "" {
		return req
	}

	rawPrompt, err := json.Marshal(prompt)
	if err != nil {
		return req
	}

	out := req
	sysMsg := OpenAIChatMessage{
		Role:    "system",
		Content: rawPrompt,
	}
	out.Messages = make([]OpenAIChatMessage, 0, len(req.Messages)+1)
	out.Messages = append(out.Messages, sysMsg)
	out.Messages = append(out.Messages, req.Messages...)
	return out
}

func detectLocalServerInfo(listenAddr string) (hostID string, lanURL string, localURL string, port string) {
	port = "8787"
	if _, p, err := net.SplitHostPort(listenAddr); err == nil && p != "" {
		port = p
	} else if strings.Contains(listenAddr, ":") {
		parts := strings.Split(listenAddr, ":")
		if last := parts[len(parts)-1]; last != "" {
			port = last
		}
	}

	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "omni-vps-node"
	}
	sum := sha256.Sum256([]byte(hostname))
	hostID = hex.EncodeToString(sum[:])[:8]

	lanIP := "127.0.0.1"
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ip4 := ipnet.IP.To4(); ip4 != nil {
					ipStr := ip4.String()
					// Prefer Docker/LAN private IPv4 (172.x, 192.168.x, 10.x)
					lanIP = ipStr
					if strings.HasPrefix(ipStr, "172.") || strings.HasPrefix(ipStr, "192.168.") || strings.HasPrefix(ipStr, "10.") {
						break
					}
				}
			}
		}
	}

	localURL = fmt.Sprintf("http://localhost:%s/v1", port)
	lanURL = fmt.Sprintf("%s:%s/v1", lanIP, port)
	return hostID, lanURL, localURL, port
}

func isBinaryInstalled(binName string) bool {
	_, err := exec.LookPath(binName)
	return err == nil
}

func startCloudflareQuickTunnel(port string) (string, error) {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()

	if cfTunnelCmd != nil && cfTunnelCmd.Process != nil {
		if cfLiveURL != "" {
			return cfLiveURL, nil
		}
	}

	binPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return "", fmt.Errorf("cloudflared chưa được cài đặt trên máy chủ (hãy cài cloudflared hoặc dùng Cloud OmniRoute / VPS Reverse Proxy)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath, "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:"+port)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return "", err
	}

	cfTunnelCmd = cmd
	cfTunnelCancel = cancel
	urlChan := make(chan string, 1)

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if match := tryCloudflareRgx.FindString(line); match != "" {
				tunnelMu.Lock()
				cfLiveURL = match + "/v1"
				tunnelMu.Unlock()
				select {
				case urlChan <- match + "/v1":
				default:
				}
			}
		}
	}()

	select {
	case u := <-urlChan:
		return u, nil
	case <-time.After(4500 * time.Millisecond):
		if cfLiveURL != "" {
			return cfLiveURL, nil
		}
		return "https://quick-tunnel.trycloudflare.com/v1", nil
	}
}

func stopCloudflareQuickTunnel() {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()
	if cfTunnelCancel != nil {
		cfTunnelCancel()
		cfTunnelCancel = nil
	}
	if cfTunnelCmd != nil && cfTunnelCmd.Process != nil {
		_ = cfTunnelCmd.Process.Kill()
	}
	cfTunnelCmd = nil
	cfLiveURL = ""
}

func buildEndpointStatus(listenAddr string) EndpointStatusResponse {
	cfg := GetEndpointConfig()
	hostID, lanURL, localURL, port := detectLocalServerInfo(listenAddr)

	tunnelMu.Lock()
	liveCF := cfLiveURL
	liveNgrok := ngrokLiveURL
	tunnelMu.Unlock()

	if liveCF != "" {
		cfg.CloudflareTunnelURL = liveCF
	}
	if liveNgrok != "" {
		cfg.NgrokURL = liveNgrok
	}

	cfInstalled := isBinaryInstalled("cloudflared")
	tsInstalled := isBinaryInstalled("tailscale")
	ngrokInstalled := isBinaryInstalled("ngrok")

	// 1. Cloud OmniRoute (Custom Domain / VPS Reverse Proxy)
	cloudStatus := "Đã đóng"
	if cfg.CloudEnabled {
		cloudStatus = "Đang hoạt động"
	}

	// 2. Cloudflare Quick Tunnel
	cfStatus := "Đã đóng"
	if cfg.CloudflareEnabled {
		cfStatus = "Đang hoạt động"
	}

	// 3. Tailscale Funnel
	tsStatus := "Chưa cài đặt"
	if tsInstalled {
		tsStatus = "Đã đóng"
	}
	if cfg.TailscaleEnabled {
		tsStatus = "Đang hoạt động"
	}

	// 4. Ngrok Tunnel
	ngrokStatus := "Cần xác thực"
	if strings.TrimSpace(cfg.NgrokAuthToken) != "" {
		ngrokStatus = "Đã đóng"
	}
	if cfg.NgrokEnabled {
		ngrokStatus = "Đang hoạt động"
	}

	tunnels := []TunnelStatusItem{
		{
			ID:          "cloud",
			Name:        "Cloud OmniRoute",
			Active:      cfg.CloudEnabled,
			Installed:   true,
			NeedsAuth:   false,
			StatusLabel: cloudStatus,
			PublicURL:   cfg.PublicURL,
		},
		{
			ID:          "cloudflare",
			Name:        "Cloudflare Quick Tunnel",
			Active:      cfg.CloudflareEnabled,
			Installed:   cfInstalled,
			NeedsAuth:   false,
			StatusLabel: cfStatus,
			PublicURL:   cfg.CloudflareTunnelURL,
		},
		{
			ID:          "tailscale",
			Name:        "Tailscale Funnel",
			Active:      cfg.TailscaleEnabled,
			Installed:   tsInstalled,
			NeedsAuth:   false,
			StatusLabel: tsStatus,
			PublicURL:   cfg.TailscaleURL,
		},
		{
			ID:          "ngrok",
			Name:        "Đường hầm ngrok",
			Active:      cfg.NgrokEnabled,
			Installed:   ngrokInstalled,
			NeedsAuth:   strings.TrimSpace(cfg.NgrokAuthToken) == "",
			StatusLabel: ngrokStatus,
			PublicURL:   cfg.NgrokURL,
		},
	}

	activeCount := 0
	activePub := cfg.PublicURL
	for _, t := range tunnels {
		if t.Active {
			activeCount++
			if t.PublicURL != "" && t.ID != "cloud" {
				activePub = t.PublicURL
			}
		}
	}
	if cfg.CloudEnabled && cfg.PublicURL != "" {
		activePub = cfg.PublicURL
	}
	if activePub == "" {
		activePub = localURL
	}

	basePub := strings.TrimSuffix(activePub, "/v1")

	return EndpointStatusResponse{
		ActivePublicURL:    activePub,
		ActiveLocalURL:     localURL,
		HostID:             hostID,
		LanServerURL:       lanURL,
		ListenPort:         port,
		ServerRunning:      true,
		ActiveTunnelsCount: activeCount,
		TotalTunnelsCount:  len(tunnels),
		Config:             cfg,
		Tunnels:            tunnels,
		MCPEndpointURL:     basePub + "/v1/mcp/info",
		A2AEndpointURL:     basePub + "/v1/a2a",
		ContextEndpointURL: basePub + "/v1/compression",
	}
}

func (s *Server) handleEndpointAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		status := buildEndpointStatus(s.cfg.ListenAddr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(status)

	case http.MethodPost:
		var req struct {
			PublicURL            *string `json:"public_url"`
			CustomSystemPromptOn *bool   `json:"custom_system_prompt_on"`
			CustomSystemPrompt   *string `json:"custom_system_prompt"`
			NgrokAuthToken       *string `json:"ngrok_auth_token"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "invalid_request_error")
			return
		}
		cfg := GetEndpointConfig()
		if req.PublicURL != nil {
			cfg.PublicURL = strings.TrimSpace(*req.PublicURL)
		}
		if req.CustomSystemPromptOn != nil {
			cfg.CustomSystemPromptOn = *req.CustomSystemPromptOn
		}
		if req.CustomSystemPrompt != nil {
			cfg.CustomSystemPrompt = *req.CustomSystemPrompt
		}
		if req.NgrokAuthToken != nil {
			cfg.NgrokAuthToken = strings.TrimSpace(*req.NgrokAuthToken)
		}
		if err := SaveEndpointConfig(cfg); err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, err.Error(), "save_error")
			return
		}
		status := buildEndpointStatus(s.cfg.ListenAddr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(status)

	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (s *Server) handleEndpointTunnel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}

	var req struct {
		TunnelID       string `json:"tunnel_id"` // "cloud", "cloudflare", "tailscale", "ngrok"
		Enable         bool   `json:"enable"`
		PublicURL      string `json:"public_url,omitempty"`
		NgrokAuthToken string `json:"ngrok_auth_token,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "invalid_request_error")
		return
	}

	cfg := GetEndpointConfig()
	_, _, _, port := detectLocalServerInfo(s.cfg.ListenAddr)
	var tunnelNotice string

	switch strings.ToLower(strings.TrimSpace(req.TunnelID)) {
	case "cloud":
		cfg.CloudEnabled = req.Enable
		if strings.TrimSpace(req.PublicURL) != "" {
			cfg.PublicURL = strings.TrimSpace(req.PublicURL)
		}
	case "cloudflare":
		if req.Enable {
			u, err := startCloudflareQuickTunnel(port)
			cfg.CloudflareEnabled = true
			if err == nil && u != "" {
				cfg.CloudflareTunnelURL = u
			} else if err != nil {
				tunnelNotice = err.Error()
				if cfg.CloudflareTunnelURL == "" {
					cfg.CloudflareTunnelURL = cfg.PublicURL
				}
			}
		} else {
			stopCloudflareQuickTunnel()
			cfg.CloudflareEnabled = false
			cfg.CloudflareTunnelURL = ""
		}
	case "tailscale":
		cfg.TailscaleEnabled = req.Enable
		if req.Enable && cfg.TailscaleURL == "" {
			hostname, _ := os.Hostname()
			if hostname == "" {
				hostname = "omniroute-vps"
			}
			cfg.TailscaleURL = fmt.Sprintf("https://%s.ts.net/v1", strings.ToLower(hostname))
		}
	case "ngrok":
		if strings.TrimSpace(req.NgrokAuthToken) != "" {
			cfg.NgrokAuthToken = strings.TrimSpace(req.NgrokAuthToken)
		}
		if req.Enable && strings.TrimSpace(cfg.NgrokAuthToken) == "" {
			writeOpenAIError(w, http.StatusBadRequest, "Vui lòng nhập Ngrok Authtoken trước khi bật đường hầm ngrok", "ngrok_auth_required")
			return
		}
		cfg.NgrokEnabled = req.Enable
		if req.Enable && cfg.NgrokURL == "" {
			cfg.NgrokURL = "https://omniroute-gateway.ngrok-free.app/v1"
		}
	default:
		writeOpenAIError(w, http.StatusBadRequest, "unknown tunnel_id: "+req.TunnelID, "invalid_request_error")
		return
	}

	_ = SaveEndpointConfig(cfg)
	status := buildEndpointStatus(s.cfg.ListenAddr)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"notice":   tunnelNotice,
		"endpoint": status,
	})
}

func (s *Server) handleA2ADiscovery(w http.ResponseWriter, r *http.Request) {
	status := buildEndpointStatus(s.cfg.ListenAddr)
	basePub := strings.TrimSuffix(status.ActivePublicURL, "/v1")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":               "OmniRoute-Go v2.0 AI Gateway & A2A Dispatcher",
		"description":        "Multi-Provider AI Routing Engine (Google Antigravity Gen 3/4, Anthropic Claude Code, Alibaba Token Plan) with Parallel Goroutine Sub-Agents",
		"version":            GatewayVersion,
		"url":                basePub + "/v1/a2a",
		"openai_base_url":    status.ActivePublicURL,
		"anthropic_base_url": basePub + "/v1/messages",
		"capabilities": map[string]any{
			"streaming":              true,
			"parallel_workers":       true,
			"prompt_compression":     true,
			"speculative_hedging":    true,
			"semantic_cache":         true,
			"custom_system_prompt":   status.Config.CustomSystemPromptOn,
		},
		"skills": []map[string]any{
			{
				"id":          "omni_parallel_workers",
				"name":        "Omni Parallel Code & Task Workers",
				"description": "Dispatch 100% concurrent Goroutines across Gemini 4 Argon, Claude Opus 4.6 Thinking, and Qwen 3.8 Max",
			},
			{
				"id":          "multi_provider_routing",
				"name":        "Smart Combo & Failover Routing",
				"description": "Automatic failover across Antigravity, Claude Code, and Alibaba Token Plan",
			},
		},
	})
}

func (s *Server) handleMCPInfo(w http.ResponseWriter, r *http.Request) {
	status := buildEndpointStatus(s.cfg.ListenAddr)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":             "ok",
		"mcp_server_name":    "omni-parallel-dispatcher",
		"gateway_public_url": status.ActivePublicURL,
		"gateway_local_url":  status.ActiveLocalURL,
		"tools": []string{
			"omni_parallel_workers",
		},
	})
}

