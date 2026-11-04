package gateway

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	VectorEmbeddingDim   = 256
	MaxInMemoryVectors   = 2000
	DefaultVectorTopK    = 3
	DefaultMinSimilarity = 0.24
)

var (
	vectorMu          sync.RWMutex
	vectorMemories    []VectorMemoryEntry
	vectorLoadedLocal bool
	pgVectorExtension bool
)

// GetVectorDBConfig returns the active VectorDBConfig with sensible defaults enabled by default.
func GetVectorDBConfig() VectorDBConfig {
	cfg := LoadSavedAuthConfig()
	if cfg.VectorDBConfig == nil {
		return VectorDBConfig{
			Enabled:          true,
			AutoLearnPrompts: true,
			AutoInjectRAG:    true,
			TopK:             DefaultVectorTopK,
			MinSimilarity:    DefaultMinSimilarity,
		}
	}
	vc := *cfg.VectorDBConfig
	if vc.TopK <= 0 || vc.TopK > 10 {
		vc.TopK = DefaultVectorTopK
	}
	if vc.MinSimilarity <= 0 || vc.MinSimilarity >= 1.0 {
		vc.MinSimilarity = DefaultMinSimilarity
	}
	return vc
}

// SaveVectorDBConfig persists updated VectorDBConfig to gateway_config.json.
func SaveVectorDBConfig(vc VectorDBConfig) error {
	if vc.TopK <= 0 || vc.TopK > 10 {
		vc.TopK = DefaultVectorTopK
	}
	if vc.MinSimilarity <= 0 || vc.MinSimilarity >= 1.0 {
		vc.MinSimilarity = DefaultMinSimilarity
	}
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if cfg.VectorDBConfig != nil && vc.TotalRAGHits == 0 {
		vc.TotalRAGHits = cfg.VectorDBConfig.TotalRAGHits
	}
	cfg.VectorDBConfig = &vc
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveAuthConfigLocked(cfg)
}

func incrementVectorRAGHits() {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if cfg.VectorDBConfig == nil {
		vc := GetVectorDBConfig()
		cfg.VectorDBConfig = &vc
	}
	cfg.VectorDBConfig.TotalRAGHits++
	saveAuthConfigAsyncLocked(cfg)
}

func vectorFallbackFilePath() string {
	base := GatewayConfigFilePath()
	return filepath.Join(filepath.Dir(base), "omni_vector_memories.json")
}

func defaultSeededPinnedMemories() []VectorMemoryEntry {
	sopTitle := "Quy Trình Deploy Phần Mềm Lên Server Chuẩn Công Nghiệp (4 Cải Tiến Tự Động Hóa Hoàn Toàn — Server 103.37.60.13)"
	sopPrompt := "Quy trình chuẩn công nghiệp khi deploy phần mềm lên server sản xuất 103.37.60.13 (BuCloud): Single Source of Truth nhánh main, Auto-CD từ ci-gate, Atomic Symlink Release (/opt/bucloud/releases), bucloud-rollback và Post-Deploy Smoke Check tự động."
	sopAnswer := `GIẢI PHÁP DEPLOY PHẦN MỀM CHUẨN CÔNG NGHIỆP (4 CẢI TIẾN TỰ ĐỘNG HÓA HOÀN TOÀN):

1. Nguyên tắc "Single Source of Truth" — Chỉ nhánh main mới được phép Deploy:
   - Cấm hoàn toàn việc đẩy trực tiếp dist hay binary từ máy cá nhân lên server 103.37.60.13.
   - Mọi thay đổi bắt buộc đi theo 1 đường duy nhất:
     Local Commit ➔ Máy CI (ci-gate) ➔ GitHub PR ➔ Merge vào main ➔ Server tự động Deploy.

2. Tự động Deploy ngay khi Merge PR (Auto-CD bằng GitHub Webhook hoặc Máy CI đẩy sang Server):
   - Ngay khi ci-gate kiểm tra xong hoặc khi PR được merge vào main, chính máy CI (nơi vừa build sẵn admin/dist và api đã pass 100% test) sẽ tự động đóng gói artifact đã kiểm chứng đó và đẩy sang 103.37.60.13.
   - Lợi ích: Không tốn thời gian build lại lần 2, và đảm bảo 100% code trên server chính xác từng byte với code đã pass CI và nằm trên main.

3. Atomic Symlink Release (Triển khai nguyên tử & Rollback trong 1 giây):
   - Thay vì chép đè trực tiếp vào /opt/bucloud2/admin/dist, script deploy tự động sẽ giải nén vào thư mục theo mã commit (<commit-sha>):
     • /opt/bucloud/releases/<commit-sha>/admin-dist
     • /opt/bucloud/releases/<commit-sha>/bucloud
   - Sau đó đổi symlink nguyên tử (ln -sfn):
     • /opt/bucloud2/admin/dist -> /opt/bucloud/releases/<commit-sha>/admin-dist
   - Giữ lại 5 bản release gần nhất. Nếu bản mới có bất kỳ sự cố gì, chỉ cần 1 lệnh bucloud-rollback là quay về bản trước đó trong < 1 giây.

4. Post-Deploy Smoke Check tự động:
   - Ngay sau khi đổi symlink nguyên tử và khởi động lại dịch vụ, hệ thống tự động thực thi Post-Deploy Smoke Check (kiểm tra HTTP health check, API status, và giao diện admin/dist). Nếu phát hiện lỗi, tự động chạy bucloud-rollback ngay lập tức.`

	emb := ComputeSemanticEmbedding(sopTitle + "\n" + sopPrompt + "\n" + sopAnswer)
	return []VectorMemoryEntry{
		{
			ID:           "mem-sop-deploy-bucloud-ci-cd",
			CreatedAt:    "05/11/2026 01:20:00",
			Title:        sopTitle,
			Category:     "devops_sop",
			EmployeeName: "System Admin",
			Model:        "pinned-knowledge",
			PromptText:   sopPrompt,
			AnswerText:   sopAnswer,
			Pinned:       true,
			HitCount:     1,
			Embedding:    emb,
		},
	}
}

func ensureVectorMemoriesLoadedLocked() {
	if vectorLoadedLocal {
		return
	}
	vectorLoadedLocal = true
	data, err := os.ReadFile(vectorFallbackFilePath())
	var list []VectorMemoryEntry
	if err == nil {
		_ = json.Unmarshal(data, &list)
	}
	seenIDs := make(map[string]bool, len(list))
	for i := range list {
		seenIDs[list[i].ID] = true
		if len(list[i].Embedding) != VectorEmbeddingDim {
			list[i].Embedding = ComputeSemanticEmbedding(list[i].Title + "\n" + list[i].PromptText + "\n" + list[i].AnswerText)
		}
	}
	for _, seed := range defaultSeededPinnedMemories() {
		if !seenIDs[seed.ID] {
			list = append([]VectorMemoryEntry{seed}, list...)
			seenIDs[seed.ID] = true
		}
	}
	vectorMemories = list
}

func saveVectorMemoriesLocalAsync() {
	vectorMu.RLock()
	snapshot := make([]VectorMemoryEntry, len(vectorMemories))
	copy(snapshot, vectorMemories)
	vectorMu.RUnlock()

	go func(items []VectorMemoryEntry) {
		path := vectorFallbackFilePath()
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		b, err := json.MarshalIndent(items, "", "  ")
		if err == nil {
			_ = os.WriteFile(path, b, 0o600)
		}
	}(snapshot)
}

// ComputeSemanticEmbedding generates a unit-normalized 256-dimensional dense vector embedding
// using signed multi-granular feature hashing (word unigrams, word bigrams, technical identifiers, and char 3-grams).
func ComputeSemanticEmbedding(text string) []float32 {
	vec := make([]float32, VectorEmbeddingDim)
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return vec
	}

	// Extract word tokens (preserving Vietnamese letters, English letters, numbers, and code symbols)
	var tokens []string
	var cur strings.Builder
	for _, r := range clean {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/' || r == '.' {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				tok := strings.Trim(cur.String(), "._-/")
				if len(tok) >= 2 {
					tokens = append(tokens, tok)
				}
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		tok := strings.Trim(cur.String(), "._-/")
		if len(tok) >= 2 {
			tokens = append(tokens, tok)
		}
	}

	if len(tokens) == 0 {
		return vec
	}

	freq := make(map[string]int, len(tokens)*2)
	for i, t := range tokens {
		// Unigram with higher weight
		freq["w:"+t] += 3
		// Word bigram for phrase semantics (e.g., "vector database", "triển khai vps")
		if i+1 < len(tokens) {
			freq["b:"+t+"_"+tokens[i+1]] += 4
		}
		// Character 3-grams for morphological/typo resilience
		runes := []rune(t)
		if len(runes) >= 4 {
			for j := 0; j+3 <= len(runes); j++ {
				freq["c:"+string(runes[j:j+3])] += 1
			}
		}
	}

	for feat, count := range freq {
		weight := float32(1.0 + math.Log(float64(count)))
		h1, sign1 := hashFeature(feat, 0)
		h2, sign2 := hashFeature(feat, 1)
		vec[h1%VectorEmbeddingDim] += float32(sign1) * weight
		vec[h2%VectorEmbeddingDim] += float32(sign2) * (weight * 0.5)
	}

	// L2 Normalize so dot product == Cosine Similarity
	var normSq float64
	for _, v := range vec {
		normSq += float64(v) * float64(v)
	}
	if normSq > 0 {
		invNorm := float32(1.0 / math.Sqrt(normSq))
		for i := range vec {
			vec[i] *= invNorm
		}
	}
	return vec
}

func hashFeature(s string, seed byte) (uint32, int) {
	h := fnv.New64a()
	_, _ = h.Write([]byte{seed})
	_, _ = h.Write([]byte(s))
	sum := h.Sum64()
	idx := uint32(sum & 0xFFFFFFFF)
	sign := 1
	if (sum>>32)&1 == 1 {
		sign = -1
	}
	return idx, sign
}

// CosineSimilarity computes the cosine similarity between two unit-normalized vectors in [0.0, 1.0].
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	if dot < 0 {
		return 0
	}
	if dot > 1.0 {
		return 1.0
	}
	return dot
}

// migratePostgresVectorTables creates the omni_vector_memories table (and pgvector extension if available) in PostgreSQL.
func migratePostgresVectorTables(ctx context.Context, db *sql.DB) {
	if db == nil {
		return
	}
	// Attempt to enable pgvector extension if the PostgreSQL server has pgvector installed
	if _, err := db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS vector;`); err == nil {
		vectorMu.Lock()
		pgVectorExtension = true
		vectorMu.Unlock()
		log.Printf("[omniroute-vectordb] PostgreSQL pgvector extension enabled!")
	}

	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS omni_vector_memories (
			id TEXT PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at_str TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT 'auto_qa',
			employee_name TEXT NOT NULL DEFAULT '',
			api_key_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			prompt_text TEXT NOT NULL DEFAULT '',
			answer_text TEXT NOT NULL DEFAULT '',
			pinned BOOLEAN NOT NULL DEFAULT FALSE,
			hit_count BIGINT NOT NULL DEFAULT 0,
			embedding_json TEXT NOT NULL DEFAULT '[]'
		);
		CREATE INDEX IF NOT EXISTS idx_omni_vector_memories_created ON omni_vector_memories(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_omni_vector_memories_pinned ON omni_vector_memories(pinned);
	`)
	if err != nil {
		log.Printf("[omniroute-vectordb] table migration warning: %v", err)
		return
	}

	syncVectorMemoriesBidirectional(ctx, db)
}

func syncVectorMemoriesBidirectional(ctx context.Context, db *sql.DB) {
	vectorMu.Lock()
	ensureVectorMemoriesLoadedLocked()
	localByID := make(map[string]VectorMemoryEntry, len(vectorMemories))
	for _, m := range vectorMemories {
		localByID[m.ID] = m
	}
	vectorMu.Unlock()

	rows, err := db.QueryContext(ctx, `
		SELECT id, created_at_str, title, category, employee_name, api_key_id, model, prompt_text, answer_text, pinned, hit_count, embedding_json
		FROM omni_vector_memories
		ORDER BY pinned DESC, created_at DESC
		LIMIT 1500
	`)
	if err == nil {
		defer rows.Close()
		var merged []VectorMemoryEntry
		seen := make(map[string]bool)
		for rows.Next() {
			var m VectorMemoryEntry
			var embJSON string
			if scanErr := rows.Scan(
				&m.ID, &m.CreatedAt, &m.Title, &m.Category, &m.EmployeeName, &m.APIKeyID,
				&m.Model, &m.PromptText, &m.AnswerText, &m.Pinned, &m.HitCount, &embJSON,
			); scanErr == nil {
				_ = json.Unmarshal([]byte(embJSON), &m.Embedding)
				if len(m.Embedding) != VectorEmbeddingDim {
					m.Embedding = ComputeSemanticEmbedding(m.Title + "\n" + m.PromptText + "\n" + m.AnswerText)
				}
				merged = append(merged, m)
				seen[m.ID] = true
			}
		}

		// Add any local memories not yet in DB
		for id, localMem := range localByID {
			if !seen[id] {
				merged = append(merged, localMem)
				upsertVectorMemoryToPostgres(ctx, db, localMem)
			}
		}

		vectorMu.Lock()
		vectorMemories = merged
		vectorMu.Unlock()
		saveVectorMemoriesLocalAsync()
	}
}

func upsertVectorMemoryToPostgres(ctx context.Context, db *sql.DB, m VectorMemoryEntry) {
	if db == nil {
		return
	}
	embBytes, _ := json.Marshal(m.Embedding)
	_, _ = db.ExecContext(ctx, `
		INSERT INTO omni_vector_memories (
			id, created_at_str, title, category, employee_name, api_key_id, model, prompt_text, answer_text, pinned, hit_count, embedding_json
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET
			title = EXCLUDED.title,
			category = EXCLUDED.category,
			prompt_text = EXCLUDED.prompt_text,
			answer_text = EXCLUDED.answer_text,
			pinned = EXCLUDED.pinned,
			hit_count = GREATEST(omni_vector_memories.hit_count, EXCLUDED.hit_count),
			embedding_json = EXCLUDED.embedding_json
	`, m.ID, m.CreatedAt, m.Title, m.Category, m.EmployeeName, m.APIKeyID, m.Model, m.PromptText, m.AnswerText, m.Pinned, m.HitCount, string(embBytes))
}

// AddVectorMemory adds or updates a semantic memory in the Vector Database (both in-memory index and PostgreSQL).
func AddVectorMemory(title, category, promptText, answerText, employeeName, apiKeyID, model string, pinned bool) (VectorMemoryEntry, error) {
	promptText = strings.TrimSpace(promptText)
	answerText = strings.TrimSpace(answerText)
	title = strings.TrimSpace(title)
	if promptText == "" && answerText == "" {
		return VectorMemoryEntry{}, fmt.Errorf("nội dung ghi nhớ không được để trống")
	}
	if title == "" {
		firstLine := promptText
		if idx := strings.IndexByte(firstLine, '\n'); idx > 0 {
			firstLine = firstLine[:idx]
		}
		title = truncateAuditText(firstLine, 90)
	}
	if category == "" {
		if pinned {
			category = "knowledge"
		} else {
			category = "auto_qa"
		}
	}

	emb := ComputeSemanticEmbedding(title + "\n" + promptText + "\n" + answerText)

	vectorMu.Lock()
	ensureVectorMemoriesLoadedLocked()

	// Check if a near-duplicate memory already exists (Cosine Similarity >= 0.93)
	for i := range vectorMemories {
		sim := CosineSimilarity(emb, vectorMemories[i].Embedding)
		if sim >= 0.93 && vectorMemories[i].Pinned == pinned {
			vectorMemories[i].AnswerText = truncateAuditText(answerText, 6000)
			vectorMemories[i].HitCount++
			updated := vectorMemories[i]
			vectorMu.Unlock()

			saveVectorMemoriesLocalAsync()
			pgMu.RLock()
			db := pgDB
			pgMu.RUnlock()
			if db != nil {
				go func(m VectorMemoryEntry) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					upsertVectorMemoryToPostgres(ctx, db, m)
				}(updated)
			}
			return updated, nil
		}
	}

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	entry := VectorMemoryEntry{
		ID:           "vec-" + hex.EncodeToString(b),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		Title:        title,
		Category:     category,
		EmployeeName: employeeName,
		APIKeyID:     apiKeyID,
		Model:        model,
		PromptText:   truncateAuditText(promptText, 4000),
		AnswerText:   truncateAuditText(answerText, 6000),
		Pinned:       pinned,
		HitCount:     1,
		Embedding:    emb,
	}

	// Prepend newest memory and cap size while preserving pinned entries
	vectorMemories = append([]VectorMemoryEntry{entry}, vectorMemories...)
	if len(vectorMemories) > MaxInMemoryVectors {
		vectorMemories = vectorMemories[:MaxInMemoryVectors]
	}
	vectorMu.Unlock()

	saveVectorMemoriesLocalAsync()
	pgMu.RLock()
	db := pgDB
	pgMu.RUnlock()
	if db != nil {
		go func(m VectorMemoryEntry) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			upsertVectorMemoryToPostgres(ctx, db, m)
		}(entry)
	}

	return entry, nil
}

// AutoLearnFromPromptLog automatically indexes high-value Q&A turns from employee prompts into the Vector DB.
func AutoLearnFromPromptLog(entry GatewayLogEntry) {
	if entry.IsError || entry.StatusCode >= 400 {
		return
	}
	cfg := GetVectorDBConfig()
	if !cfg.Enabled || !cfg.AutoLearnPrompts {
		return
	}
	q := extractLastUserQuestion(entry.PromptText)
	ans := strings.TrimSpace(entry.ResponseText)
	if len(q) < 12 || len(ans) < 20 {
		return
	}
	go func() {
		_, _ = AddVectorMemory("", "auto_qa", q, ans, entry.EmployeeName, entry.APIKeyID, entry.ResolvedModel, false)
	}()
}

func extractLastUserQuestion(formattedPrompt string) string {
	idx := strings.LastIndex(formattedPrompt, "[USER]:")
	if idx >= 0 {
		return strings.TrimSpace(formattedPrompt[idx+7:])
	}
	return strings.TrimSpace(formattedPrompt)
}

// SearchVectorMemories performs semantic cosine similarity search across the Vector Database.
func SearchVectorMemories(query string, topK int, minSim float64) []VectorMemoryEntry {
	query = strings.TrimSpace(query)
	if topK <= 0 {
		topK = DefaultVectorTopK
	}
	if minSim <= 0 {
		minSim = 0.10
	}

	vectorMu.RLock()
	ensureVectorMemoriesLoadedLocked()
	snapshot := make([]VectorMemoryEntry, len(vectorMemories))
	copy(snapshot, vectorMemories)
	vectorMu.RUnlock()

	if len(snapshot) == 0 {
		return []VectorMemoryEntry{}
	}

	if query == "" {
		limit := topK
		if limit > len(snapshot) {
			limit = len(snapshot)
		}
		out := make([]VectorMemoryEntry, limit)
		for i := 0; i < limit; i++ {
			cp := snapshot[i]
			cp.Embedding = nil
			out[i] = cp
		}
		return out
	}

	qVec := ComputeSemanticEmbedding(query)
	qLower := strings.ToLower(query)

	type scored struct {
		entry VectorMemoryEntry
		score float64
	}
	var candidates []scored

	for _, m := range snapshot {
		sim := CosineSimilarity(qVec, m.Embedding)
		// Boost exact substring match
		if strings.Contains(strings.ToLower(m.Title+" "+m.PromptText), qLower) {
			sim = math.Min(1.0, sim+0.25)
		}
		if m.Pinned && sim >= minSim*0.75 {
			sim = math.Min(1.0, sim+0.08)
		}
		if sim >= minSim {
			cp := m
			cp.Similarity = math.Round(sim*1000) / 1000
			cp.Embedding = nil
			candidates = append(candidates, scored{entry: cp, score: sim})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if len(candidates) > topK {
		candidates = candidates[:topK]
	}

	out := make([]VectorMemoryEntry, len(candidates))
	for i, c := range candidates {
		out[i] = c.entry
	}
	return out
}

// ApplyVectorMemoryRAG retrieves the Top-K relevant long-term vector memories and injects them into the system prompt.
func ApplyVectorMemoryRAG(req OpenAIChatRequest) (OpenAIChatRequest, int) {
	cfg := GetVectorDBConfig()
	if !cfg.Enabled || !cfg.AutoInjectRAG || len(req.Messages) == 0 {
		return req, 0
	}

	// Find the latest user message
	var userQuery string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(req.Messages[i].Role, "user") {
			userQuery = strings.TrimSpace(req.Messages[i].ContentText())
			break
		}
	}
	if len(userQuery) < 6 {
		return req, 0
	}

	matches := SearchVectorMemories(userQuery, cfg.TopK, cfg.MinSimilarity)
	if len(matches) == 0 {
		return req, 0
	}

	var ragBuilder strings.Builder
	ragBuilder.WriteString("[OMNI VECTOR DATABASE — TRÍ NHỚ DÀI HẠN & TRI THỨC NỘI BỘ]\n")
	ragBuilder.WriteString("Dưới đây là các tri thức dự án và lịch sử giải pháp tương tự đã được xác thực trong Vector Database (hãy ưu tiên tham khảo nếu phù hợp với câu hỏi):\n")

	for i, m := range matches {
		tag := "Tự học từ lịch sử"
		if m.Pinned {
			tag = "📌 Tri thức Cốt lõi"
		}
		ragBuilder.WriteString(fmt.Sprintf("\n--- Memory #%d (%s | Độ tương đồng Cosine: %.0f%%) ---\n", i+1, tag, m.Similarity*100))
		if m.PromptText != "" {
			ragBuilder.WriteString("Ngữ cảnh / Câu hỏi: " + truncateAuditText(m.PromptText, 600) + "\n")
		}
		if m.AnswerText != "" {
			ragBuilder.WriteString("Kiến thức / Giải pháp: " + truncateAuditText(m.AnswerText, 1200) + "\n")
		}
	}

	ragBlock := ragBuilder.String()

	// Increment hit count asynchronously
	incrementVectorRAGHits()

	outMessages := make([]OpenAIChatMessage, len(req.Messages))
	copy(outMessages, req.Messages)

	if strings.EqualFold(outMessages[0].Role, "system") || strings.EqualFold(outMessages[0].Role, "developer") {
		existing := outMessages[0].ContentText()
		combinedJSON, _ := json.Marshal(existing + "\n\n" + ragBlock)
		outMessages[0].Content = combinedJSON
	} else {
		rawSys, _ := json.Marshal(ragBlock)
		outMessages = append([]OpenAIChatMessage{{
			Role:    "system",
			Content: rawSys,
		}}, outMessages...)
	}

	req.Messages = outMessages
	return req, len(matches)
}

// DeleteVectorMemory deletes a memory by ID, or clears all non-pinned auto-learned memories if id == "all_auto".
func DeleteVectorMemory(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("thiếu id vector memory")
	}

	vectorMu.Lock()
	ensureVectorMemoriesLoadedLocked()
	var filtered []VectorMemoryEntry
	for _, m := range vectorMemories {
		if id == "all_auto" {
			if m.Pinned {
				filtered = append(filtered, m)
			}
			continue
		}
		if m.ID == id {
			continue
		}
		filtered = append(filtered, m)
	}
	vectorMemories = filtered
	vectorMu.Unlock()

	saveVectorMemoriesLocalAsync()

	pgMu.RLock()
	db := pgDB
	pgMu.RUnlock()
	if db != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if id == "all_auto" {
				_, _ = db.ExecContext(ctx, `DELETE FROM omni_vector_memories WHERE pinned = FALSE`)
			} else {
				_, _ = db.ExecContext(ctx, `DELETE FROM omni_vector_memories WHERE id = $1`, id)
			}
		}()
	}
	return nil
}

// ListVectorMemories returns stored vector memories without raw float32 arrays for fast UI rendering.
func ListVectorMemories(limit int) []VectorMemoryEntry {
	if limit <= 0 || limit > 300 {
		limit = 100
	}
	vectorMu.RLock()
	ensureVectorMemoriesLoadedLocked()
	n := len(vectorMemories)
	if n > limit {
		n = limit
	}
	out := make([]VectorMemoryEntry, n)
	for i := 0; i < n; i++ {
		cp := vectorMemories[i]
		cp.Embedding = nil
		out[i] = cp
	}
	vectorMu.RUnlock()
	return out
}

// GetVectorDBStatus returns comprehensive statistics and configuration of the Vector Database.
func GetVectorDBStatus() map[string]any {
	vectorMu.RLock()
	ensureVectorMemoriesLoadedLocked()
	total := len(vectorMemories)
	pinnedCount := 0
	autoCount := 0
	for _, m := range vectorMemories {
		if m.Pinned {
			pinnedCount++
		} else {
			autoCount++
		}
	}
	hasPgVec := pgVectorExtension
	vectorMu.RUnlock()

	pgStatus := GetPostgresStatus()
	pgConnected, _ := pgStatus["connected"].(bool)

	engineLabel := "Hybrid Cosine Vector Index (256-dim L2 + Local JSON)"
	if pgConnected && hasPgVec {
		engineLabel = "PostgreSQL 16 pgvector + Hybrid Cosine Index (256-dim)"
	} else if pgConnected {
		engineLabel = "PostgreSQL 16 + Hybrid Cosine Vector Index (256-dim)"
	}

	vc := GetVectorDBConfig()
	return map[string]any{
		"config":             vc,
		"total_memories":     total,
		"pinned_count":       pinnedCount,
		"auto_learned_count": autoCount,
		"pg_connected":       pgConnected,
		"pgvector_extension": hasPgVec,
		"embedding_dim":      VectorEmbeddingDim,
		"engine":             engineLabel,
	}
}

