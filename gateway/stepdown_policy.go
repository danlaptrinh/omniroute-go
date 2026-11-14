package gateway

import (
	"fmt"
	"strings"
	"time"
)

// buildResolvedChatTargets resolves multi-provider combo chains or preserves single model targets.
func buildResolvedChatTargets(resolved ResolvedModel) []ResolvedModel {
	if !resolved.IsCombo {
		return []ResolvedModel{resolved}
	}

	if len(resolved.FallbackChain) > 0 {
		var targets []ResolvedModel
		resolver := NewResolver()
		for _, fbModelID := range resolved.FallbackChain {
			cleanFb := strings.TrimSpace(fbModelID)
			if cleanFb == "" {
				continue
			}
			if strings.EqualFold(cleanFb, resolved.RequestedID) || strings.EqualFold(cleanFb, resolved.CanonicalID) {
				continue
			}
			fbResolved := resolver.Resolve(cleanFb, nil)
			fbResolved.RequestedID = resolved.RequestedID
			targets = append(targets, fbResolved)
		}
		if len(targets) > 0 {
			return targets
		}
	}

	return []ResolvedModel{resolved}
}

// strictPrimaryFlagshipTierForModel returns the highest flagship tier models without stepping down.
func strictPrimaryFlagshipTierForModel(endpoint string, isConnected bool, resolved ResolvedModel) []string {
	canon := strings.ToLower(strings.TrimSpace(resolved.CanonicalID))
	ccpaID := strings.ToLower(strings.TrimSpace(resolved.CCPAModelID))

	if resolved.ThinkingLevel == ThinkingLevelLow ||
		strings.Contains(canon, "lite") ||
		strings.HasSuffix(canon, "-low") ||
		ccpaID == "gemini-3-flash" ||
		strings.Contains(ccpaID, "image") {
		return nil
	}

	isDefaultProd := strings.EqualFold(strings.TrimSpace(endpoint), DefaultCCPAEndpoint)

	// Carbon Family
	if resolved.IsCarbonFamily || strings.HasPrefix(ccpaID, "gemini-4-carbon") {
		if resolved.ThinkingLevel == ThinkingLevelMedium {
			if isDefaultProd && isConnected {
				return []string{"gemini-4-carbon-medium", "gemini-4-argon-medium", "gemini-3.8-flash-medium", "gemini-3.8-flash-high"}
			}
			return []string{"gemini-4-carbon-medium", "gemini-4-carbon", "gemini-4-argon-medium", "gemini-3.8-flash-medium", "gemini-3.8-flash-high"}
		}
		if isDefaultProd && isConnected {
			return []string{"gemini-4-carbon-high", "gemini-4-argon-high", "gemini-3.8-flash-high"}
		}
		return []string{"gemini-4-carbon-high", "gemini-4-carbon", "gemini-4-argon-high", "gemini-4-argon", "gemini-3.8-flash-high"}
	}

	// Argon Family
	if resolved.IsArgonFamily || strings.HasPrefix(ccpaID, "gemini-4-argon") {
		if resolved.ThinkingLevel == ThinkingLevelMedium {
			if isDefaultProd && isConnected {
				return []string{"gemini-4-argon-medium", "gemini-4-carbon-medium", "gemini-3.8-flash-medium", "gemini-3.8-flash-high"}
			}
			return []string{"gemini-4-argon-medium", "gemini-4-argon", "gemini-4-carbon-medium", "gemini-3.8-flash-medium", "gemini-3.8-flash-high"}
		}
		if isDefaultProd && isConnected {
			return []string{"gemini-4-argon-high", "gemini-4-carbon-high", "gemini-3.8-flash-high"}
		}
		return []string{"gemini-4-argon-high", "gemini-4-argon", "gemini-4-carbon-high", "gemini-4-carbon", "gemini-3.8-flash-high"}
	}

	// Gemini 3.1 Pro
	if strings.HasPrefix(ccpaID, "gemini-3.1-pro") || strings.Contains(canon, "gemini-3.1-pro") {
		if isDefaultProd && isConnected {
			return []string{"gemini-3.8-flash-high"}
		}
		return []string{"gemini-3.1-pro-high", "gemini-3.1-pro-preview", "gemini-3.8-flash-high"}
	}

	// Gemini 3.8 Flash
	reqLower := strings.ToLower(strings.TrimSpace(resolved.RequestedID))
	if strings.HasPrefix(ccpaID, "gemini-3.8-flash") || strings.Contains(canon, "gemini-3.8-flash") {
		if strings.Contains(canon, "tiered") || strings.Contains(reqLower, "tiered") {
			return []string{"gemini-3.8-flash-tiered"}
		}
		if resolved.ThinkingLevel == ThinkingLevelMedium {
			return []string{"gemini-3.8-flash-medium"}
		}
		return []string{"gemini-3.8-flash-high"}
	}

	// Claude Opus
	if strings.HasPrefix(ccpaID, "claude-opus") || strings.Contains(canon, "claude-opus") {
		return []string{"claude-opus-4-6-thinking"}
	}

	// Claude Sonnet
	if strings.HasPrefix(ccpaID, "claude-sonnet") || strings.Contains(canon, "claude-sonnet") {
		return []string{"claude-sonnet-4-6"}
	}

	return nil
}

// isAllowedWorkingPinForCanonical validates whether a working model pin matches the requested canonical model tier.
func isAllowedWorkingPinForCanonical(canonicalID, pinnedModel string) bool {
	canon := sanitizePDCAModelToken(canonicalID)
	pin := sanitizePDCAModelToken(pinnedModel)
	if canon == "" || pin == "" {
		return false
	}

	if canon == "auto/fast" {
		return pin == "gemini-3-flash" || pin == "gemini-3.8-flash-low" || pin == "gemini-3.5-flash-lite"
	}

	// Carbon / Argon
	if strings.HasPrefix(canon, "gemini-4-carbon") || strings.HasPrefix(canon, "gemini-4-argon") {
		if strings.HasSuffix(canon, "-low") {
			return pin == "gemini-4-carbon-low" || pin == "gemini-4-argon-low" || pin == "gemini-3.8-flash-low"
		}
		if strings.HasSuffix(canon, "-medium") {
			return pin == "gemini-4-carbon-medium" || pin == "gemini-4-argon-medium" || pin == "gemini-4-carbon" || pin == "gemini-4-argon" || pin == "gemini-3.8-flash-medium" || pin == "gemini-3.8-flash-high"
		}
		return pin == "gemini-4-carbon-high" || pin == "gemini-4-argon-high" || pin == "gemini-4-carbon" || pin == "gemini-4-argon" || pin == "gemini-3.8-flash-high"
	}

	// Gemini 3.1 Pro
	if strings.HasPrefix(canon, "gemini-3.1-pro") {
		if strings.HasSuffix(canon, "-low") {
			return pin == "gemini-3.1-pro-low" || pin == "gemini-3.8-flash-low"
		}
		if strings.HasSuffix(canon, "-medium") {
			return pin == "gemini-3.1-pro-medium" || pin == "gemini-3.8-flash-medium"
		}
		return pin == "gemini-3.1-pro-high" || pin == "gemini-3.1-pro-preview" || pin == "gemini-3.8-flash-high"
	}

	// Gemini 3.8 Flash
	if strings.HasPrefix(canon, "gemini-3.8-flash") {
		if strings.Contains(canon, "tiered") {
			return pin == "gemini-3.8-flash-tiered"
		}
		if strings.HasSuffix(canon, "-low") {
			return pin == "gemini-3.8-flash-low"
		}
		if strings.HasSuffix(canon, "-medium") {
			return pin == "gemini-3.8-flash-medium"
		}
		return pin == "gemini-3.8-flash-high"
	}

	// Fixed single models
	if canon == "gemini-3-flash" {
		return pin == "gemini-3-flash"
	}
	if strings.Contains(canon, "3.1-flash-lite") {
		return pin == "gemini-3.1-flash-lite"
	}
	if strings.Contains(canon, "3.5-flash-lite") || strings.Contains(canon, "flash-lite") {
		return pin == "gemini-3.5-flash-lite"
	}

	// Claude
	if strings.HasPrefix(canon, "claude-opus") {
		return pin == "claude-opus-4-6-thinking"
	}
	if strings.HasPrefix(canon, "claude-sonnet") {
		return pin == "claude-sonnet-4-6"
	}

	return pin == canon
}

// buildStrictCCPACandidates constructs zero-stepdown CCPA candidate models for execution.
func buildStrictCCPACandidates(endpoint, accScope string, resolved ResolvedModel, noThinking bool) []string {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = DefaultCCPAEndpoint
	}
	isDefaultProdEndpoint := strings.EqualFold(strings.TrimSpace(endpoint), DefaultCCPAEndpoint)
	isAutoFastExplicit := strings.EqualFold(strings.TrimSpace(resolved.RequestedID), "auto/fast") || strings.EqualFold(strings.TrimSpace(resolved.CanonicalID), "auto/fast")
	scope := normalizeAccScope(accScope)

	// Striped combo mode single candidate bypass
	if resolved.ComboMode == ComboModeStriped && strings.TrimSpace(resolved.CCPAModelID) != "" && !isVirtualCCPAIDEAlias(resolved.CCPAModelID) {
		cand := strings.TrimSpace(resolved.CCPAModelID)
		now := time.Now()
		deadOnAcc := isAccountDeadCCPAModel(endpoint, scope, cand, now) || isDeadCCPAModel(endpoint, cand, now)
		rateMasked := strings.Contains(scope, "@") && IsAccountModelRateLimited(scope, cand)
		if deadOnAcc || rateMasked {
			return nil
		}
		return []string{cand}
	}

	_, isConnected := lookupConnectedAntigravityAccountFast(scope)
	claudeExhausted := isAccountClaudeQuotaExhaustedFast(scope)

	var raw []string
	cacheKey := endpoint + "|acc:" + scope + "|" + resolved.CanonicalID
	if !noThinking && !isAutoFastExplicit && resolved.CanonicalID != "" {
		if val, ok := workingCCPAModelCache.Load(cacheKey); ok {
			if s, okStr := val.(string); okStr {
				sTrim := strings.TrimSpace(s)
				if sTrim != "" {
					if !isAllowedWorkingPinForCanonical(resolved.CanonicalID, sTrim) {
						workingCCPAModelCache.Delete(cacheKey)
					} else {
						isRateMasked := strings.Contains(scope, "@") && IsAccountModelRateLimited(scope, sTrim)
						isClaudeBlocked := claudeExhausted && strings.HasPrefix(strings.ToLower(sTrim), "claude-")
						if !isRateMasked && !isClaudeBlocked {
							raw = append(raw, sTrim)
						}
					}
				}
			}
		}
	}

	canonLower := strings.ToLower(strings.TrimSpace(resolved.CanonicalID))
	ccpaLower := strings.ToLower(strings.TrimSpace(resolved.CCPAModelID))
	reqLower := strings.ToLower(strings.TrimSpace(resolved.RequestedID))

	if isAutoFastExplicit {
		raw = append(raw, "gemini-3-flash", "gemini-3.8-flash-low", "gemini-3.5-flash-lite")
	} else if resolved.IsCarbonFamily || strings.HasPrefix(ccpaLower, "gemini-4-carbon") || strings.Contains(canonLower, "gemini-4-carbon") {
		switch resolved.ThinkingLevel {
		case ThinkingLevelLow:
			raw = append(raw, "gemini-4-carbon-low", "gemini-3.8-flash-low")
		case ThinkingLevelMedium:
			raw = append(raw, "gemini-4-carbon-medium", "gemini-4-argon-medium", "gemini-4-carbon", "gemini-3.8-flash-medium", "gemini-3.8-flash-high")
		default:
			raw = append(raw, "gemini-4-carbon-high", "gemini-4-argon-high", "gemini-4-carbon", "gemini-3.8-flash-high")
		}
	} else if resolved.IsArgonFamily || strings.HasPrefix(ccpaLower, "gemini-4-argon") || strings.Contains(canonLower, "gemini-4-argon") {
		if !isDefaultProdEndpoint && resolved.CCPAModelID != "" {
			raw = append(raw, resolved.CCPAModelID)
		}
		switch resolved.ThinkingLevel {
		case ThinkingLevelLow:
			raw = append(raw, "gemini-4-argon-low", "gemini-3.8-flash-low")
		case ThinkingLevelMedium:
			raw = append(raw, "gemini-4-argon-medium", "gemini-4-carbon-medium", "gemini-4-argon", "gemini-3.8-flash-medium", "gemini-3.8-flash-high")
		default:
			raw = append(raw, "gemini-4-argon-high", "gemini-4-carbon-high", "gemini-4-argon", "gemini-3.8-flash-high")
		}
	} else if strings.HasPrefix(ccpaLower, "gemini-3.1-pro") || strings.Contains(canonLower, "gemini-3.1-pro") {
		if !isDefaultProdEndpoint && resolved.CCPAModelID != "" {
			raw = append(raw, resolved.CCPAModelID)
		}
		switch resolved.ThinkingLevel {
		case ThinkingLevelLow:
			raw = append(raw, "gemini-3.8-flash-low")
		case ThinkingLevelMedium:
			raw = append(raw, "gemini-3.8-flash-medium")
		default:
			raw = append(raw, "gemini-3.8-flash-high")
		}
	} else if strings.HasPrefix(ccpaLower, "gemini-3.8-flash") || strings.Contains(canonLower, "gemini-3.8-flash") {
		if strings.Contains(canonLower, "tiered") || strings.Contains(reqLower, "tiered") {
			raw = append(raw, "gemini-3.8-flash-tiered")
		} else {
			switch resolved.ThinkingLevel {
			case ThinkingLevelLow:
				raw = append(raw, "gemini-3.8-flash-low")
			case ThinkingLevelMedium:
				raw = append(raw, "gemini-3.8-flash-medium")
			default:
				raw = append(raw, "gemini-3.8-flash-high")
			}
		}
	} else if resolved.CCPAModelID == "gemini-3-flash" || canonLower == "antigravity/gemini-3-flash" || canonLower == "gemini-3-flash" {
		raw = append(raw, "gemini-3-flash")
	} else if strings.Contains(canonLower, "3.1-flash-lite") {
		raw = append(raw, "gemini-3.1-flash-lite")
	} else if strings.Contains(canonLower, "lite") || strings.Contains(canonLower, "3.5-flash-lite") {
		raw = append(raw, "gemini-3.5-flash-lite")
	} else if strings.Contains(canonLower, "image") {
		raw = append(raw, "gemini-3.1-flash-image")
	} else if strings.HasPrefix(ccpaLower, "claude-opus") || strings.Contains(canonLower, "claude-opus") {
		if isDefaultProdEndpoint && isConnected && (claudeExhausted || (strings.Contains(scope, "@") && IsAccountModelRateLimited(scope, "claude-opus-4-6-thinking"))) {
			return nil
		}
		if !isDefaultProdEndpoint && resolved.CCPAModelID != "" {
			raw = append(raw, resolved.CCPAModelID)
		}
		raw = append(raw, "claude-opus-4-6-thinking")
	} else if strings.HasPrefix(ccpaLower, "claude-sonnet") || strings.Contains(canonLower, "claude-sonnet") {
		if isDefaultProdEndpoint && isConnected && (claudeExhausted || (strings.Contains(scope, "@") && IsAccountModelRateLimited(scope, "claude-sonnet-4-6"))) {
			return nil
		}
		if !isDefaultProdEndpoint && resolved.CCPAModelID != "" {
			raw = append(raw, resolved.CCPAModelID)
		}
		raw = append(raw, "claude-sonnet-4-6")
	} else {
		if resolved.CCPAModelID != "" && !(isDefaultProdEndpoint && isVirtualCCPAIDEAlias(resolved.CCPAModelID)) {
			raw = append(raw, resolved.CCPAModelID)
		} else {
			raw = append(raw, "gemini-3.8-flash-high")
		}
	}

	// Deduplicate raw
	seen := make(map[string]bool, len(raw))
	var deduped []string
	for _, m := range raw {
		cleanM := strings.TrimSpace(m)
		if cleanM != "" && !seen[cleanM] {
			seen[cleanM] = true
			deduped = append(deduped, cleanM)
		}
	}

	if len(deduped) == 0 {
		return nil
	}

	// Filter out virtual CCPA IDE aliases on prod
	if isDefaultProdEndpoint {
		var realIDs []string
		for _, m := range deduped {
			if !isVirtualCCPAIDEAlias(m) {
				realIDs = append(realIDs, m)
			}
		}
		if len(realIDs) > 0 {
			deduped = realIDs
		}
	}

	// Filter out dead models
	now := time.Now()
	var alive []string
	for _, m := range deduped {
		if !isDeadCCPAModel(endpoint, m, now) && !isAccountDeadCCPAModel(endpoint, scope, m, now) {
			alive = append(alive, m)
		}
	}

	if len(alive) > 0 {
		return alive
	}
	return deduped
}

// sanitizeSteppedDownWorkingPins purges contaminated cache entries in workingCCPAModelCache.
func sanitizeSteppedDownWorkingPins(cycleID, nowRFC string) (int, []PDCAAnomaly, []PDCAAction) {
	purgedCount := 0
	var anomalies []PDCAAnomaly
	var actions []PDCAAction

	workingCCPAModelCache.Range(func(key, value any) bool {
		ks, okK := key.(string)
		vs, okV := value.(string)
		if !okK || !okV {
			return true
		}

		parts := strings.Split(ks, "|")
		if len(parts) < 3 {
			return true
		}

		accPart := parts[1]
		canonPart := parts[2]
		accEmail := strings.TrimPrefix(accPart, "acc:")

		if !isAllowedWorkingPinForCanonical(canonPart, vs) {
			workingCCPAModelCache.Delete(ks)
			purgedCount++

			detail := fmt.Sprintf("Purged contaminated working pin %q for canonical model %q on account %q", vs, canonPart, accEmail)
			anom := PDCAAnomaly{
				ID:        generatePDCAID("anom-pin"),
				CycleID:   cycleID,
				Timestamp: nowRFC,
				Type:      "model_stepdown_pin_contamination",
				Severity:  "warning",
				Provider:  ProviderAntigravity,
				Account:   accEmail,
				Model:     vs,
				Detail:    detail,
			}
			anomalies = append(anomalies, anom)

			act := PDCAAction{
				ID:        generatePDCAID("act-pin"),
				CycleID:   cycleID,
				Timestamp: nowRFC,
				Type:      "heal_model_stepdown_pin",
				Provider:  ProviderAntigravity,
				Account:   accEmail,
				Target:    ks,
				Detail:    detail,
			}
			actions = append(actions, act)
		}
		return true
	})

	return purgedCount, anomalies, actions
}

// detectProCarbonStepdownLeaks audits recent gateway logs for Pro account requests stepping down unexpectedly.
func detectProCarbonStepdownLeaks(recentLogs []GatewayLogEntry, proEmails map[string]bool, cycleID, nowRFC string) []PDCAAnomaly {
	var anomalies []PDCAAnomaly
	seenAccs := make(map[string]bool)

	for _, entry := range recentLogs {
		if entry.Provider != ProviderAntigravity || entry.IsError {
			continue
		}

		cleanAcc := strings.ToLower(strings.TrimSpace(entry.Account))
		if cleanAcc == "" || !proEmails[cleanAcc] {
			continue
		}

		if IsCarbonOrArgonHighOrMedium(entry.RequestedModel) {
			// Pro accounts requesting Carbon/Argon Flagship should not be routed to lower flash stepdowns
			up := strings.ToLower(strings.TrimSpace(entry.UpstreamModel))
			res := strings.ToLower(strings.TrimSpace(entry.ResolvedModel))
			isSteppedDown := strings.Contains(up, "flash") || strings.Contains(res, "flash")

			if isSteppedDown && !seenAccs[cleanAcc] {
				seenAccs[cleanAcc] = true
				detail := fmt.Sprintf("Pro account %s requested flagship %q but stepped down to upstream %q (resolved: %q)", cleanAcc, entry.RequestedModel, entry.UpstreamModel, entry.ResolvedModel)
				anomalies = append(anomalies, PDCAAnomaly{
					ID:        generatePDCAID("anom-pro-leak"),
					CycleID:   cycleID,
					Timestamp: nowRFC,
					Type:      "pro_carbon_stepdown_leak",
					Severity:  "warning",
					Provider:  ProviderAntigravity,
					Account:   cleanAcc,
					Model:     entry.RequestedModel,
					Detail:    detail,
				})
			}
		}
	}

	return anomalies
}
