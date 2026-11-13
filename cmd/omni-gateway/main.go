package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"omni-parallel-mcp/gateway"
)

func main() {
	saved := gateway.LoadSavedAuthConfig()
	if err := gateway.InitPostgresDB(""); err != nil {
		log.Printf("[omni-gateway] PostgreSQL connection warning (falling back to Local JSON mode): %v", err)
	}

	var (
		listenAddr   = flag.String("listen", envFirstNonEmpty([]string{"GATEWAY_LISTEN_ADDR", "OMNI_GATEWAY_ADDR"}, "127.0.0.1:8787"), "Local HTTP listen address for the Standalone AI Gateway")
		unixSocket   = flag.String("unix-socket", strings.TrimSpace(os.Getenv("GATEWAY_UNIX_SOCKET")), "Optional Unix domain socket path for zero-TCP-overhead reverse proxy (e.g. /run/omni/gateway.sock)")
		callbackAddr = flag.String("oauth-callback-addr", envOrDefault("GATEWAY_OAUTH_CALLBACK_ADDR", "127.0.0.1:20128"), "Local OAuth callback listener address matching redirect_uri http://127.0.0.1:20128/callback")
		omniURL      = flag.String("omni-url", envOrDefault("OMNIROUTE_BASE_URL", ""), "Optional external OpenAI upstream URL (disabled by default)")
		omniKey      = flag.String("omni-key", envOrDefault("OMNIROUTE_API_KEY", ""), "Optional external OpenAI upstream API key (disabled by default)")
		ccpaURL      = flag.String("ccpa-url", envOrDefault("AGY_CCPA_ENDPOINT", gateway.DefaultCCPAEndpoint), "Direct Antigravity Cloud Code Private API endpoint")
		agyToken     = flag.String("agy-token", envOrDefault("AGY_OAUTH_TOKEN", saved.AgyOAuthToken), "Google AI Ultra OAuth token (ya29....) for direct daily-cloudcode-pa access")
		agyProject   = flag.String("agy-project", envOrDefault("AGY_PROJECT_ID", saved.AgyProjectID), "Optional Antigravity Cloud Code project ID")
		fallbacks    = flag.String("fallbacks", envFirstNonEmpty([]string{"GATEWAY_FALLBACK_MODELS", "OMNI_FALLBACK_MODELS"}, "antigravity/gemini-4-carbon-high,antigravity/gemini-4-argon-high,claude-code/claude-opus-4-6-thinking,alibaba/qwen3-coder-plus"), "Comma-separated fallback models")
	)
	flag.Parse()

	var fbList []string
	for _, item := range strings.Split(*fallbacks, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			fbList = append(fbList, trimmed)
		}
	}

	cfg := gateway.GatewayConfig{
		ListenAddr:       *listenAddr,
		OmniRouteBaseURL: strings.TrimSpace(*omniURL),
		OmniRouteAPIKey:  strings.TrimSpace(*omniKey),
		CCPAEndpoint:     *ccpaURL,
		AgyOAuthToken:    strings.TrimSpace(*agyToken),
		AgyProjectID:     strings.TrimSpace(*agyProject),
		FallbackModels:   fbList,
	}

	resolver := gateway.NewResolver()
	upstream := gateway.NewUpstream(cfg)
	gatewaySrv := gateway.NewServer(cfg, resolver, upstream)
	pdcaWatchdogCtx, stopPDCAWatchdog := context.WithCancel(context.Background())
	defer stopPDCAWatchdog()
	gatewaySrv.StartPDCAWatchdog(pdcaWatchdogCtx)
	handler := gatewaySrv.Handler()

	// Start automatic OAuth callback listener on 127.0.0.1:20128 in the background
	var cbServer *http.Server
	if *callbackAddr != "" && *callbackAddr != cfg.ListenAddr {
		cbServer = newGatewayHTTPServer(*callbackAddr, handler)
		go func() {
			if err := cbServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("[omni-gateway] OAuth callback listener on %s note: %v (paste URL manually in /ui if port is busy)", *callbackAddr, err)
			}
		}()
	}

	// Optional dual-listener Unix Domain Socket (e.g. /run/omni/gateway.sock -> /dev/shm/omni/gateway.sock)
	unixSocketPath := strings.TrimSpace(*unixSocket)
	var (
		unixLn  net.Listener
		unixSrv *http.Server
	)
	if unixSocketPath != "" {
		ln, err := listenUnixGatewaySocket(unixSocketPath)
		if err != nil {
			log.Printf("[omni-gateway] Unix socket listener warning on %s (continuing with TCP %s): %v", unixSocketPath, cfg.ListenAddr, err)
		} else {
			unixLn = ln
			unixSrv = newGatewayHTTPServer("", handler)
		}
	}

	omniStatus := "DISABLED (100% Standalone AI Gateway Mode)"
	if cfg.OmniRouteBaseURL != "" {
		omniStatus = cfg.OmniRouteBaseURL
	}

	fmt.Printf("======================================================================\n")
	fmt.Printf("  OmniRoute-Go v%s (3 Providers: Antigravity + Claude Code + Alibaba)\n", gateway.GatewayVersion)
	fmt.Printf("======================================================================\n")
	fmt.Printf("  Local OpenAI Base URL : http://%s/v1\n", cfg.ListenAddr)
	if unixLn != nil {
		fmt.Printf("  Unix Domain Socket    : unix:%s (0666)\n", unixSocketPath)
	}
	fmt.Printf("  Chat Completions URL  : http://%s/v1/chat/completions\n", cfg.ListenAddr)
	fmt.Printf("  Anthropic Native URL  : http://%s/v1/messages\n", cfg.ListenAddr)
	fmt.Printf("  Models & Combos URL   : http://%s/v1/models\n", cfg.ListenAddr)
	fmt.Printf("  Providers Info URL    : http://%s/v1/providers\n", cfg.ListenAddr)
	fmt.Printf("  Control Center UI     : http://%s/ui\n", cfg.ListenAddr)
	fmt.Printf("  OAuth Auto-Callback   : http://%s/callback\n", *callbackAddr)
	fmt.Printf("  Routing Strategy      : %s\n", saved.RoutingStrategy)
	fmt.Printf("  External Upstream     : %s\n", omniStatus)
	fmt.Printf("  Direct CCPA Endpoint  : %s\n", cfg.CCPAEndpoint)
	fmt.Printf("  Config File Path      : %s\n", gateway.GatewayConfigFilePath())
	pgStatus := gateway.GetPostgresStatus()
	if pgConnected, _ := pgStatus["connected"].(bool); pgConnected {
		fmt.Printf("  Database Backend      : PostgreSQL Connected (%v)\n", pgStatus["dsn_masked"])
	} else {
		fmt.Printf("  Database Backend      : Local JSON + RAM (Set DATABASE_URL for PostgreSQL)\n")
	}
	fmt.Printf("  Connected Accounts    : %d\n", len(saved.Accounts))
	fmt.Printf("  Supported Providers   : 1. Google Antigravity (Gemini 4 Carbon (High), Gemini 4 Argon, Gen 3 & Claude)\n")
	fmt.Printf("                          2. Anthropic Claude Code (OAuth PKCE / API Key)\n")
	fmt.Printf("                          3. Alibaba Token Plan (Bailian Coding Plan)\n")
	fmt.Printf("======================================================================\n")

	srv := newGatewayHTTPServer(cfg.ListenAddr, handler)

	if unixSrv != nil && unixLn != nil {
		go func() {
			if err := unixSrv.Serve(unixLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("[omni-gateway] Unix socket HTTP server (%s) exited with error: %v", unixSocketPath, err)
			}
		}()
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var fatalServerErr error
	select {
	case err := <-serverErrCh:
		fatalServerErr = err
		log.Printf("[omni-gateway] TCP server exited with error: %v", err)
	case sig := <-sigCh:
		log.Printf("[omni-gateway] received signal %v, shutting down gracefully...", sig)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var shutdownWG sync.WaitGroup
	if cbServer != nil {
		shutdownWG.Add(1)
		go func() {
			defer shutdownWG.Done()
			_ = cbServer.Shutdown(shutdownCtx)
		}()
	}
	if unixSrv != nil {
		shutdownWG.Add(1)
		go func() {
			defer shutdownWG.Done()
			if err := unixSrv.Shutdown(shutdownCtx); err != nil {
				log.Printf("[omni-gateway] Unix socket server shutdown error: %v", err)
			}
			_ = os.Remove(unixSocketPath)
		}()
	}
	shutdownWG.Add(1)
	go func() {
		defer shutdownWG.Done()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("[omni-gateway] HTTP server shutdown error: %v", err)
		}
	}()
	shutdownWG.Wait()

	if unixSocketPath != "" && unixLn != nil {
		_ = os.Remove(unixSocketPath)
	}

	gateway.StopAllTunnels()
	gateway.FlushPendingConfigSave()
	_ = gateway.ClosePostgres()
	if fatalServerErr != nil {
		os.Exit(1)
	}
	log.Printf("[omni-gateway] graceful shutdown complete.")
}

func newGatewayHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func listenUnixGatewaySocket(sockPath string) (net.Listener, error) {
	sockPath = strings.TrimSpace(sockPath)
	if sockPath == "" {
		return nil, errors.New("empty unix socket path")
	}
	if sockDir := filepath.Dir(sockPath); sockDir != "" && sockDir != "." {
		if err := os.MkdirAll(sockDir, 0777); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", sockDir, err)
		}
	}
	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}
	if chmodErr := os.Chmod(sockPath, 0666); chmodErr != nil {
		log.Printf("[omni-gateway] chmod 0666 note on %s: %v", sockPath, chmodErr)
	}
	return ln, nil
}

func envOrDefault(key, def string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return def
}

func envFirstNonEmpty(keys []string, def string) string {
	for _, k := range keys {
		if val := strings.TrimSpace(os.Getenv(k)); val != "" {
			return val
		}
	}
	return def
}
