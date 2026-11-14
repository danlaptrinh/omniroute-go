package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProDirectGemini38FlashHighNoTieredSubstitution(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	exp := time.Now().Add(2 * time.Hour).Unix()
	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				ID:          "u-1",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				Email:       "ntky152022@gmail.com",
				AccessToken: "ya29.ultra-ntky",
				ProjectID:   "aicode-consumers",
				ExpiresAt:   exp,
			},
			{
				ID:          "p-1",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "advietnam789@gmail.com",
				AccessToken: "ya29.pro-advn",
				ProjectID:   "aicode-consumers",
				ExpiresAt:   exp,
			},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth failed: %v", err)
	}

	var ultraCalls atomic.Int32
	var proCalls atomic.Int32

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
	upstream.client = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			var payload ccpaRequestPayload
			_ = json.Unmarshal(raw, &payload)
			auth := strings.TrimSpace(r.Header.Get("Authorization"))

			if strings.Contains(auth, "ya29.ultra-ntky") {
				ultraCalls.Add(1)
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":503,"message":"ultra should not be touched"}}`)),
				}, nil
			}

			if strings.Contains(auth, "ya29.pro-advn") {
				proCalls.Add(1)
				if payload.Model == "gemini-3.8-flash-high" {
					sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pro-flash-high-success\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":5,\"totalTokenCount\":15}}}\n\ndata: [DONE]\n\n"
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
						Body:       io.NopCloser(strings.NewReader(sse)),
					}, nil
				}
			}

			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":404,"message":"not found"}}`)),
			}, nil
		}),
	}

	resolved := ResolveModel("antigravity/gemini-3.8-flash-high", nil)
	rec := httptest.NewRecorder()
	req := OpenAIChatRequest{
		Model: "antigravity/gemini-3.8-flash-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Test direct gemini-3.8-flash-high on pro"`)},
		},
	}

	if err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, ""); err != nil {
		t.Fatalf("HandleChatCompletion failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	if ultraCalls.Load() != 0 {
		t.Fatalf("expected Ultra account to receive 0 requests, got %d", ultraCalls.Load())
	}
	if proCalls.Load() == 0 {
		t.Fatalf("expected Pro account to receive requests, got 0")
	}

	winningAcc := rec.Header().Get("X-Omni-Gateway-Account")
	if winningAcc != "advietnam789@gmail.com" {
		t.Fatalf("expected winning account advietnam789@gmail.com, got %q", winningAcc)
	}

	saved := LoadSavedAuthConfig()
	var foundPro bool
	for _, a := range saved.Accounts {
		if a.Email == "advietnam789@gmail.com" {
			foundPro = true
			wantLastModel := "antigravity/gemini-3.8-flash-high [gemini-3.8-flash]"
			if a.LastModel != wantLastModel {
				t.Fatalf("expected LastModel %q, got %q", wantLastModel, a.LastModel)
			}
		}
	}
	if !foundPro {
		t.Fatalf("Pro account advietnam789@gmail.com not found in saved config")
	}

	report := RunPDCACycle(time.Now().UTC())
	leakCount := 0
	for _, anom := range report.Anomalies {
		if anom.Type == "ultra_tier_routing_leak" {
			leakCount++
		}
	}
	if leakCount != 0 {
		t.Fatalf("expected 0 UltraTierLeakCount anomalies, got %d", leakCount)
	}
}

func TestProGemini31ProDiscontinuedUsesGemini38FlashHigh(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	exp := time.Now().Add(2 * time.Hour).Unix()
	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyRoundRobin,
		Accounts: []ConnectedAccount{
			{
				ID:          "u-1",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				Email:       "ntky152022@gmail.com",
				AccessToken: "ya29.ultra-ntky",
				ProjectID:   "aicode-consumers",
				ExpiresAt:   exp,
			},
			{
				ID:          "p-1",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "advietnam789@gmail.com",
				AccessToken: "ya29.pro-advn",
				ProjectID:   "aicode-consumers",
				ExpiresAt:   exp,
			},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth failed: %v", err)
	}

	var ultraCalls atomic.Int32
	var proCalls atomic.Int32
	var requestedUpstreamModel string

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
	upstream.client = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			var payload ccpaRequestPayload
			_ = json.Unmarshal(raw, &payload)
			auth := strings.TrimSpace(r.Header.Get("Authorization"))

			if strings.Contains(auth, "ya29.ultra-ntky") {
				ultraCalls.Add(1)
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":503,"message":"ultra should not be touched"}}`)),
				}, nil
			}

			if strings.Contains(auth, "ya29.pro-advn") {
				proCalls.Add(1)
				requestedUpstreamModel = payload.Model
				if payload.Model == "gemini-3.8-flash-high" {
					sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"gemini-31-pro-aliased-to-flash-high\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":14,\"candidatesTokenCount\":7,\"totalTokenCount\":21}}}\n\ndata: [DONE]\n\n"
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
						Body:       io.NopCloser(strings.NewReader(sse)),
					}, nil
				}
			}

			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":404,"message":"not found"}}`)),
			}, nil
		}),
	}

	resolved := ResolveModel("antigravity/gemini-3.1-pro-high", nil)
	rec := httptest.NewRecorder()
	req := OpenAIChatRequest{
		Model: "antigravity/gemini-3.1-pro-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Test discontinued gemini-3.1-pro on pro account"`)},
		},
	}

	if err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, ""); err != nil {
		t.Fatalf("HandleChatCompletion failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	if ultraCalls.Load() != 0 {
		t.Fatalf("expected Ultra account to receive 0 requests, got %d", ultraCalls.Load())
	}
	if proCalls.Load() == 0 {
		t.Fatalf("expected Pro account to receive requests, got 0")
	}

	if requestedUpstreamModel != "gemini-3.8-flash-high" {
		t.Fatalf("expected upstream model to be gemini-3.8-flash-high, got %q", requestedUpstreamModel)
	}

	winningAcc := rec.Header().Get("X-Omni-Gateway-Account")
	if winningAcc != "advietnam789@gmail.com" {
		t.Fatalf("expected winning account advietnam789@gmail.com, got %q", winningAcc)
	}
}

func TestProGemini38FlashHighNeverStepsDownToTieredAndFailsOverToProPeer(t *testing.T) {
	setupUltraFlagshipPDCATestEnv(t)

	exp := time.Now().Add(2 * time.Hour).Unix()
	if err := saveTestGatewayAuth(SavedGatewayAuth{
		RoutingStrategy: StrategyFillFirst,
		Accounts: []ConnectedAccount{
			{
				ID:          "u-1",
				Provider:    ProviderAntigravity,
				Tier:        "Ultra",
				Email:       "ntky152022@gmail.com",
				AccessToken: "ya29.ultra-ntky",
				ProjectID:   "aicode-consumers",
				Priority:    110,
				ExpiresAt:   exp,
			},
			{
				ID:          "p-1",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "advietnam789@gmail.com",
				AccessToken: "ya29.pro-advn",
				ProjectID:   "aicode-consumers",
				Priority:    100,
				ExpiresAt:   exp,
			},
			{
				ID:          "p-2",
				Provider:    ProviderAntigravity,
				Tier:        "Pro",
				Email:       "pro2@gmail.com",
				AccessToken: "ya29.pro-2",
				ProjectID:   "aicode-consumers",
				Priority:    90,
				ExpiresAt:   exp,
			},
		},
	}); err != nil {
		t.Fatalf("saveTestGatewayAuth failed: %v", err)
	}

	var ultraCalls atomic.Int32
	var flashTieredCalls atomic.Int32
	var pro1FlashHighCalls atomic.Int32
	var pro2FlashHighCalls atomic.Int32

	upstream := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
	upstream.client = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			var payload ccpaRequestPayload
			_ = json.Unmarshal(raw, &payload)
			auth := strings.TrimSpace(r.Header.Get("Authorization"))

			if payload.Model == "gemini-3.8-flash-tiered" || payload.Model == "gemini-3-flash" {
				flashTieredCalls.Add(1)
			}

			if strings.Contains(auth, "ya29.ultra-ntky") {
				ultraCalls.Add(1)
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":503,"message":"ultra should not be touched"}}`)),
				}, nil
			}

			if strings.Contains(auth, "ya29.pro-advn") {
				if payload.Model == "gemini-3.8-flash-high" {
					pro1FlashHighCalls.Add(1)
					return &http.Response{
						StatusCode: http.StatusServiceUnavailable,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"error":{"code":503,"message":"pro1 503 congested"}}`)),
					}, nil
				}
			}

			if strings.Contains(auth, "ya29.pro-2") {
				if payload.Model == "gemini-3.8-flash-high" {
					pro2FlashHighCalls.Add(1)
					sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pro-peer-flash-high-ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":6,\"totalTokenCount\":18}}}\n\ndata: [DONE]\n\n"
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
						Body:       io.NopCloser(strings.NewReader(sse)),
					}, nil
				}
			}

			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":404,"message":"not found"}}`)),
			}, nil
		}),
	}

	resolved := ResolveModel("antigravity/gemini-3.8-flash-high", nil)
	rec := httptest.NewRecorder()
	req := OpenAIChatRequest{
		Model: "antigravity/gemini-3.8-flash-high",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: json.RawMessage(`"Test pro peer failover without tiered stepdown"`)},
		},
	}

	if err := upstream.HandleChatCompletion(context.Background(), rec, req, resolved, ""); err != nil {
		t.Fatalf("HandleChatCompletion failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	if ultraCalls.Load() != 0 {
		t.Fatalf("expected Ultra account to receive 0 requests, got %d", ultraCalls.Load())
	}
	if flashTieredCalls.Load() != 0 {
		t.Fatalf("expected 0 flash-tiered / flash calls (Zero Stepdown Policy), got %d", flashTieredCalls.Load())
	}
	if pro1FlashHighCalls.Load() == 0 {
		t.Fatalf("expected pro1 (advietnam789) to be attempted first")
	}
	if pro2FlashHighCalls.Load() == 0 {
		t.Fatalf("expected pro2 to be attempted on pro1 503")
	}

	winningAcc := rec.Header().Get("X-Omni-Gateway-Account")
	if winningAcc != "pro2@gmail.com" {
		t.Fatalf("expected winning account pro2@gmail.com, got %q", winningAcc)
	}

	resolvedModelHeader := rec.Header().Get("X-Omni-Gateway-Resolved-Model")
	if resolvedModelHeader != "gemini-3.8-flash-high" && resolvedModelHeader != "antigravity/gemini-3.8-flash-high" {
		t.Fatalf("expected resolved model header to be gemini-3.8-flash-high, got %q", resolvedModelHeader)
	}

	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "pro-peer-flash-high-ok") {
		t.Fatalf("expected response body to contain %q, got: %s", "pro-peer-flash-high-ok", bodyStr)
	}

	// Secondary check: When ALL Pro accounts fail with 503 on gemini-3.8-flash-high,
	// the gateway must fail cleanly rather than stepping down to gemini-3.8-flash-tiered or spilling onto Ultra.
	ultraCalls.Store(0)
	flashTieredCalls.Store(0)

	upstreamAll503 := NewUpstream(GatewayConfig{CCPAEndpoint: DefaultCCPAEndpoint})
	upstreamAll503.client = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			var payload ccpaRequestPayload
			_ = json.Unmarshal(raw, &payload)
			auth := strings.TrimSpace(r.Header.Get("Authorization"))

			if payload.Model == "gemini-3.8-flash-tiered" || payload.Model == "gemini-3-flash" {
				flashTieredCalls.Add(1)
			}

			if strings.Contains(auth, "ya29.ultra-ntky") {
				ultraCalls.Add(1)
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":503,"message":"ultra touched"}}`)),
				}, nil
			}

			if strings.Contains(auth, "ya29.pro-advn") || strings.Contains(auth, "ya29.pro-2") {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":503,"message":"all pros congested"}}`)),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":404,"message":"not found"}}`)),
			}, nil
		}),
	}

	recAll503 := httptest.NewRecorder()
	errAll503 := upstreamAll503.HandleChatCompletion(context.Background(), recAll503, req, resolved, "")
	if errAll503 == nil && recAll503.Code == http.StatusOK {
		t.Fatalf("expected error or non-200 status when all pros return 503, got status 200 with body: %s", recAll503.Body.String())
	}

	if ultraCalls.Load() != 0 {
		t.Fatalf("expected Ultra account to receive 0 requests even when all pros 503, got %d", ultraCalls.Load())
	}
	if flashTieredCalls.Load() != 0 {
		t.Fatalf("expected 0 calls to gemini-3.8-flash-tiered under Zero Stepdown Policy, got %d", flashTieredCalls.Load())
	}
}
