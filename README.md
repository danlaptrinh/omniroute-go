# OmniRoute-Go — AI Routing Engine & Parallel MCP Dispatcher

Hệ thống **AI Gateway đa nhà cung cấp (Multi-Provider Routing Engine)** kết hợp **Parallel Goroutine MCP Server** viết bằng Go hiệu năng cao, hỗ trợ triển khai độc lập trên Local hoặc VPS qua Docker Compose.

---

## 🌟 Tính năng nổi bật

1. **Định tuyến 3 Nhà cung cấp AI lớn**:
   - **Google Antigravity (Direct CCPA HTTP/2)**: **Gemini 4 Carbon High** (`antigravity/gemini-4-carbon-high`, `medium`, `low`), **Gemini 4 Argon** (`antigravity/gemini-4-argon-high`, `medium`, `low`), Gemini 3.1 Pro, Gemini 3.8 Flash, Gemini 3.5 Flash Lite, Gemini 3 Pro Image (Imagen 3), Claude Opus 4.6 Thinking, Claude Sonnet 4.6.
   - **Anthropic Claude Code (OAuth PKCE & API Key)**: Claude Opus 5.5 / 4.6 Thinking, Fable 5.1, Sonnet 5.5 / 4.6, Haiku 5.5 / 4.5.
   - **Alibaba Token Plan (Bailian Coding Plan / MaaS)**: Qwen 3.8 Max, Qwen 3.8 Flash, DeepSeek V4 Pro, DeepSeek V4.1 Flash, GLM 5.3, Qwen 3 Coder Plus (hỗ trợ Night Discount 40%–50% Off).

2. **Bảng Mô Hình Chủ Lực (Model Catalog)**:

| Nhà cung cấp | Model ID tiêu biểu | Chế độ suy luận / Đặc điểm |
| :--- | :--- | :--- |
| **Google Antigravity** | `antigravity/gemini-4-carbon-high` | **Gemini 4 Carbon High** — Flagship Gen 4 Thinking cho kiến trúc & lập trình phức tạp |
| **Google Antigravity** | `antigravity/gemini-4-argon-high` | **Gemini 4 Argon High** — Suy luận sâu & sinh mã tốc độ cao |
| **Google Antigravity** | `antigravity/gemini-3.1-pro-high` | Gemini 3.1 Pro High — Tối ưu nội dung dài, phân tích & SEO |
| **Google Antigravity** | `antigravity/gemini-3.8-flash-high` | Gemini 3.8 Flash High — Tốc độ phản hồi cực nhanh cho Sub-Agents |
| **Google Antigravity** | `gemini-3-pro-image` | Google Imagen 3 — Tạo ảnh trực tiếp qua `/v1/chat/completions` |
| **Claude Code** | `claude-code/claude-opus-5-5` / `claude-opus-4-6-thinking` | Anthropic Claude Opus — Thiết kế kiến trúc & review mã nguồn |
| **Alibaba Bailian** | `alibaba/qwen3.8-max` / `alibaba/qwen3-coder-plus` | Qwen 3.8 Max & Coder Plus — Hỗ trợ ưu đãi ban đêm 40%–50% Off |

3. **Bảo mật Quản trị Nâng cao (Hardened Admin RBAC & Security)**:
   - **Hardened Admin RBAC**: Phân tách chặt chẽ giữa phiên đăng nhập Admin (HTTP-Only Session Cookie + PBKDF2/SHA-256) / khóa API có `admin_access: true` và khóa API Nhân viên tiêu chuẩn (`admin_access: false`). Nhân viên chỉ được phép gọi suy luận (`/v1/chat/completions`, `/v1/messages`, `/v1/models`) và tự tra cứu hạn mức được cấp phép, hoàn toàn bị chặn truy cập các API quản trị (`/v1/keys`, `/v1/logs`, `/v1/quota`, `/v1/providers`, `/v1/database`, `/v1/endpoint`).
   - **Chống Tấn công & Rò rỉ Dữ liệu**: Chống Slowloris (`ReadHeaderTimeout`), khóa biên đường dẫn chống Path Traversal (`resolvePathSafe`) trên MCP Dispatcher, tự động từ chối ghi đè tệp nguồn nếu upstream trả về cảnh báo phiên bản client lỗi thời, và tắt máy an toàn (Graceful Shutdown `SIGINT`/`SIGTERM`) bảo toàn dữ liệu cấu hình, đường hầm và kết nối DB.

4. **PostgreSQL 18 + Native `pgvector` HNSW Indexing & Async I/O**:
   - **PostgreSQL 18 (`pgvector/pgvector:pg18`)**: Sử dụng kiến trúc Asynchronous I/O (`io_method=worker`), B-tree Skip Scan và chuẩn mount volume mới `/var/lib/postgresql` (`PGDATA=/var/lib/postgresql/18/docker`), tự động đồng bộ 2 chiều khóa API & bộ nhớ Vector từ `gateway_config.json` khi nâng cấp từ PG16.
   - **Native `pgvector` HNSW Indexing**: Lưu trữ và truy vấn bộ nhớ dài hạn AI (Long-Term Vector Memory & Auto-RAG) trực tiếp bằng kiểu dữ liệu `vector(256)` và chỉ mục **HNSW (`vector_cosine_ops`)** tốc độ cao trên PostgreSQL 18, tự động fallback mượt mà về tìm kiếm Cosine RAM/JSON khi chạy Local không cần cài đặt DB.
   - **Quản lý Khóa API & Nhật ký Prompt Nhân viên**: Cấp phát khóa riêng (`sk-omni-...`) cho từng nhân viên trên Cursor, Claude Code, Cline, Antigravity; lưu bền vững vào `omni_api_keys` & `omni_prompt_logs` để kiểm tra chi tiết Prompt, phản hồi AI, model, số token và độ trễ trên Control Center.

5. **Kiến trúc Định tuyến & Tối ưu Hóa Hiệu Năng**:
   - **Quản lý Điểm cuối & 4 Đường hầm**: Cloud OmniRoute (VPS Custom Domain), Cloudflare Quick Tunnel, Tailscale Funnel, ngrok Tunnel; hỗ trợ chuẩn **MCP (`/v1/mcp/info`)** và **A2A (`/.well-known/agent.json`, `/v1/a2a`)**.
   - **Cân bằng tải Đa tài khoản**: Xoay vòng tài khoản (`round-robin`, `least-used`, `p2c`, `fill-first`) kèm Atomic In-Flight Tracking, Circuit Breaker & Cooldown tự động cho Pool 50+ tài khoản.
   - **Nén Token & Đệm Thông Minh**: Prompt Compression (`Lite`, `Caveman`, `RTK`, `Ultra`), luồng HTTP `gzip`, Semantic/Exact LRU Cache, Model Aliasing, Multi-Model Combos (`failover`, `pipeline`, `consensus`), Speculative Hedged Requests & SSE Keep-Alive Heartbeat.

6. **MCP Server (`omni_parallel_workers`)**:
   - Điều phối **N Goroutines chạy song song 100%** qua cổng Gateway (`antigravity/gemini-4-carbon-high` / `antigravity/gemini-4-argon-high`) và ghi trực tiếp mã nguồn sạch xuống ổ cứng trong một lượt gọi duy nhất.

---

## 🐳 Triển khai nhanh trên VPS (Docker Compose)

```bash
git clone https://github.com/danlaptrinh/omniroute-go.git
cd omniroute-go
docker compose up -d --build
```

Sau khi container chạy, mở trình duyệt truy cập:
- **Control Center UI**: `http://<IP-VPS>:8787/ui`
- **Endpoint API Dashboard**: `http://<IP-VPS>:8787/dashboard/endpoint`
- **OpenAI Compatible Base URL**: `http://<IP-VPS>:8787/v1`
- **Anthropic Native Messages URL**: `http://<IP-VPS>:8787/v1/messages`

---

## 💻 Chạy trực tiếp từ mã nguồn (Local / Windows / Linux)

```bash
# 1. Build và chạy OmniRoute-Go Gateway
go build -o omni-gateway ./cmd/omni-gateway
./omni-gateway -listen 127.0.0.1:8787

# 2. Build MCP Parallel Dispatcher (tùy chọn cho IDE Antigravity / Claude Code)
go build -o omni-parallel-mcp .
```
