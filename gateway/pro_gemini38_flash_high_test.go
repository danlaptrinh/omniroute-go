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

func TestProGemini38FlashHighFallsBackToTieredOn503(t *testing.T) {
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
	var flashHighCalls atomic.Int32
	var flashTieredCalls atomic.Int32

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
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":503,"message":"ultra touched"}}`)),
				}, nil
			}

			if strings.Contains(auth, "ya29.pro-advn") {
				if payload.Model == "gemini-3.8-flash-high" {
					flashHighCalls.Add(1)
					return &http.Response{
						StatusCode: http.StatusServiceUnavailable,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"error":{"code":503,"message":"flash high 503 congested"}}`)),
					}, nil
				}
				if payload.Model == "gemini-3.8-flash-tiered" {
					flashTieredCalls.Add(1)
					sse := "data: {\"response\":{\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pro-flash-tiered-fallback-ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":6,\"totalTokenCount\":18}}}\n\ndata: [DONE]\n\n"
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
			{Role: "user", Content: json.RawMessage(`"Test fallback to tiered on 503"`)},
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
	if flashHighCalls.Load() == 0 {
		t.Fatalf("expected flash-high to be attempted first on pro account")
	}
	if flashTieredCalls.Load() == 0 {
		t.Fatalf("expected flash-tiered fallback to be attempted on 503")
	}

	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "pro-flash-tiered-fallback-ok") {
		t.Fatalf("expected response to contain %q, got: %s", "pro-flash-tiered-fallback-ok", bodyStr)
	}
}
