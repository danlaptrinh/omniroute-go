package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	handler := gatewaySrv.Handler()

	// Start automatic OAuth callback listener on 127.0.0.1:20128 in the background
	var cbServer *http.Server
	if *callbackAddr != "" && *callbackAddr != cfg.ListenAddr {
		cbServer = &http.Server{
			Addr:              *callbackAddr,
			Handler:           handler,
			ReadHeaderTimeout: 15 * time.Second,
		}
		go func() {
			if err := cbServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("[omni-gateway] OAuth callback listener on %s note: %v (paste URL manually in /ui if port is busy)", *callbackAddr, err)
			}
		}()
	}

	omniStatus := "DISABLED (100% Standalone AI Gateway Mode)"
	if cfg.OmniRouteBaseURL != "" {
		omniStatus = cfg.OmniRouteBaseURL
	}

	fmt.Printf("======================================================================\n")
	fmt.Printf("  OmniRoute-Go v%s (3 Providers: Antigravity + Claude Code + Alibaba)\n", gateway.GatewayVersion)
	fmt.Printf("======================================================================\n")
	fmt.Printf("  Local OpenAI Base URL : http://%s/v1\n", cfg.ListenAddr)
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

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
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

	select {
	case err := <-serverErrCh:
		log.Fatalf("[omni-gateway] server exited with error: %v", err)
	case sig := <-sigCh:
		log.Printf("[omni-gateway] received signal %v, shutting down gracefully...", sig)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if cbServer != nil {
		_ = cbServer.Shutdown(shutdownCtx)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[omni-gateway] HTTP server shutdown error: %v", err)
	}

	gateway.StopAllTunnels()
	gateway.FlushPendingConfigSave()
	_ = gateway.ClosePostgres()
	log.Printf("[omni-gateway] graceful shutdown complete.")
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
