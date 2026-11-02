package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"omni-parallel-mcp/gateway"
)

func main() {
	saved := gateway.LoadSavedAuthConfig()
	if err := gateway.InitPostgresDB(""); err != nil {
		log.Printf("[omni-gateway] PostgreSQL connection warning (falling back to Local JSON mode): %v", err)
	}

	var (
		listenAddr   = flag.String("listen", envOrDefault("GATEWAY_LISTEN_ADDR", "127.0.0.1:8787"), "Local HTTP listen address for the Standalone AI Gateway")
		callbackAddr = flag.String("oauth-callback-addr", envOrDefault("GATEWAY_OAUTH_CALLBACK_ADDR", "127.0.0.1:20128"), "Local OAuth callback listener address matching redirect_uri http://127.0.0.1:20128/callback")
		omniURL      = flag.String("omni-url", envOrDefault("OMNIROUTE_BASE_URL", ""), "Optional external OpenAI upstream URL (disabled by default)")
		omniKey      = flag.String("omni-key", envOrDefault("OMNIROUTE_API_KEY", ""), "Optional external OpenAI upstream API key (disabled by default)")
		ccpaURL      = flag.String("ccpa-url", envOrDefault("AGY_CCPA_ENDPOINT", gateway.DefaultCCPAEndpoint), "Direct Antigravity Cloud Code Private API endpoint")
		agyToken     = flag.String("agy-token", envOrDefault("AGY_OAUTH_TOKEN", saved.AgyOAuthToken), "Google AI Ultra OAuth token (ya29....) for direct daily-cloudcode-pa access")
		agyProject   = flag.String("agy-project", envOrDefault("AGY_PROJECT_ID", saved.AgyProjectID), "Optional Antigravity Cloud Code project ID")
		fallbacks    = flag.String("fallbacks", envOrDefault("GATEWAY_FALLBACK_MODELS", "gemini-4-argon,gemini-3.1-pro,claude-opus-4-6-thinking"), "Comma-separated fallback models")
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
	srv := gateway.NewServer(cfg, resolver, upstream)
	handler := srv.Handler()

	// Start automatic OAuth callback listener on 127.0.0.1:20128 in the background
	if *callbackAddr != "" && *callbackAddr != cfg.ListenAddr {
		go func() {
			cbServer := &http.Server{
				Addr:              *callbackAddr,
				Handler:           handler,
				ReadHeaderTimeout: 15 * time.Second,
			}
			if err := cbServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
	fmt.Printf("  Supported Providers   : 1. Google Antigravity (Gen 3, Gen 4 & Claude)\n")
	fmt.Printf("                          2. Anthropic Claude Code (OAuth PKCE / API Key)\n")
	fmt.Printf("                          3. Alibaba Token Plan (Bailian Coding Plan)\n")
	fmt.Printf("======================================================================\n")

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
	}

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[omni-gateway] server exited with error: %v", err)
	}
}

func envOrDefault(key, def string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return def
}
