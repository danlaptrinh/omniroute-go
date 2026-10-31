# OmniRoute-Go v2.0 — AI Routing Engine & Parallel MCP Dispatcher

Hệ thống **AI Gateway đa nhà cung cấp (Multi-Provider Routing Engine)** kết hợp **Parallel Goroutine MCP Server** viết bằng Go thuần (Zero external dependencies), hỗ trợ triển khai độc lập trên Local hoặc VPS qua Docker Compose.

---

## 🌟 Tính năng nổi bật

1. **Định tuyến 3 Nhà cung cấp AI lớn**:
   - **Google Antigravity (Direct CCPA HTTP/2)**: Gemini 4 Argon (`high`/`medium`/`low`), Gemini 3.8 Flash, Gemini 3.5 Flash Lite, Gemini 3.1 Flash Image (Imagen 3), Claude Opus 4.6 Thinking, Claude Sonnet 4.6.
   - **Anthropic Claude Code (OAuth PKCE & API Key)**: Claude Opus 4.6 Thinking, Sonnet 4.6, Haiku 4.5.
   - **Alibaba Token Plan (Bailian Coding Plan / MaaS)**: Qwen 3.8 Max, Qwen 3.8 Flash, DeepSeek V4 Pro, DeepSeek V4.1 Flash, GLM 5.3, Qwen 3 Coder Plus.
2. **Quản lý Điểm cuối (Endpoint API) & Đường hầm (Tunnels)**:
   - Quản lý Endpoint Công khai (`https://omni.huongque.com/v1`), Cục bộ (`http://localhost:8787/v1`) và Máy chủ LAN/Docker.
   - Tích hợp 4 đường hầm: **Cloud OmniRoute (VPS Custom Domain)**, **Cloudflare Quick Tunnel**, **Tailscale Funnel**, **ngrok Tunnel**.
   - Hỗ trợ **Lời nhắc hệ thống tùy chỉnh (Custom System Prompt)** tự động chèn vào mọi yêu cầu mô hình.
   - Hỗ trợ chuẩn **MCP (`/v1/mcp/info`)** và **A2A (`/.well-known/agent.json`, `/v1/a2a`)**.
3. **Theo dõi Token & Hạn mức thực tế (Real-time Quota & Token Tracking)**:
   - Đo chính xác 100% số token thực tế tiêu thụ trên từng tài khoản (`prompt_tokens`, `completion_tokens`, `total_tokens`).
   - Tự động xoay vòng tài khoản (`round-robin`, `least-used`, `p2c`, `fill-first`) kèm Circuit Breaker & Cooldown tự động.
4. **Tối ưu hóa hiệu năng cao**:
   - **Nén Token (Prompt Compression)**: Chế độ `Lite`, `Caveman`, `RTK`, `Ultra` + nén luồng HTTP `gzip`.
   - **Semantic Exact-Match Cache (LRU)**, **Model Aliasing** & **Speculative Hedged Requests** kèm **SSE Keep-Alive Heartbeat**.
5. **MCP Server (`omni_parallel_workers`)**:
   - Phá bỏ giới hạn tuần tự bằng cách điều phối **N Goroutines chạy song song 100%** qua cổng Gateway và ghi trực tiếp kết quả xuống ổ cứng.

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
