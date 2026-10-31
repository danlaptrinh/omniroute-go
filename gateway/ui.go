package gateway

const gatewayUIHTML = `<!DOCTYPE html>
<html lang="vi">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>OmniRoute-Go v2.0 — Control Center</title>
  <style>
    :root {
      --bg: #0d1117;
      --card: #161b26;
      --card-inner: #10141f;
      --border: #283144;
      --border-focus: #7c3aed;
      --orange: #e0531f;
      --orange-hover: #c94617;
      --purple: #7c3aed;
      --purple-hover: #6d28d9;
      --blue: #0284c7;
      --green: #10b981;
      --green-bg: rgba(16, 185, 129, 0.15);
      --red: #f87171;
      --red-bg: rgba(239, 68, 68, 0.15);
      --text: #f1f5f9;
      --muted: #94a3b8;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      padding: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      background: var(--bg);
      color: var(--text);
      min-height: 100vh;
      overflow-x: hidden;
    }
    .app-layout {
      display: flex;
      min-height: 100vh;
      width: 100%;
    }
    .sidebar {
      width: 275px;
      min-width: 275px;
      background: #111520;
      border-right: 1px solid var(--border);
      display: flex;
      flex-direction: column;
      height: 100vh;
      position: sticky;
      top: 0;
      z-index: 100;
      box-shadow: 2px 0 16px rgba(0, 0, 0, 0.4);
    }
    .sidebar-header {
      padding: 20px 18px 16px;
      border-bottom: 1px solid var(--border);
    }
    .brand-title {
      font-size: 17px;
      font-weight: 700;
      letter-spacing: -0.02em;
      color: #fff;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .brand-sub {
      font-size: 11.5px;
      color: var(--muted);
      margin-top: 5px;
      line-height: 1.4;
    }
    .sidebar-nav {
      flex: 1;
      padding: 14px 10px;
      display: flex;
      flex-direction: column;
      gap: 5px;
      overflow-y: auto;
    }
    .nav-btn {
      width: 100%;
      text-align: left;
      background: transparent;
      border: 1px solid transparent;
      color: var(--muted);
      padding: 10px 14px;
      border-radius: 9px;
      font-size: 13px;
      font-weight: 600;
      cursor: pointer;
      display: flex;
      align-items: center;
      gap: 10px;
      transition: all 0.15s ease;
      white-space: nowrap;
    }
    .nav-btn .nav-icon {
      font-size: 16px;
      width: 20px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      flex-shrink: 0;
    }
    .nav-btn .nav-text {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .nav-btn:hover {
      color: #fff;
      background: rgba(255, 255, 255, 0.05);
      border-color: rgba(255, 255, 255, 0.08);
    }
    .nav-btn.active {
      background: #171d2b;
      color: #38bdf8;
      border: 1px solid rgba(56, 189, 248, 0.4);
      box-shadow: 0 2px 10px rgba(56, 189, 248, 0.12);
    }
    .sidebar-footer {
      padding: 14px 16px;
      border-top: 1px solid var(--border);
      background: #0d111a;
    }
    .main-content {
      flex: 1;
      min-width: 0;
      padding: 24px 36px;
      width: 100%;
      box-sizing: border-box;
    }
    @media (max-width: 960px) {
      .app-layout {
        flex-direction: column;
      }
      .sidebar {
        width: 100%;
        min-width: 100%;
        height: auto;
        position: relative;
        border-right: none;
        border-bottom: 1px solid var(--border);
      }
      .sidebar-nav {
        flex-direction: row;
        overflow-x: auto;
      }
      .main-content {
        padding: 16px 14px;
      }
    }
    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 18px;
      padding-bottom: 14px;
      border-bottom: 1px solid var(--border);
    }
    h1 { margin: 0 0 4px 0; font-size: 22px; font-weight: 700; letter-spacing: -0.02em; }
    h2 { margin: 0 0 12px 0; font-size: 16px; color: #e2e8f0; font-weight: 600; }
    .card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 14px;
      padding: 20px;
      margin-bottom: 18px;
      box-shadow: 0 10px 30px rgba(0, 0, 0, 0.4);
    }
    .view-content { display: none; }
    .view-content.active { display: block; }
    .grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
    .btn-row { display: flex; gap: 10px; margin-top: 12px; flex-wrap: wrap; }
    button, .btn {
      background: var(--card-inner);
      color: var(--text);
      border: 1px solid var(--border);
      padding: 8px 14px;
      border-radius: 8px;
      font-size: 12.5px;
      font-weight: 600;
      cursor: pointer;
      display: inline-flex;
      align-items: center;
      gap: 6px;
      text-decoration: none;
      transition: all 0.15s ease;
    }
    button:hover, .btn:hover { border-color: #475569; background: #1c2333; }
    .btn-primary { background: var(--orange); border-color: var(--orange); color: #fff; }
    .btn-primary:hover { background: var(--orange-hover); border-color: var(--orange-hover); }
    .btn-purple { background: var(--purple); border-color: var(--purple); color: #fff; }
    .btn-purple:hover { background: var(--purple-hover); border-color: var(--purple-hover); }
    input, textarea, select {
      width: 100%;
      padding: 9px 12px;
      border-radius: 8px;
      border: 1px solid var(--border);
      background: var(--card-inner);
      color: var(--text);
      font-family: inherit;
      font-size: 13px;
    }
    input:focus, textarea:focus, select:focus {
      outline: none;
      border-color: var(--border-focus);
    }
    .hint { font-size: 12px; color: var(--muted); margin-top: 6px; line-height: 1.4; }
    .badge {
      display: inline-block;
      padding: 3px 8px;
      border-radius: 999px;
      font-size: 11px;
      font-weight: 600;
    }
    .badge-antigravity { background: rgba(224, 83, 31, 0.15); color: #fb923c; border: 1px solid rgba(224, 83, 31, 0.3); }
    .badge-claude { background: rgba(168, 85, 247, 0.15); color: #c084fc; border: 1px solid rgba(168, 85, 247, 0.3); }
    .badge-alibaba { background: rgba(56, 189, 248, 0.15); color: #38bdf8; border: 1px solid rgba(56, 189, 248, 0.3); }
    .badge-green { background: var(--green-bg); color: var(--green); border: 1px solid rgba(16, 185, 129, 0.3); }
    .badge-red { background: var(--red-bg); color: var(--red); border: 1px solid rgba(239, 68, 68, 0.3); }
    table { width: 100%; border-collapse: collapse; margin-top: 10px; font-size: 12.5px; }
    th, td { padding: 9px 12px; text-align: left; border-bottom: 1px solid var(--border); }
    th { color: var(--muted); font-weight: 600; background: var(--card-inner); }
    pre {
      background: #080b10;
      border: 1px solid var(--border);
      padding: 12px;
      border-radius: 8px;
      overflow-x: auto;
      font-size: 12px;
      white-space: pre-wrap;
    }

    /* Modal Styling */
    .modal-backdrop {
      display: none;
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.75);
      backdrop-filter: blur(4px);
      z-index: 999;
      align-items: center;
      justify-content: center;
      padding: 16px;
    }
    .modal-backdrop.open { display: flex; }
    .modal-card {
      background: #171b26;
      border: 1px solid #2b3347;
      border-radius: 14px;
      width: 100%;
      max-width: 520px;
      box-shadow: 0 24px 60px rgba(0,0,0,0.6);
      overflow: hidden;
      animation: modalIn 0.18s ease-out;
    }
    @keyframes modalIn {
      from { transform: scale(0.96); opacity: 0; }
      to { transform: scale(1); opacity: 1; }
    }
    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 14px 18px;
      border-bottom: 1px solid #242c3d;
    }
    .traffic-lights {
      display: flex;
      gap: 7px;
      align-items: center;
    }
    .dot { width: 11px; height: 11px; border-radius: 50%; display: inline-block; }
    .dot-red { background: #ef4444; }
    .dot-yellow { background: #f59e0b; }
    .dot-green { background: #10b981; }
    .modal-title { font-weight: 700; font-size: 15px; color: #fff; }
    .modal-close {
      background: transparent;
      border: none;
      color: var(--muted);
      font-size: 16px;
      cursor: pointer;
      padding: 4px;
    }
    .modal-close:hover { color: #fff; background: transparent; }
    .modal-body { padding: 18px; }
    .modal-footer {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 14px 18px;
      border-top: 1px solid #242c3d;
      background: #121620;
    }
    .form-group { margin-bottom: 16px; }
    .form-label { font-size: 13px; font-weight: 600; color: #e2e8f0; margin-bottom: 6px; display: block; }
    .setting-card {
      background: #10141f;
      border: 1px solid #242c3d;
      border-radius: 10px;
      padding: 12px 14px;
      margin-bottom: 12px;
    }
    .setting-row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;
      padding: 8px 0;
    }
    .setting-row:not(:last-child) {
      border-bottom: 1px solid rgba(255,255,255,0.05);
    }
    .setting-info { flex: 1; }
    .setting-title { font-size: 13px; font-weight: 600; color: #f1f5f9; }
    .setting-desc { font-size: 11.5px; color: var(--muted); margin-top: 3px; line-height: 1.35; }
    .toggle-btn {
      padding: 5px 12px;
      border-radius: 8px;
      font-size: 12px;
      font-weight: 600;
      border: 1px solid #2b3347;
      background: #19202f;
      color: #94a3b8;
      cursor: pointer;
      display: inline-flex;
      align-items: center;
      gap: 6px;
      white-space: nowrap;
      transition: all 0.15s ease;
    }
    .toggle-btn.on {
      background: rgba(16, 185, 129, 0.18);
      color: #34d399;
      border-color: rgba(16, 185, 129, 0.4);
    }
    .comp-card {
      cursor: pointer;
      transition: all 0.15s ease;
    }
    .comp-card:hover {
      border-color: var(--border-focus);
      background: #141b2b;
    }
    .comp-card.active {
      border-color: #10b981;
      background: rgba(16, 185, 129, 0.08);
      box-shadow: 0 0 12px rgba(16, 185, 129, 0.2);
    }
  </style>
</head>
<body>
  <div class="app-layout">
    <!-- SIDEBAR VERTICAL MENU -->
    <aside class="sidebar">
      <div class="sidebar-header">
        <div class="brand-title">
          <span>OmniRoute-Go</span>
          <span class="badge badge-green" style="font-size:10px;">v2.0</span>
        </div>
        <div class="brand-sub">
          AI Routing Engine đa nhà cung cấp: Antigravity, Claude Code, Alibaba Token Plan
        </div>
        <div style="margin-top:10px;">
          <span class="badge" style="background:#0f172a;color:#38bdf8;border:1px solid #1e293b;font-family:monospace;font-size:11px;">127.0.0.1:8787/v1</span>
        </div>
      </div>

      <!-- Vertical Navigation Menu -->
      <nav class="sidebar-nav">
        <button class="nav-btn" onclick="switchNav('view-endpoint')">
          <span class="nav-icon">🎯</span>
          <span class="nav-text">Điểm Cuối (Endpoint API)</span>
        </button>
        <button class="nav-btn active" onclick="switchNav('view-quota')">
          <span class="nav-icon">📊</span>
          <span class="nav-text">Hạn Mức Nhà Cung Cấp</span>
        </button>
        <button class="nav-btn" onclick="switchNav('view-models')">
          <span class="nav-icon">🤖</span>
          <span class="nav-text">Mô Hình Có Sẵn</span>
        </button>
        <button class="nav-btn" onclick="switchNav('view-providers')">
          <span class="nav-icon">🔌</span>
          <span class="nav-text">Kết Nối Nhà Cung Cấp</span>
        </button>
        <button class="nav-btn" onclick="switchNav('view-keys')">
          <span class="nav-icon">🔑</span>
          <span class="nav-text">Khóa API (API Keys)</span>
        </button>
        <button class="nav-btn" onclick="switchNav('view-compression')">
          <span class="nav-icon">⚡</span>
          <span class="nav-text">Cơ Chế Nén Token</span>
        </button>
        <button class="nav-btn" onclick="switchNav('view-optimization')">
          <span class="nav-icon">🚀</span>
          <span class="nav-text">Tối Ưu & Bộ Nhớ Đệm</span>
        </button>
        <button class="nav-btn" onclick="switchNav('view-logs')">
          <span class="nav-icon">📋</span>
          <span class="nav-text">Nhật Ký Kết Nối</span>
        </button>
        <button class="nav-btn" onclick="switchNav('view-playground')">
          <span class="nav-icon">🧪</span>
          <span class="nav-text">Playground (Test & Tạo Ảnh)</span>
        </button>
      </nav>

      <!-- Sidebar Footer -->
      <div class="sidebar-footer">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">
          <span id="authStatusBadge" class="badge" style="background:#1e293b;color:#cbd5e1;cursor:pointer;" onclick="openChangePasswordModal()">🛡️ Admin</span>
          <button id="logoutBtn" style="padding:4px 8px;font-size:11px;" onclick="doLogout()">Đăng xuất</button>
        </div>
        <div style="font-size:11px;color:var(--muted);text-align:center;">
          OmniRoute AI Gateway • Standalone
        </div>
      </div>
    </aside>

    <!-- MAIN CONTENT (FULL WIDTH EXPANSION) -->
    <main class="main-content">
      <!-- Top Bar in Main Content -->
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;padding-bottom:12px;border-bottom:1px solid var(--border);">
        <div>
          <h1 style="margin:0;font-size:20px;font-weight:700;">Bảng Điều Khiển OmniRoute-Go</h1>
          <div style="font-size:12px;color:var(--muted);margin-top:2px;">Hệ thống định tuyến & giám sát Gateway hiệu năng cao</div>
        </div>
        <div style="display:flex;align-items:center;gap:10px;">
          <span class="badge badge-green">🟢 Hoạt động</span>
        </div>
      </div>

  <!-- VIEW 1: PROVIDER QUOTAS (EXACT MATCH SCREENSHOT 1) -->
  <div id="view-quota" class="view-content active">
    <div style="margin-bottom:14px;">
      <h2 style="font-size:17px;margin-bottom:3px;display:flex;align-items:center;gap:8px;">
        <span style="color:#ef4444;">📊</span> Hạn mức của nhà cung cấp
      </h2>
      <div style="font-size:12.5px;color:var(--muted);">
        Cấu hình giới hạn tốc độ và hạn mức cho mỗi khóa API và nhà cung cấp
      </div>
      <div id="quotaAccountCountLabel" style="font-size:13px;font-weight:600;color:#94a3b8;margin-top:8px;">
        Giới hạn của nhà cung cấp
      </div>
    </div>

    <!-- Summary KPI Cards (matching screenshot 1) -->
    <div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(170px, 1fr));gap:12px;margin-bottom:16px;">
      <div class="setting-card" style="padding:14px;border-left:3px solid #38bdf8;">
        <div style="font-size:11px;color:var(--muted);font-weight:600;">TỔNG CỘNG</div>
        <div id="kpiTotal" style="font-size:26px;font-weight:700;color:#f1f5f9;margin-top:4px;">0</div>
      </div>
      <div class="setting-card" style="padding:14px;border-left:3px solid #ef4444;">
        <div style="font-size:11px;color:#f87171;font-weight:600;display:flex;align-items:center;gap:6px;">
          <span style="width:7px;height:7px;border-radius:50%;background:#ef4444;display:inline-block;"></span> NGHIÊM TRỌNG
        </div>
        <div id="kpiCritical" style="font-size:26px;font-weight:700;color:#ef4444;margin-top:4px;">0</div>
      </div>
      <div class="setting-card" style="padding:14px;border-left:3px solid #f59e0b;">
        <div style="font-size:11px;color:#fbbf24;font-weight:600;display:flex;align-items:center;gap:6px;">
          <span style="width:7px;height:7px;border-radius:50%;background:#f59e0b;display:inline-block;"></span> CẢNH BÁO
        </div>
        <div id="kpiWarning" style="font-size:26px;font-weight:700;color:#f59e0b;margin-top:4px;">0</div>
      </div>
      <div class="setting-card" style="padding:14px;border-left:3px solid #10b981;">
        <div style="font-size:11px;color:#34d399;font-weight:600;display:flex;align-items:center;gap:6px;">
          <span style="width:7px;height:7px;border-radius:50%;background:#10b981;display:inline-block;"></span> HOẠT ĐỘNG TỐT
        </div>
        <div id="kpiHealthy" style="font-size:26px;font-weight:700;color:#10b981;margin-top:4px;">0</div>
      </div>
    </div>

    <!-- Filter Pills & Action Row -->
    <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;margin-bottom:14px;">
      <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;">
        <span style="font-size:11.5px;color:var(--muted);font-weight:600;">HẠNG:</span>
        <button id="pillTierAll" class="btn" style="padding:4px 9px;font-size:11px;background:#242d3d;color:#fff;" onclick="filterQuotaTier('all')">Tất cả</button>
        <button id="pillTierUltra" class="btn" style="padding:4px 9px;font-size:11px;" onclick="filterQuotaTier('Ultra')">Ultra</button>
        <button id="pillTierPro" class="btn" style="padding:4px 9px;font-size:11px;" onclick="filterQuotaTier('Pro')">Pro</button>
        <button id="pillTierLite" class="btn" style="padding:4px 9px;font-size:11px;" onclick="filterQuotaTier('Lite')">Lite</button>
      </div>
      <div>
        <button onclick="loadQuotaSummary(true)">🔄 Làm mới tất cả</button>
      </div>
    </div>

    <!-- Account Quota Cards Grid (matching screenshot 1) -->
    <div id="quotaCardsGrid" style="display:grid;grid-template-columns:repeat(auto-fill, minmax(320px, 1fr));gap:14px;">
      Đang tải danh sách hạn mức tài khoản...
    </div>
  </div>

  <!-- VIEW 2: AVAILABLE MODELS (EXACT MATCH SCREENSHOT 2) -->
  <div id="view-models" class="view-content">
    <div style="margin-bottom:14px;">
      <h2 style="font-size:17px;margin-bottom:3px;display:flex;align-items:center;gap:8px;">
        <span>🤖</span> Mô hình có sẵn
      </h2>
      <div style="font-size:12.5px;color:var(--muted);">
        Kiểm tra độ trễ, khả năng tương thích và quản lý hiển thị các mô hình thuộc Antigravity (Gen 3/4 & Imagen 3), Claude Code, Alibaba.
      </div>
    </div>

    <!-- Controls Bar (matching screenshot 2) -->
    <div class="setting-card" style="padding:12px 14px;margin-bottom:16px;">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div style="display:flex;gap:8px;align-items:center;flex:1;max-width:380px;">
          <input id="modelSearchInput" type="text" placeholder="🔍 Lọc mô hình (argon, image, opus, qwen)..." oninput="filterModelsGrid()" style="padding:6px 12px;font-size:12px;" />
        </div>
        <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;">
          <button id="pillFilterModAll" class="btn" style="padding:4px 9px;font-size:11px;background:#242d3d;color:#fff;" onclick="setModelVisibilityFilter('all')">Tất cả</button>
          <button id="pillFilterModVisible" class="btn" style="padding:4px 9px;font-size:11px;" onclick="setModelVisibilityFilter('visible')">Đang hiển thị</button>
          <button id="pillFilterModHidden" class="btn" style="padding:4px 9px;font-size:11px;" onclick="setModelVisibilityFilter('hidden')">Bị ẩn</button>
          <button class="btn-primary" style="padding:5px 12px;font-size:11.5px;" onclick="runTestAllModels()">🧪 Kiểm tra tất cả mô hình</button>
        </div>
      </div>
    </div>

    <!-- Models Grid (matching screenshot 2) -->
    <div id="modelsCardsGrid" style="display:grid;grid-template-columns:repeat(auto-fill, minmax(290px, 1fr));gap:12px;">
      Đang tải danh sách mô hình...
    </div>
  </div>

  <!-- VIEW 3: PROVIDERS & ROUTING -->
  <div id="view-providers" class="view-content">
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;">
        <h2>⚙️ Cơ chế Điều Phối (Routing Strategy) & Circuit Breaker</h2>
        <span id="activeStrategyBadge" class="badge badge-antigravity">round-robin</span>
      </div>
      <div style="display:flex;gap:10px;align-items:center;margin-top:6px;">
        <select id="strategySelect" style="max-width:290px;">
          <option value="round-robin">round-robin (Xoay vòng đều giữa các tài khoản)</option>
          <option value="fill-first">fill-first (Ưu tiên tài khoản chính đến khi hết quota/lỗi)</option>
          <option value="p2c">p2c (Power of Two Choices - chọn latency thấp nhất)</option>
          <option value="least-used">least-used (Chọn tài khoản có token/request ít nhất)</option>
        </select>
        <button class="btn-primary" onclick="saveStrategy()">Lưu Cơ Chế</button>
        <span id="strategyMsg" class="hint" style="margin-top:0;"></span>
      </div>
    </div>

    <!-- Provider Connection Tabs -->
    <div class="card">
      <h2>🔌 Kết Nối Nhà Cung Cấp (3 Supported Providers)</h2>
      <div style="display:flex;gap:8px;margin-bottom:14px;border-bottom:1px solid var(--border);padding-bottom:8px;">
        <button class="btn" style="border:none;" onclick="switchProvTab('tab-antigravity')">1. Google Antigravity</button>
        <button class="btn" style="border:none;" onclick="switchProvTab('tab-claude')">2. Claude Code</button>
        <button class="btn" style="border:none;" onclick="switchProvTab('tab-alibaba')">3. Alibaba Token Plan</button>
      </div>

      <!-- Tab Antigravity -->
      <div id="tab-antigravity" class="prov-tab">
        <p class="hint" style="margin:0 0 10px 0;">
          Hỗ trợ Gemini 3 Flash/Pro, Gemini 4 Argon, <strong>Gemini 3 Pro Image (Imagen 3)</strong> và Claude 4.5/4.6 trên Antigravity CCPA.
        </p>
        <div style="font-size:12.5px;color:var(--muted);margin-bottom:6px;">Bước 1: Mở URL xác thực Google OAuth</div>
        <div id="agyOAuthUrlBox" style="background:var(--card-inner);border:1px solid var(--border);padding:10px;border-radius:8px;font-family:monospace;font-size:11.5px;word-break:break-all;">Đang tạo URL...</div>
        <div class="btn-row">
          <button onclick="copyAgyUrl()">📋 Sao chép URL</button>
          <a id="agyOpenBrowserBtn" href="#" target="_blank" class="btn btn-primary">🌐 Mở trình duyệt (Tự động bắt Callback :20128)</a>
        </div>
        <div style="font-size:12.5px;color:var(--muted);margin-top:14px;margin-bottom:6px;">Bước 2: Dán URL Callback hoặc mã code</div>
        <input id="agyCallbackInput" type="text" placeholder="http://127.0.0.1:20128/callback?code=4/0A... hoặc token ya29..." />
        <div class="btn-row" style="justify-content:flex-end;">
          <button onclick="loadAgyUrl()">Làm mới URL</button>
          <button class="btn-primary" onclick="connectAgy()">Kết Nối Antigravity</button>
        </div>
        <div id="agyMsg" class="hint"></div>
      </div>

      <!-- Tab Claude Code -->
      <div id="tab-claude" class="prov-tab" style="display:none;">
        <p class="hint" style="margin:0 0 10px 0;">
          Kết nối Claude Pro / Max qua OAuth PKCE của Claude Code hoặc nhập trực tiếp Token <code>sk-ant-oat...</code> / API Key <code>sk-ant-api...</code>.
        </p>
        <div class="grid-2">
          <div>
            <div style="font-size:12.5px;color:var(--muted);margin-bottom:6px;">Cách 1: OAuth PKCE (Claude Pro/Max)</div>
            <div id="claudeOAuthUrlBox" style="background:var(--card-inner);border:1px solid var(--border);padding:10px;border-radius:8px;font-family:monospace;font-size:11.5px;word-break:break-all;">Đang tạo Claude OAuth URL...</div>
            <div class="btn-row">
              <a id="claudeOpenBrowserBtn" href="#" target="_blank" class="btn btn-purple">🌐 Đăng nhập Claude</a>
              <button onclick="loadClaudeUrl()">Tạo URL mới</button>
            </div>
            <div style="font-size:12.5px;color:var(--muted);margin-top:10px;margin-bottom:6px;">Dán mã code#state hoặc URL callback:</div>
            <input id="claudeCodeInput" type="text" placeholder="code#state hoặc callback URL..." />
          </div>
          <div>
            <div style="font-size:12.5px;color:var(--muted);margin-bottom:6px;">Cách 2: Nhập Token / API Key trực tiếp</div>
            <input id="claudeDirectInput" type="password" placeholder="sk-ant-oat01-... hoặc sk-ant-api03-..." />
            <div style="font-size:12.5px;color:var(--muted);margin-top:10px;margin-bottom:6px;">Label / Nhãn nhận diện:</div>
            <input id="claudeLabelInput" type="text" placeholder="vd: claude-pro-acc-1" />
          </div>
        </div>
        <div class="btn-row" style="justify-content:flex-end;margin-top:14px;">
          <button class="btn-purple" onclick="connectClaude()">Kết Nối Claude Code</button>
        </div>
        <div id="claudeMsg" class="hint"></div>
      </div>

      <!-- Tab Alibaba -->
      <div id="tab-alibaba" class="prov-tab" style="display:none;">
        <p class="hint" style="margin:0 0 10px 0;">
          Alibaba Token Plan / Bailian Coding Plan (DashScope) hỗ trợ Qwen3 Coder Plus, Qwen3 Max, QwQ Plus, DeepSeek R1/V3.
        </p>
        <div class="grid-2">
          <div>
            <div style="font-size:12.5px;color:var(--muted);margin-bottom:6px;">Alibaba API Key (sk-sp-... hoặc sk-...)</div>
            <input id="aliKeyInput" type="password" placeholder="sk-sp-... hoặc sk-..." />
            <div style="font-size:12.5px;color:var(--muted);margin-top:10px;margin-bottom:6px;">Vùng / Region</div>
            <select id="aliRegionSelect">
              <option value="intl-coding">Quốc tế Coding Plan (coding-intl.dashscope.aliyuncs.com)</option>
              <option value="intl-standard">Quốc tế Standard (dashscope-intl.aliyuncs.com)</option>
              <option value="cn-coding">China Coding Plan (coding.dashscope.aliyuncs.com)</option>
              <option value="cn-standard">China Standard (dashscope.aliyuncs.com)</option>
            </select>
          </div>
          <div>
            <div style="font-size:12.5px;color:var(--muted);margin-bottom:6px;">Label / Nhãn nhận diện</div>
            <input id="aliLabelInput" type="text" placeholder="vd: bailian-coding-primary" />
            <div style="font-size:12.5px;color:var(--muted);margin-top:10px;margin-bottom:6px;">Custom Base URL (Tùy chọn)</div>
            <input id="aliBaseURLInput" type="text" placeholder="Để trống nếu dùng mặc định" />
          </div>
        </div>
        <div class="btn-row" style="justify-content:flex-end;margin-top:14px;">
          <button class="btn-primary" onclick="connectAlibaba()">Lưu Alibaba Token Plan</button>
        </div>
        <div id="aliMsg" class="hint"></div>
      </div>
    </div>
  </div>

  <!-- VIEW 4: API KEYS MANAGEMENT -->
  <div id="view-keys" class="view-content">
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;">
        <div>
          <h2>🔑 Quản Lý Khóa API (API Keys)</h2>
          <div style="font-size:12.5px;color:var(--muted);">Tạo và phân quyền các khóa API để dùng cho Cursor, Cline, Claude Code hoặc các client OpenAI.</div>
        </div>
        <button class="btn-purple" onclick="openCreateKeyModal()">+ Tạo Khóa API</button>
      </div>
      <div id="apiKeysTableBox" style="margin-top:14px;">Đang tải danh sách khóa...</div>
    </div>
  </div>

  <!-- VIEW 5: COMPRESSION PIPELINE -->
  <div id="view-compression" class="view-content">
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;">
        <div>
          <h2>⚡ Cơ Chế Nén Prompt & Giảm Tiêu Thụ Token (OmniRoute Compression)</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Tối ưu hóa cấu trúc prompt trước khi gửi tới upstream (Antigravity, Claude, Alibaba) giúp giảm 15% - 75% token, tiết kiệm quota và tăng tốc độ.
          </div>
        </div>
        <span id="activeCompressionBadge" class="badge badge-antigravity">lite</span>
      </div>

      <!-- Compression Mode Cards -->
      <div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(170px, 1fr));gap:12px;margin-top:16px;">
        <div id="compCard-off" class="setting-card comp-card" onclick="selectCompMode('off')">
          <div style="font-weight:700;font-size:13.5px;color:#cbd5e1;">off (Tắt)</div>
          <div style="font-size:11.5px;color:var(--muted);margin-top:4px;">Không nén. Giữ nguyên 100% prompt gốc.</div>
        </div>
        <div id="compCard-lite" class="setting-card comp-card active" onclick="selectCompMode('lite')">
          <div style="font-weight:700;font-size:13.5px;color:#38bdf8;">lite (~15%)</div>
          <div style="font-size:11.5px;color:var(--muted);margin-top:4px;">Chuẩn hoá khoảng trắng, gộp dòng trống thừa, bảo vệ code fences.</div>
        </div>
        <div id="compCard-caveman" class="setting-card comp-card" onclick="selectCompMode('caveman')">
          <div style="font-weight:700;font-size:13.5px;color:#a855f7;">caveman (~30%)</div>
          <div style="font-size:11.5px;color:var(--muted);margin-top:4px;">Lược bỏ từ ngữ khách sáo, hedging, câu chào hỏi thừa thãi.</div>
        </div>
        <div id="compCard-rtk" class="setting-card comp-card" onclick="selectCompMode('rtk')">
          <div style="font-weight:700;font-size:13.5px;color:#f59e0b;">rtk (~60%)</div>
          <div style="font-size:11.5px;color:var(--muted);margin-top:4px;">Cắt gọn log terminal, git diff, build & test khổng lồ của Coding Agent.</div>
        </div>
        <div id="compCard-ultra" class="setting-card comp-card" onclick="selectCompMode('ultra')">
          <div style="font-weight:700;font-size:13.5px;color:#10b981;">ultra (50-75%)</div>
          <div style="font-size:11.5px;color:var(--muted);margin-top:4px;">Kết hợp Caveman + RTK + Thu gọn lịch sử hội thoại dài.</div>
        </div>
      </div>

      <!-- Settings & Toggles -->
      <div class="setting-card" style="margin-top:16px;">
        <div class="setting-row">
          <div class="setting-info">
            <div class="setting-title">HTTP Transport Gzip Compression</div>
            <div class="setting-desc">Tự động nén gzip toàn bộ phản hồi HTTP (Web UI, API) giúp tiết kiệm băng thông khi đưa lên VPS.</div>
          </div>
          <button id="toggleGzipBtn" class="toggle-btn on" onclick="toggleSetting('toggleGzipBtn')">
            🗜️ <span>Đã bật</span>
          </button>
        </div>
      </div>

      <!-- Stats Box -->
      <div class="grid-2" style="margin-top:16px;">
        <div style="background:#10141f;border:1px solid var(--border);border-radius:10px;padding:14px;text-align:center;">
          <div style="font-size:12px;color:var(--muted);">TỔNG TOKEN ĐÃ TIẾT KIỆM</div>
          <div id="statTokensSaved" style="font-size:26px;font-weight:700;color:#10b981;margin-top:4px;">0</div>
        </div>
        <div style="background:#10141f;border:1px solid var(--border);border-radius:10px;padding:14px;text-align:center;">
          <div style="font-size:12px;color:var(--muted);">REQUEST ĐÃ TỐI ƯU HÓA</div>
          <div id="statCallsHandled" style="font-size:26px;font-weight:700;color:#38bdf8;margin-top:4px;">0</div>
        </div>
      </div>

      <div class="btn-row" style="margin-top:16px;justify-content:flex-end;">
        <button class="btn-primary" onclick="saveCompressionConfig()">💾 Lưu Cấu Hình Nén</button>
      </div>
      <div id="compMsg" class="hint"></div>
    </div>
  </div>

  <!-- VIEW 6: ERROR & CONNECTION LOGS -->
  <div id="view-logs" class="view-content">
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>📋 Nhật Ký Kết Nối & Kiểm Tra Lỗi Nhà Cung Cấp</h2>
          <div style="font-size:12.5px;color:var(--muted);">Giám sát chi tiết request, độ trễ và thông báo lỗi (404, 429, 502) từ Antigravity, Claude Code, Alibaba.</div>
        </div>
        <div style="display:flex;align-items:center;gap:8px;">
          <label style="font-size:12px;color:var(--muted);display:flex;align-items:center;gap:5px;cursor:pointer;">
            <input type="checkbox" id="autoRefreshLogsCheckbox" onchange="toggleAutoRefreshLogs(this.checked)" checked /> Tự động làm mới (2s)
          </label>
          <button onclick="loadLogs()">🔄 Làm Mới</button>
          <button onclick="clearLogs()" style="color:#f87171;">🗑️ Xoá Logs</button>
        </div>
      </div>

      <!-- Filters -->
      <div style="display:flex;gap:8px;margin-top:14px;flex-wrap:wrap;">
        <button id="btnFilterAll" class="btn" style="background:#222b3d;color:#fff;" onclick="setLogFilter('all')">Tất cả logs</button>
        <button id="btnFilterErrors" class="btn" onclick="setLogFilter('errors')">⚠️ Chỉ hiển thị Lỗi</button>
        <button id="btnFilterAgy" class="btn" onclick="setLogFilter('antigravity')">Antigravity</button>
        <button id="btnFilterClaude" class="btn" onclick="setLogFilter('claude-code')">Claude Code</button>
        <button id="btnFilterAli" class="btn" onclick="setLogFilter('alibaba')">Alibaba</button>
      </div>

      <div id="logsListBox" style="margin-top:14px;">Đang tải nhật ký...</div>
    </div>
  </div>

  <!-- VIEW 7: PLAYGROUND (CHAT & TẠO ẢNH) -->
  <div id="view-playground" class="view-content">
    <div class="card">
      <h2>🧪 Test Trực Tiếp API (/v1/chat/completions & Tạo Ảnh)</h2>
      <div class="grid-2">
        <div>
          <div style="font-size:12.5px;color:var(--muted);margin-bottom:6px;">Chọn Model hoặc Combo</div>
          <select id="testModelSelect">
            <optgroup label="Tạo Ảnh Gemini (Imagen 3)">
              <option value="gemini-3-pro-image" selected>gemini-3-pro-image (Google Imagen 3)</option>
              <option value="imagen-3">imagen-3</option>
            </optgroup>
            <optgroup label="Google Antigravity (Gen 3 & 4)">
              <option value="gemini-4-argon-high">gemini-4-argon-high (Argon High Thinking)</option>
              <option value="gemini-4-argon-medium">gemini-4-argon-medium</option>
              <option value="gemini-3.8-flash-high">gemini-3.8-flash-high</option>
              <option value="antigravity/claude-opus-4-6-thinking">antigravity/claude-opus-4-6-thinking</option>
            </optgroup>
            <optgroup label="Auto Combos (OmniRoute Failover)">
              <option value="auto/best-coding">auto/best-coding (Antigravity Argon ➔ Claude ➔ Qwen3 Coder)</option>
              <option value="auto/fast">auto/fast (Gemini 3.8 Flash ➔ Haiku 4.5)</option>
              <option value="auto/reasoning">auto/reasoning (Argon High ➔ Claude Opus Thinking ➔ DeepSeek R1)</option>
            </optgroup>
            <optgroup label="Claude Code (Anthropic)">
              <option value="claude-code/claude-opus-4-6-thinking">claude-code/claude-opus-4-6-thinking</option>
              <option value="claude-code/claude-sonnet-4-6">claude-code/claude-sonnet-4-6</option>
            </optgroup>
            <optgroup label="Alibaba Token Plan">
              <option value="alibaba/qwen3-coder-plus">alibaba/qwen3-coder-plus</option>
              <option value="alibaba/deepseek-r1">alibaba/deepseek-r1</option>
            </optgroup>
          </select>
        </div>
        <div>
          <div style="font-size:12.5px;color:var(--muted);margin-bottom:6px;">Prompt Thử Nghiệm</div>
          <input id="testPromptInput" type="text" value="chú mèo phi hành gia bay lơ lửng ngoài vũ trụ phong cách cyberpunk neon cực đẹp" />
        </div>
      </div>
      <div class="btn-row" style="margin-top:14px;">
        <button class="btn-primary" onclick="testChatCompletion()">🚀 Gửi Request</button>
      </div>
      <div style="margin-top:14px;">
        <div id="testOutputContainer">
          <pre id="testOutputBox">Kết quả phản hồi JSON sẽ xuất hiện tại đây...</pre>
        </div>
      </div>
    </div>
  </div>

  <!-- VIEW 8: OPTIMIZATION, CACHE, ALIASES & HEDGED REQUESTS -->
  <div id="view-optimization" class="view-content">
    <!-- Section 1: Response Cache -->
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>⚡ Bộ Nhớ Đệm Phản Hồi (In-Memory Prompt & Response Cache)</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Tự động lưu đệm và trả về kết quả &lt; 1ms cho các prompt giống nhau (SHA-256 hash), hỗ trợ cả streaming replay và non-streaming, tiết kiệm 100% quota và thời gian chờ.
          </div>
        </div>
        <div style="display:flex;gap:8px;">
          <button onclick="loadCacheStats()">🔄 Làm mới</button>
          <button onclick="clearResponseCache()" style="color:#f87171;">🗑️ Xóa Bộ Nhớ Đệm</button>
        </div>
      </div>

      <div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(170px, 1fr));gap:12px;margin-top:16px;">
        <div class="setting-card" style="padding:14px;border-left:3px solid #38bdf8;">
          <div style="font-size:11px;color:var(--muted);font-weight:600;">SỐ MỤC ĐANG LƯU</div>
          <div id="cacheStatEntries" style="font-size:24px;font-weight:700;color:#f1f5f9;margin-top:4px;">0 / 1000</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #10b981;">
          <div style="font-size:11px;color:#34d399;font-weight:600;">TRÚNG CACHE (HITS)</div>
          <div id="cacheStatHits" style="font-size:24px;font-weight:700;color:#10b981;margin-top:4px;">0</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #f59e0b;">
          <div style="font-size:11px;color:#fbbf24;font-weight:600;">TRƯỢT CACHE (MISSES)</div>
          <div id="cacheStatMisses" style="font-size:24px;font-weight:700;color:#fbbf24;margin-top:4px;">0</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #a855f7;">
          <div style="font-size:11px;color:#c084fc;font-weight:600;">TỶ LỆ TRÚNG (HIT RATIO)</div>
          <div id="cacheStatRatio" style="font-size:24px;font-weight:700;color:#c084fc;margin-top:4px;">0.0%</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #10b981;">
          <div style="font-size:11px;color:#34d399;font-weight:600;">TOKEN TIẾT KIỆM</div>
          <div id="cacheStatTokens" style="font-size:24px;font-weight:700;color:#10b981;margin-top:4px;">0</div>
        </div>
      </div>
    </div>

    <!-- Section 2: Model Aliases -->
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>🔀 Bí Danh Mô Hình (Model Aliasing)</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Tự động ánh xạ model Cursor, Cline, Claude Code hoặc OpenAI (ví dụ <code>gpt-4o</code>, <code>claude-3-5-sonnet</code>, <code>cursor-fast</code>, <code>deepseek-r1</code>) sang model tối ưu của Antigravity/Alibaba.
          </div>
        </div>
        <button class="btn-purple" onclick="openAddAliasModal()">+ Thêm Bí Danh</button>
      </div>

      <div id="aliasesTableBox" style="margin-top:14px;">Đang tải danh sách bí danh...</div>
    </div>

    <!-- Section 3: Hedged Requests & SSE Keep-Alive -->
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>🏎️ Đua Tải Đầu Cơ (Hedged Requests) & SSE Heartbeat</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Giảm triệt để tail latency (p99 spikes) bằng cách tự động bắn thêm request song song khi upstream bị chậm, và duy trì SSE ping để chống timeout.
          </div>
        </div>
        <span id="hedgedActiveBadge" class="badge badge-antigravity">Đang tắt</span>
      </div>

      <div class="setting-card" style="margin-top:16px;">
        <div class="setting-row">
          <div class="setting-info">
            <div class="setting-title">Kích hoạt Hedged Requests (Speculative Racing)</div>
            <div class="setting-desc">Khi một request đến model/tài khoản đầu tiên vượt quá thời gian trễ chỉ định, tự động kích hoạt worker chạy song song trên tài khoản/model dự phòng. Bên nào trả về trước sẽ giành chiến thắng (cancel bên còn lại).</div>
          </div>
          <button id="toggleHedgedBtn" class="toggle-btn" onclick="toggleSetting('toggleHedgedBtn')">
            🏎️ <span>Đã tắt</span>
          </button>
        </div>
        <div style="margin-top:12px;padding-top:12px;border-top:1px solid rgba(255,255,255,0.06);display:flex;align-items:center;gap:12px;">
          <div style="font-size:12.5px;color:#e2e8f0;white-space:nowrap;font-weight:600;">Độ trễ kích hoạt đua tải (Hedge Delay ms):</div>
          <input id="hedgeDelayInput" type="number" value="1500" min="200" max="10000" step="100" style="max-width:140px;padding:6px 10px;" />
          <span style="font-size:12px;color:var(--muted);">(Khuyến nghị: 1200ms - 2000ms)</span>
        </div>
        <div style="margin-top:12px;padding-top:12px;border-top:1px solid rgba(255,255,255,0.06);display:flex;align-items:center;gap:10px;">
          <span style="color:#10b981;font-size:14px;">💓</span>
          <div style="font-size:12px;color:var(--muted);">
            <strong>SSE Keep-Alive Heartbeat:</strong> Tự động kích hoạt (mỗi 2.5s) gửi <code>: ping - thinking\n\n</code> khi Claude Opus Thinking, Gemini 4 Argon, DeepSeek R1 đang suy nghĩ sâu, chống ngắt kết nối IDE.
          </div>
        </div>
      </div>

      <div class="btn-row" style="margin-top:16px;justify-content:flex-end;">
        <button class="btn-primary" onclick="saveHedgedConfig()">💾 Lưu Cấu Hình Hedging</button>
      </div>
      <div id="hedgedMsg" class="hint"></div>
    </div>
  </div>

  <!-- VIEW 9: ENDPOINT API (MATCHING SCREENSHOT media_1793434160018_03e903b3.png) -->
  <div id="view-endpoint" class="view-content">
    <!-- Top Header -->
    <div style="display:flex;align-items:center;gap:12px;margin-bottom:16px;">
      <div style="width:38px;height:38px;border-radius:10px;background:rgba(239,68,68,0.12);border:1px solid rgba(239,68,68,0.3);display:flex;align-items:center;justify-content:center;font-size:18px;color:#ef4444;">
        🎯
      </div>
      <div>
        <h2 style="margin:0;font-size:18px;font-weight:700;color:#fff;">Điểm cuối</h2>
        <div style="font-size:12px;color:var(--muted);margin-top:2px;">Quản lý các endpoint proxy, MCP, A2A và API (Sẵn sàng Deploy VPS)</div>
      </div>
    </div>

    <!-- Breadcrumb -->
    <div style="font-size:12px;color:var(--muted);margin-bottom:12px;">
      Trang tổng quan <span style="margin:0 4px;">›</span> <strong style="color:#e2e8f0;">Điểm cuối</strong>
    </div>

    <!-- Sub-tabs: API | MCP | A2A | Nguồn ngữ cảnh -->
    <div style="display:inline-flex;gap:4px;background:#111520;border:1px solid #242c3d;padding:4px;border-radius:10px;margin-bottom:18px;">
      <button id="epSubBtn-api" class="btn" style="padding:6px 14px;font-size:12px;background:#1e2638;color:#fff;border-color:#334155;" onclick="switchEndpointSubTab('api')">🌐 API</button>
      <button id="epSubBtn-mcp" class="btn" style="padding:6px 14px;font-size:12px;background:transparent;border-color:transparent;color:var(--muted);" onclick="switchEndpointSubTab('mcp')">🔌 MCP</button>
      <button id="epSubBtn-a2a" class="btn" style="padding:6px 14px;font-size:12px;background:transparent;border-color:transparent;color:var(--muted);" onclick="switchEndpointSubTab('a2a')">🕸️ A2A</button>
      <button id="epSubBtn-ctx" class="btn" style="padding:6px 14px;font-size:12px;background:transparent;border-color:transparent;color:var(--muted);" onclick="switchEndpointSubTab('ctx')">🗄️ Nguồn ngữ cảnh &amp; VPS</button>
    </div>

    <!-- SUB-TAB 1: API ENDPOINT & TUNNELS -->
    <div id="epSubTab-api" class="ep-subtab">
      <div class="card" style="background:#121620;border:1px solid #232a3b;padding:22px;">
        <h2 style="font-size:16px;font-weight:700;color:#fff;margin-bottom:14px;">Endpoint API</h2>

        <!-- ENDPOINT ĐANG HOẠT ĐỘNG BOX -->
        <div style="background:rgba(127, 29, 29, 0.12);border:1px solid rgba(239, 68, 68, 0.28);border-radius:10px;padding:14px 16px;margin-bottom:16px;">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px;">
            <span style="font-size:10.5px;font-weight:700;letter-spacing:0.06em;color:#f87171;text-transform:uppercase;">ENDPOINT ĐANG HOẠT ĐỘNG</span>
            <button style="padding:3px 9px;font-size:11px;background:#1a202e;" onclick="editPublicDomainPrompt()">✏️ Đổi tên miền VPS</button>
          </div>

          <!-- Công khai -->
          <div style="display:flex;justify-content:space-between;align-items:center;padding:6px 0;gap:12px;">
            <div style="display:flex;align-items:center;gap:10px;min-width:110px;">
              <span style="width:7px;height:7px;border-radius:50%;background:#10b981;display:inline-block;"></span>
              <span style="font-size:12.5px;color:#cbd5e1;">Công khai</span>
            </div>
            <div style="flex:1;font-family:monospace;font-size:13px;font-weight:700;color:#fff;word-break:break-all;" id="epActivePublicURL">https://omni.huongque.com/v1</div>
            <button title="Sao chép Endpoint Công khai" style="padding:5px 9px;font-size:12px;" onclick="copyEndpointField('epActivePublicURL')">📋</button>
          </div>

          <!-- Cục bộ -->
          <div style="display:flex;justify-content:space-between;align-items:center;padding:6px 0;gap:12px;">
            <div style="display:flex;align-items:center;gap:10px;min-width:110px;">
              <span style="width:7px;height:7px;border-radius:50%;background:#10b981;display:inline-block;"></span>
              <span style="font-size:12.5px;color:#cbd5e1;">Cục bộ</span>
            </div>
            <div style="flex:1;font-family:monospace;font-size:13px;font-weight:700;color:#fff;word-break:break-all;" id="epActiveLocalURL">http://localhost:8787/v1</div>
            <button title="Sao chép Endpoint Cục bộ" style="padding:5px 9px;font-size:12px;" onclick="copyEndpointField('epActiveLocalURL')">📋</button>
          </div>
        </div>

        <!-- MÁY CHỦ CỤC BỘ ROW -->
        <div style="display:flex;justify-content:space-between;align-items:center;padding:12px 4px;border-bottom:1px solid rgba(255,255,255,0.06);margin-bottom:16px;flex-wrap:wrap;gap:10px;">
          <div style="display:flex;align-items:center;gap:10px;">
            <span style="color:#10b981;font-size:16px;">🖥️</span>
            <span style="font-weight:700;font-size:13.5px;color:#fff;">Máy chủ cục bộ</span>
            <span id="epHostID" style="font-family:monospace;font-size:12px;color:#64748b;">569fb55e</span>
            <span id="epLanServerURL" style="font-family:monospace;font-size:12.5px;color:#94a3b8;">172.17.0.2:8787/v1</span>
            <button title="Sao chép IP máy chủ" style="padding:2px 6px;font-size:11px;background:transparent;border:none;color:#94a3b8;" onclick="copyEndpointField('epLanServerURL')">📋</button>
          </div>
          <div style="display:flex;align-items:center;gap:10px;">
            <span class="badge badge-green" style="padding:4px 10px;">● Đang chạy</span>
            <button style="padding:6px 12px;font-size:12px;" onclick="copyEndpointField('epLanServerURL')">📋 Sao chép</button>
          </div>
        </div>

        <!-- ĐƯỜNG HẦM (TUNNELS) -->
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px;">
          <div style="font-size:11px;font-weight:700;color:var(--muted);letter-spacing:0.05em;text-transform:uppercase;">
            🔗 ĐƯỜNG HẦM
          </div>
          <div id="epActiveTunnelsCount" style="font-size:11.5px;color:var(--muted);">1/4 đang hoạt động</div>
        </div>

        <!-- 1. Cloud OmniRoute -->
        <div style="display:flex;justify-content:space-between;align-items:center;padding:13px 4px;border-bottom:1px solid rgba(255,255,255,0.05);flex-wrap:wrap;gap:10px;">
          <div style="display:flex;align-items:center;gap:10px;">
            <span style="color:#38bdf8;font-size:16px;">☁️</span>
            <div>
              <div style="font-weight:600;font-size:13.5px;color:#f1f5f9;">Cloud OmniRoute</div>
              <div id="epCloudSubUrl" style="font-size:11px;color:var(--muted);font-family:monospace;">https://omni.huongque.com/v1</div>
            </div>
          </div>
          <div style="display:flex;align-items:center;gap:10px;">
            <span id="epBadgeCloud" class="badge badge-green">● Đang hoạt động</span>
            <button id="epBtnCloud" style="padding:6px 12px;font-size:12px;background:rgba(239,68,68,0.14);color:#f87171;border-color:rgba(239,68,68,0.35);" onclick="toggleEndpointTunnel('cloud')">☁️ Tắt đám mây</button>
          </div>
        </div>

        <!-- 2. Cloudflare Quick Tunnel -->
        <div style="display:flex;justify-content:space-between;align-items:center;padding:13px 4px;border-bottom:1px solid rgba(255,255,255,0.05);flex-wrap:wrap;gap:10px;">
          <div style="display:flex;align-items:center;gap:10px;">
            <span style="color:#f59e0b;font-size:16px;">🟠</span>
            <div>
              <div style="font-weight:600;font-size:13.5px;color:#f1f5f9;">Cloudflare Quick Tunnel</div>
              <div id="epCfSubUrl" style="font-size:11px;color:var(--muted);font-family:monospace;">Tự động cấp phát tên miền *.trycloudflare.com miễn phí</div>
            </div>
          </div>
          <div style="display:flex;align-items:center;gap:10px;">
            <span id="epBadgeCloudflare" class="badge" style="background:#1e293b;color:#94a3b8;">Đã đóng</span>
            <button id="epBtnCloudflare" style="padding:6px 12px;font-size:12px;background:linear-gradient(90deg,#ec4899,#8b5cf6);border:none;color:#fff;" onclick="toggleEndpointTunnel('cloudflare')">☁️ Bật Tunnel</button>
          </div>
        </div>

        <!-- 3. Tailscale Funnel -->
        <div style="display:flex;justify-content:space-between;align-items:center;padding:13px 4px;border-bottom:1px solid rgba(255,255,255,0.05);flex-wrap:wrap;gap:10px;">
          <div style="display:flex;align-items:center;gap:10px;">
            <span style="color:#818cf8;font-size:16px;">🟣</span>
            <div>
              <div style="font-weight:600;font-size:13.5px;color:#f1f5f9;">Tailscale Funnel</div>
              <div id="epTsSubUrl" style="font-size:11px;color:var(--muted);font-family:monospace;"> Chia sẻ bảo mật qua mạng lưới Tailscale Funnel (*.ts.net)</div>
            </div>
          </div>
          <div style="display:flex;align-items:center;gap:10px;">
            <span id="epBadgeTailscale" class="badge" style="background:#1e293b;color:#94a3b8;">Chưa cài đặt</span>
            <button id="epBtnTailscale" style="padding:6px 12px;font-size:12px;background:linear-gradient(90deg,#ec4899,#8b5cf6);border:none;color:#fff;" onclick="toggleEndpointTunnel('tailscale')">🚀 Cài đặt &amp; Bật</button>
          </div>
        </div>

        <!-- 4. Đường hầm ngrok -->
        <div style="padding:13px 4px;border-bottom:1px solid rgba(255,255,255,0.06);">
          <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
            <div style="display:flex;align-items:center;gap:10px;">
              <span style="color:#a855f7;font-size:16px;">🟣</span>
              <div>
                <div style="font-weight:600;font-size:13.5px;color:#f1f5f9;">Đường hầm ngrok</div>
                <div id="epNgrokSubUrl" style="font-size:11px;color:var(--muted);font-family:monospace;">Cổng public tốc độ cao qua ngrok</div>
              </div>
            </div>
            <div style="display:flex;align-items:center;gap:10px;">
              <span id="epBadgeNgrok" class="badge" style="background:rgba(245,158,11,0.15);color:#fbbf24;border:1px solid rgba(245,158,11,0.3);">Cần xác thực</span>
              <button id="epBtnNgrok" style="padding:6px 12px;font-size:12px;background:linear-gradient(90deg,#ec4899,#8b5cf6);border:none;color:#fff;" onclick="toggleEndpointTunnel('ngrok')">🌐 Bật đường hầm</button>
              <button style="padding:4px 8px;font-size:11px;background:transparent;border:none;color:var(--muted);" onclick="toggleNgrokAuthBox()">⌄</button>
            </div>
          </div>
          <div id="epNgrokAuthBox" style="display:none;margin-top:10px;padding:10px;background:#0b0f17;border-radius:8px;border:1px solid #242c3d;">
            <label style="font-size:11.5px;color:var(--muted);display:block;margin-bottom:5px;">Ngrok Authtoken (Lưu trên máy chủ VPS):</label>
            <div style="display:flex;gap:8px;">
              <input id="epNgrokTokenInput" type="password" placeholder="Nhập ngrok authtoken (vd: 2abcXYZ...)" style="font-family:monospace;font-size:12px;" />
              <button class="btn-primary" style="white-space:nowrap;" onclick="saveNgrokAuthToken()">Lưu Token</button>
            </div>
          </div>
        </div>

        <!-- LỜI NHẮC HỆ THỐNG TÙY CHỈNH (CUSTOM SYSTEM PROMPT) -->
        <div style="padding-top:16px;">
          <div style="display:flex;justify-content:space-between;align-items:center;gap:12px;">
            <div>
              <div style="font-weight:700;font-size:13.5px;color:#fff;">Lời nhắc hệ thống tùy chỉnh</div>
              <div style="font-size:12px;color:var(--muted);margin-top:2px;">Chèn lời nhắc hệ thống tùy chỉnh vào mọi yêu cầu mô hình đi qua Endpoint này</div>
            </div>
            <button id="epCustomSysToggleBtn" class="toggle-btn" onclick="toggleCustomSystemPrompt()">
              <span>Đã tắt</span>
            </button>
          </div>
          <div id="epCustomSysPromptBox" style="display:none;margin-top:12px;">
            <textarea id="epCustomSysPromptInput" rows="3" placeholder="Ví dụ: Bạn là trợ lý kỹ thuật AI chuyên nghiệp của hệ thống BuCloud. Luôn trả lời ngắn gọn, chính xác bằng tiếng Việt..." style="font-size:12.5px;"></textarea>
            <div style="display:flex;justify-content:flex-end;margin-top:8px;">
              <button class="btn-primary" onclick="saveCustomSystemPrompt()">💾 Lưu Lời Nhắc Hệ Thống</button>
            </div>
          </div>
          <div id="epSaveNotice" class="hint" style="margin-top:6px;"></div>
        </div>
      </div>
    </div>

    <!-- SUB-TAB 2: MCP ENDPOINT -->
    <div id="epSubTab-mcp" class="ep-subtab" style="display:none;">
      <div class="card" style="background:#121620;border:1px solid #232a3b;">
        <h2>🔌 Cấu hình Endpoint MCP (Model Context Protocol)</h2>
        <div style="font-size:12.5px;color:var(--muted);margin-bottom:14px;">
          Kết nối trực tiếp IDE (Antigravity, Claude Code, Cursor, Cline) vào máy chủ VPS qua giao thức MCP &amp; Parallel Goroutine Workers:
        </div>
        <pre id="epMcpConfigSnippet">{
  "mcpServers": {
    "omni-parallel-dispatcher": {
      "command": "omni-mcp",
      "env": {
        "OMNI_GATEWAY_URL": "https://omni.huongque.com/v1/chat/completions",
        "OMNI_DEFAULT_MODEL": "antigravity/gemini-4-argon-high"
      }
    }
  }
}</pre>
        <div class="btn-row">
          <button class="btn-primary" onclick="copyEndpointField('epMcpConfigSnippet')">📋 Sao chép cấu hình MCP</button>
        </div>
      </div>
    </div>

    <!-- SUB-TAB 3: A2A ENDPOINT -->
    <div id="epSubTab-a2a" class="ep-subtab" style="display:none;">
      <div class="card" style="background:#121620;border:1px solid #232a3b;">
        <h2>🕸️ Giao thức Agent-to-Agent (A2A) &amp; Đa Tác Tử</h2>
        <div style="font-size:12.5px;color:var(--muted);margin-bottom:14px;">
          Endpoint chuẩn hóa cho phép các AI Agent khác trên VPS (hoặc dự án BuCloud) khám phá và gọi chéo lẫn nhau:
        </div>
        <div class="setting-card">
          <div class="setting-row">
            <div class="setting-info">
              <div class="setting-title">A2A Discovery Card URL</div>
              <div class="setting-desc" id="epA2ADiscoveryURL" style="font-family:monospace;color:#38bdf8;">https://omni.huongque.com/.well-known/agent.json</div>
            </div>
            <button onclick="copyEndpointField('epA2ADiscoveryURL')">📋 Sao chép</button>
          </div>
          <div class="setting-row">
            <div class="setting-info">
              <div class="setting-title">A2A Task Dispatch Endpoint</div>
              <div class="setting-desc" id="epA2ADispatchURL" style="font-family:monospace;color:#34d399;">https://omni.huongque.com/v1/a2a</div>
            </div>
            <button onclick="copyEndpointField('epA2ADispatchURL')">📋 Sao chép</button>
          </div>
        </div>
      </div>
    </div>

    <!-- SUB-TAB 4: NGUỒN NGỮ CẢNH & DEPLOY VPS -->
    <div id="epSubTab-ctx" class="ep-subtab" style="display:none;">
      <div class="card" style="background:#121620;border:1px solid #232a3b;">
        <h2>🐳 Hướng dẫn Deploy Endpoint API lên VPS (Docker Compose &amp; Nginx SSL)</h2>
        <div style="font-size:12.5px;color:var(--muted);margin-bottom:12px;">
          Toàn bộ mã nguồn đã tích hợp sẵn <code>Dockerfile</code> và <code>docker-compose.yml</code>. Chỉ cần chạy lệnh sau trên VPS Ubuntu/Debian của bạn:
        </div>
        <div style="font-size:12px;font-weight:600;color:#e2e8f0;margin-bottom:6px;">1. Khởi chạy bằng Docker Compose trên VPS:</div>
        <pre id="epVpsDockerCmd">git clone &lt;repo-cua-ban&gt; omniroute-vps &amp;&amp; cd omniroute-vps
docker compose up -d --build</pre>
        <div style="font-size:12px;font-weight:600;color:#e2e8f0;margin:12px 0 6px;">2. Cấu hình Nginx Reverse Proxy cho tên miền công khai (SSE Streaming chuẩn):</div>
        <pre id="epVpsNginxConf">server {
    listen 80;
    server_name omni.huongque.com;

    location / {
        proxy_pass http://127.0.0.1:8787;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Connection '';
        proxy_buffering off;
        proxy_cache off;
        chunked_transfer_encoding off;
        proxy_read_timeout 600s;
    }
}</pre>
        <div class="btn-row">
          <button class="btn-primary" onclick="copyEndpointField('epVpsDockerCmd')">📋 Copy lệnh Docker</button>
          <button class="btn-purple" onclick="copyEndpointField('epVpsNginxConf')">📋 Copy cấu hình Nginx</button>
        </div>
      </div>
    </div>
  </div>
    </main>
  </div>

  <!-- MODAL: THÊM BÍ DANH MÔ HÌNH -->
  <div id="addAliasModal" class="modal-backdrop">
    <div class="modal-card" style="max-width:480px;">
      <div class="modal-header">
        <div class="traffic-lights">
          <span class="dot dot-red"></span>
          <span class="dot dot-yellow"></span>
          <span class="dot dot-green"></span>
        </div>
        <div class="modal-title">Thêm / Sửa Bí Danh Mô Hình</div>
        <button class="modal-close" onclick="closeAddAliasModal()">✕</button>
      </div>
      <div class="modal-body">
        <div class="form-group">
          <label class="form-label">Tên bí danh (Alias từ IDE / Client)</label>
          <input id="aliasNameInput" type="text" placeholder="ví dụ: gpt-4o, my-coder, o1" />
          <div class="hint">Tên model mà IDE (Cursor, Claude Code, Cline, Aider...) gửi lên</div>
        </div>
        <div class="form-group">
          <label class="form-label">Mô hình đích thực tế (Target Model)</label>
          <input id="aliasTargetInput" type="text" placeholder="ví dụ: antigravity/claude-sonnet-4-6 hoặc alibaba/qwen3-coder-plus" />
          <div class="hint">Model nội bộ có sẵn trong OmniRoute sẽ trực tiếp xử lý request</div>
        </div>
        <div id="aliasModalMsg" class="hint"></div>
      </div>
      <div class="modal-footer">
        <button class="btn" onclick="closeAddAliasModal()">Hủy</button>
        <button class="btn-primary" onclick="submitAddAlias()">Lưu Bí Danh</button>
      </div>
    </div>
  </div>

  <!-- LOGIN / SETUP OVERLAY -->
  <div id="loginOverlay" class="modal-backdrop" style="background:#090d16;z-index:9999;">
    <div class="modal-card" style="max-width:420px;">
      <div class="modal-header">
        <div class="traffic-lights">
          <span class="dot dot-red"></span>
          <span class="dot dot-yellow"></span>
          <span class="dot dot-green"></span>
        </div>
        <div id="loginTitle" class="modal-title">Đăng nhập OmniRoute Control Center</div>
        <div></div>
      </div>
      <div class="modal-body">
        <div style="text-align:center;margin-bottom:18px;">
          <div style="font-size:38px;margin-bottom:8px;">🔒</div>
          <div id="loginSubtitle" style="font-size:13px;color:var(--muted);line-height:1.4;">
            Nhập mật khẩu quản trị viên để truy cập bảng điều khiển OmniRoute-Go trên VPS.
          </div>
        </div>
        <div class="form-group">
          <label class="form-label">Mật khẩu Quản trị</label>
          <input id="adminPasswordInput" type="password" placeholder="Nhập mật khẩu..." onkeydown="if(event.key==='Enter')doLoginOrSetup()" />
        </div>
        <div id="loginErrorMsg" style="color:#f87171;font-size:12.5px;min-height:18px;margin-top:6px;"></div>
      </div>
      <div class="modal-footer" style="justify-content:flex-end;">
        <button id="loginActionBtn" class="btn-primary" onclick="doLoginOrSetup()">Đăng Nhập</button>
      </div>
    </div>
  </div>

  <!-- CHANGE PASSWORD MODAL -->
  <div id="changePasswordModal" class="modal-backdrop">
    <div class="modal-card" style="max-width:440px;">
      <div class="modal-header">
        <div class="traffic-lights">
          <span class="dot dot-red"></span>
          <span class="dot dot-yellow"></span>
          <span class="dot dot-green"></span>
        </div>
        <div class="modal-title">Đổi mật khẩu Quản trị viên</div>
        <button class="modal-close" onclick="closeChangePasswordModal()">✕</button>
      </div>
      <div class="modal-body">
        <div class="form-group">
          <label class="form-label">Mật khẩu hiện tại</label>
          <input id="oldPassInput" type="password" placeholder="Nhập mật khẩu cũ..." />
        </div>
        <div class="form-group">
          <label class="form-label">Mật khẩu mới</label>
          <input id="newPassInput" type="password" placeholder="Nhập mật khẩu mới..." />
        </div>
        <div id="changePassMsg" class="hint"></div>
      </div>
      <div class="modal-footer">
        <button class="btn" onclick="closeChangePasswordModal()">Hủy</button>
        <button class="btn-primary" onclick="submitChangePassword()">Cập nhật Mật khẩu</button>
      </div>
    </div>
  </div>

  <!-- MODAL: TẠO KHÓA API (EXACT MATCH USER SCREENSHOT) -->
  <div id="createKeyModal" class="modal-backdrop">
    <div class="modal-card">
      <div class="modal-header">
        <div class="traffic-lights">
          <span class="dot dot-red"></span>
          <span class="dot dot-yellow"></span>
          <span class="dot dot-green"></span>
        </div>
        <div class="modal-title">Tạo khóa API</div>
        <button class="modal-close" onclick="closeCreateKeyModal()">✕</button>
      </div>
      <div class="modal-body">
        <div class="form-group">
          <label class="form-label">Tên khóa</label>
          <input id="keyNameInput" type="text" placeholder="ví dụ: Khóa sản xuất" />
          <div class="hint">Chọn một tên mô tả để xác định mục đích của khóa này</div>
        </div>

        <div class="setting-card">
          <div class="setting-row">
            <div class="setting-info">
              <div class="setting-title">Quyền truy cập quản lý</div>
              <div class="setting-desc">Cho phép khóa API này quản lý cấu hình OmniRoute.</div>
            </div>
            <button id="toggleAdminBtn" class="toggle-btn" onclick="toggleSetting('toggleAdminBtn')">
              🛡️ <span>Đã tắt</span>
            </button>
          </div>
        </div>

        <div class="setting-card">
          <div style="font-size:12px;font-weight:600;color:#e2e8f0;margin-bottom:4px;">Khả năng hiển thị tự phục vụ</div>
          <div class="setting-desc" style="margin-bottom:10px;">Kiểm soát những gì khóa này có thể xem về mức sử dụng của chính nó và hạn mức dùng chung của nhà cung cấp phía thượng nguồn.</div>

          <div class="setting-row">
            <div class="setting-info">
              <div class="setting-title">Chi phí riêng và mức sử dụng token</div>
              <div class="setting-desc">Cho phép khóa này gọi endpoint trạng thái của chính nó để xem mức sử dụng tính bằng USD, phần trăm ngân sách và tổng số token.</div>
            </div>
            <button id="toggleSelfUsageBtn" class="toggle-btn on" onclick="toggleSetting('toggleSelfUsageBtn')">
              📈 <span>Đã bật</span>
            </button>
          </div>

          <div class="setting-row">
            <div class="setting-info">
              <div class="setting-title">Hạn mức tài khoản dùng chung</div>
              <div class="setting-desc">Cho phép khóa này xem hạn mức tài khoản dùng chung của nhà cung cấp phía thượng nguồn khi một kết nối cụ thể được cấu hình.</div>
            </div>
            <button id="toggleSharedQuotaBtn" class="toggle-btn" onclick="toggleSetting('toggleSharedQuotaBtn')">
              🏛️ <span>Đã tắt</span>
            </button>
          </div>

          <div class="setting-row">
            <div class="setting-info">
              <div class="setting-title">Cho phép lệnh mức sử dụng cục bộ</div>
              <div class="setting-desc">Cho phép khóa này sử dụng @@om-usage để truy xuất thông tin mức sử dụng và hạn mức đã lưu trong cache mà không cần gọi nhà cung cấp phía thượng nguồn.</div>
            </div>
            <button id="toggleLocalUsageCmdBtn" class="toggle-btn" onclick="toggleSetting('toggleLocalUsageCmdBtn')">
              ⌨️ <span>Đã tắt</span>
            </button>
          </div>
        </div>
      </div>
      <div class="modal-footer">
        <button class="btn" style="border:none;" onclick="closeCreateKeyModal()">Hủy bỏ</button>
        <button class="btn-purple" onclick="submitCreateKey()">Tạo khóa API</button>
      </div>
    </div>
  </div>

  <!-- MODAL: KHÓA VỪA TẠO -->
  <div id="newKeySuccessModal" class="modal-backdrop">
    <div class="modal-card">
      <div class="modal-header">
        <div class="traffic-lights">
          <span class="dot dot-red"></span>
          <span class="dot dot-yellow"></span>
          <span class="dot dot-green"></span>
        </div>
        <div class="modal-title">Khóa API đã tạo thành công</div>
        <button class="modal-close" onclick="closeNewKeySuccessModal()">✕</button>
      </div>
      <div class="modal-body">
        <p style="color:#10b981;font-size:13px;margin-top:0;">✔ Khóa API đã sẵn sàng sử dụng!</p>
        <div style="font-size:12px;color:var(--muted);margin-bottom:8px;">
          Hãy sao chép và lưu khóa này vào nơi an toàn. <strong>Khóa bí mật đầy đủ sẽ chỉ hiển thị 1 lần duy nhất tại đây.</strong>
        </div>
        <div class="form-group">
          <label class="form-label">API Key bí mật:</label>
          <div style="display:flex;gap:8px;">
            <input id="newSecretKeyInput" type="text" readonly style="font-family:monospace;background:#0d1117;color:#34d399;" />
            <button class="btn-primary" onclick="copyNewSecretKey()">📋 Sao chép</button>
          </div>
        </div>
      </div>
      <div class="modal-footer" style="justify-content:flex-end;">
        <button class="btn-purple" onclick="closeNewKeySuccessModal()">Đã Lưu Khóa</button>
      </div>
    </div>
  </div>

  <script>
    let activeLogFilter = 'all';
    let logRefreshTimer = null;
    let selectedCompMode = 'lite';
    let isSetupMode = false;
    let currentQuotaSummary = null;
    let selectedTierFilter = 'all';
    let allDiagnosticsModels = [];
    let modelVisibilityFilter = 'all';

    // --- Authentication Flow ---

    async function checkAuthStatus() {
      try {
        const r = await fetch('/v1/auth/status');
        const d = await r.json();
        const overlay = document.getElementById('loginOverlay');
        const badge = document.getElementById('authStatusBadge');
        const logoutBtn = document.getElementById('logoutBtn');

        if (!d.configured || d.needs_setup) {
          isSetupMode = true;
          document.getElementById('loginTitle').textContent = 'Khởi tạo Mật khẩu Quản trị (VPS)';
          document.getElementById('loginSubtitle').textContent = 'Vui lòng thiết lập mật khẩu quản trị để bảo vệ VPS của bạn.';
          document.getElementById('loginActionBtn').textContent = 'Thiết Lập Mật Khẩu';
          overlay.classList.add('open');
          badge.style.display = 'none';
          logoutBtn.style.display = 'none';
        } else if (!d.authenticated) {
          isSetupMode = false;
          document.getElementById('loginTitle').textContent = 'Đăng nhập OmniRoute Control Center';
          document.getElementById('loginSubtitle').textContent = 'Nhập mật khẩu quản trị viên để truy cập bảng điều khiển OmniRoute-Go trên VPS.';
          document.getElementById('loginActionBtn').textContent = 'Đăng Nhập';
          overlay.classList.add('open');
          badge.style.display = 'none';
          logoutBtn.style.display = 'none';
        } else {
          overlay.classList.remove('open');
          badge.style.display = 'inline-block';
          logoutBtn.style.display = 'inline-flex';
          loadQuotaSummary();
          loadModelsDiagnostics();
        }
      } catch (err) {
        console.error('Auth check error:', err);
      }
    }

    async function doLoginOrSetup() {
      const pass = document.getElementById('adminPasswordInput').value.trim();
      const errBox = document.getElementById('loginErrorMsg');
      errBox.textContent = '';
      if (!pass) {
        errBox.textContent = 'Vui lòng nhập mật khẩu';
        return;
      }
      const endpoint = isSetupMode ? '/v1/auth/setup' : '/v1/auth/login';
      try {
        const r = await fetch(endpoint, {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({password: pass})
        });
        const d = await r.json();
        if (r.ok) {
          document.getElementById('adminPasswordInput').value = '';
          checkAuthStatus();
        } else {
          errBox.textContent = d.error ? d.error.message : (d.message || 'Thất bại');
        }
      } catch (err) {
        errBox.textContent = 'Lỗi kết nối: ' + err.message;
      }
    }

    async function doLogout() {
      await fetch('/v1/auth/logout', {method: 'POST'});
      location.reload();
    }

    function openChangePasswordModal() {
      document.getElementById('oldPassInput').value = '';
      document.getElementById('newPassInput').value = '';
      document.getElementById('changePassMsg').textContent = '';
      document.getElementById('changePasswordModal').classList.add('open');
    }

    function closeChangePasswordModal() {
      document.getElementById('changePasswordModal').classList.remove('open');
    }

    async function submitChangePassword() {
      const oldP = document.getElementById('oldPassInput').value.trim();
      const newP = document.getElementById('newPassInput').value.trim();
      const msg = document.getElementById('changePassMsg');
      if (!newP) { msg.innerHTML = '<span style="color:#f87171">Vui lòng nhập mật khẩu mới</span>'; return; }
      try {
        const r = await fetch('/v1/auth/password', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({old_password: oldP, new_password: newP})
        });
        const d = await r.json();
        if (r.ok) {
          msg.innerHTML = '<span style="color:#10b981">✔ Đổi mật khẩu thành công!</span>';
          setTimeout(closeChangePasswordModal, 1200);
        } else {
          msg.innerHTML = '<span style="color:#f87171">❌ ' + (d.error ? d.error.message : d.message) + '</span>';
        }
      } catch (err) {
        msg.innerHTML = '<span style="color:#f87171">Lỗi: ' + err.message + '</span>';
      }
    }

    // --- VIEW 1: QUOTA SUMMARY FLOW (MATCHING SCREENSHOT 1) ---

    async function loadQuotaSummary(force = false) {
      const box = document.getElementById('quotaCardsGrid');
      try {
        const r = await fetch('/v1/quota' + (force ? '?refresh=true' : ''));
        const d = await r.json();
        currentQuotaSummary = d;

        document.getElementById('kpiTotal').textContent = d.total_accounts || 0;
        document.getElementById('kpiCritical').textContent = d.critical_accounts || 0;
        document.getElementById('kpiWarning').textContent = d.warning_accounts || 0;
        document.getElementById('kpiHealthy').textContent = d.healthy_accounts || 0;
        document.getElementById('quotaAccountCountLabel').textContent = 'Giới hạn của nhà cung cấp (' + (d.total_accounts || 0) + ' tài khoản)';

        renderQuotaCards();
      } catch (err) {
        box.innerHTML = '<div style="color:#f87171;">Lỗi tải hạn mức: ' + err.message + '</div>';
      }
    }

    function filterQuotaTier(tier) {
      selectedTierFilter = tier;
      ['all', 'Ultra', 'Pro', 'Lite'].forEach(t => {
        const b = document.getElementById('pillTier' + t);
        if (b) {
          if (t === tier) { b.style.background = '#242d3d'; b.style.color = '#fff'; }
          else { b.style.background = ''; b.style.color = ''; }
        }
      });
      renderQuotaCards();
    }

    function renderQuotaCards() {
      const box = document.getElementById('quotaCardsGrid');
      if (!currentQuotaSummary || !currentQuotaSummary.accounts || currentQuotaSummary.accounts.length === 0) {
        box.innerHTML = '<div style="color:var(--muted);padding:14px;background:var(--card-inner);border-radius:8px;">Chưa có tài khoản nào được kết nối trong pool.</div>';
        return;
      }

      let accounts = currentQuotaSummary.accounts;
      if (selectedTierFilter !== 'all') {
        accounts = accounts.filter(a => a.tier === selectedTierFilter);
      }

      let html = '';
      for (const acc of accounts) {
        const isCrit = acc.status === 'critical';
        const isWarn = acc.status === 'warning';
        const statusColor = isCrit ? '#ef4444' : (isWarn ? '#f59e0b' : '#10b981');
        const borderCol = isCrit ? 'rgba(239, 68, 68, 0.4)' : '#242c3d';
        const tierBg = acc.tier === 'Ultra' ? 'rgba(16, 185, 129, 0.2)' : 'rgba(56, 189, 248, 0.2)';
        const tierCol = acc.tier === 'Ultra' ? '#34d399' : '#38bdf8';

        let modelBarsHTML = '';
        if (acc.model_quotas && acc.model_quotas.length > 0) {
          for (const mq of acc.model_quotas) {
            const barCol = mq.remaining_percent > 50 ? '#10b981' : (mq.remaining_percent > 20 ? '#f59e0b' : '#ef4444');
            const modelName = mq.display_name || mq.displayName || mq.model_id || 'Model';
            const isGroup = mq.model_id && mq.model_id.startsWith('group-');
            const nameColor = isGroup ? '#60a5fa' : '#e2e8f0';
            const nameWeight = isGroup ? '700' : '600';
            modelBarsHTML += '<div style="margin-bottom:2px;">' +
              '<div style="display:flex;justify-content:space-between;font-size:12px;margin-bottom:3px;">' +
                '<span style="font-weight:' + nameWeight + ';color:' + nameColor + ';">' + htmlEscape(modelName) + '</span>' +
                '<span style="color:' + barCol + ';font-weight:700;">' + mq.remaining_percent + '% left</span>' +
              '</div>' +
              '<div style="background:#080b10;height:6px;border-radius:999px;overflow:hidden;border:1px solid rgba(255,255,255,0.06);">' +
                '<div style="background:' + barCol + ';width:' + mq.remaining_percent + '%;height:100%;border-radius:999px;"></div>' +
              '</div>' +
              '<div style="font-size:10.5px;color:var(--muted);text-align:right;margin-top:2px;">' + (mq.reset_time || '') + '</div>' +
            '</div>';
          }
        }
        if (acc.error_message) {
          modelBarsHTML += '<div style="padding:8px;background:rgba(239, 68, 68, 0.1);border-radius:6px;color:#fca5a5;font-size:11.5px;margin-top:6px;">⚠️ ' + htmlEscape(acc.error_message) + '</div>';
        }

        const safeUsed = (acc.quota_used_percent !== undefined && acc.quota_used_percent !== null) ? acc.quota_used_percent : (acc.provider === 'alibaba' ? 72.24 : 16);
        const safeReset = (acc.reset_time_text || '').replace(/'/g, "\\'");
        const reqSuccess = acc.success_count || 0;
        const reqFail = acc.failure_count || 0;
        const realTokens = (acc.total_tokens || 0).toLocaleString();
        const avgLat = acc.avg_latency_ms ? (acc.avg_latency_ms + ' ms') : '—';
        const routeBadge = acc.enabled
          ? (acc.in_cooldown
              ? '<span style="color:#f59e0b;font-weight:700;">⏳ Cooldown</span>'
              : '<span style="color:#34d399;font-weight:700;">🟢 Đang chạy thực tế</span>')
          : '<span style="color:#94a3b8;font-weight:600;">⏸️ Đã tắt (Không nhận request)</span>';

        const realExecStatsHTML =
          '<div style="background:#0b0f17;border:1px solid rgba(56,189,248,0.18);border-radius:8px;padding:8px 10px;margin-bottom:12px;font-size:11.5px;">' +
            '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:5px;">' +
              '<span style="color:var(--muted);">Trạng thái chạy:</span>' +
              routeBadge +
            '</div>' +
            '<div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:6px;border-top:1px solid rgba(255,255,255,0.06);padding-top:6px;text-align:center;">' +
              '<div>' +
                '<div style="color:var(--muted);font-size:10px;">TOKEN ĐÃ CHẠY</div>' +
                '<div style="color:#38bdf8;font-weight:700;font-size:12.5px;font-family:monospace;">' + realTokens + '</div>' +
              '</div>' +
              '<div>' +
                '<div style="color:var(--muted);font-size:10px;">REQUEST THỰC TẾ</div>' +
                '<div style="color:#e2e8f0;font-weight:700;font-size:12.5px;font-family:monospace;">' + reqSuccess + ' <span style="color:#64748b;font-size:10.5px;">(' + reqFail + ' lỗi)</span></div>' +
              '</div>' +
              '<div>' +
                '<div style="color:var(--muted);font-size:10px;">ĐỘ TRỄ TB</div>' +
                '<div style="color:#a78bfa;font-weight:700;font-size:12.5px;font-family:monospace;">' + avgLat + '</div>' +
              '</div>' +
            '</div>' +
          '</div>';

        html += '<div class="setting-card" style="border:1px solid ' + borderCol + ';background:#111520;padding:14px;border-radius:12px;display:flex;flex-direction:column;justify-content:space-between;">' +
          '<div>' +
            '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px;">' +
              '<div style="display:flex;align-items:center;gap:7px;">' +
                '<span style="width:8px;height:8px;border-radius:50%;background:' + statusColor + ';display:inline-block;"></span>' +
                '<span style="font-weight:700;font-size:13.5px;color:#fff;">' + (acc.provider === 'antigravity' ? 'Antigravity' : (acc.provider === 'claude-code' ? 'Claude Code' : 'Alibaba')) + '</span>' +
                '<span class="badge" style="background:' + tierBg + ';color:' + tierCol + ';">' + acc.tier + '</span>' +
              '</div>' +
              '<button class="toggle-btn ' + (acc.enabled ? 'on' : '') + '" style="padding:3px 8px;font-size:11px;" onclick="toggleAccount(\'' + acc.id + '\', ' + (!acc.enabled) + ')">' +
                (acc.enabled ? 'Bật' : 'Tắt') +
              '</button>' +
            '</div>' +
            '<div style="display:flex;justify-content:space-between;align-items:center;font-size:12px;color:var(--muted);margin-bottom:9px;font-family:monospace;">' +
              '<span title="' + htmlEscape(acc.email || '') + '">' + acc.masked_email + '</span>' +
              (acc.last_used_at ? '<span style="font-size:10.5px;color:#64748b;">Chạy lúc: ' + acc.last_used_at + '</span>' : '') +
            '</div>' +
            realExecStatsHTML +
            '<div style="display:flex;flex-direction:column;gap:9px;margin-bottom:12px;">' +
              modelBarsHTML +
            '</div>' +
          '</div>' +
          '<div style="display:flex;justify-content:space-between;align-items:center;border-top:1px solid rgba(255,255,255,0.06);padding-top:8px;font-size:11px;color:var(--muted);">' +
            '<span>Đã cập nhật ' + acc.last_checked_at + '</span>' +
            '<div style="display:flex;gap:5px;">' +
              '<button style="padding:3px 7px;font-size:10.5px;" onclick="openThresholdModal(\'' + acc.id + '\', \'' + acc.provider + '\', ' + safeUsed + ', \'' + safeReset + '\')">⚙️ Ngưỡng</button>' +
              '<button style="padding:3px 7px;font-size:10.5px;" onclick="loadQuotaSummary(true)">🔄 Làm mới</button>' +
            '</div>' +
          '</div>' +
        '</div>';
      }
      box.innerHTML = html;
    }

    function openThresholdModal(accId, provider, currentUsed, currentReset) {
      const currentVal = (currentUsed !== undefined && currentUsed !== null) ? currentUsed : (provider === 'alibaba' ? 72.24 : 16);
      const promptMsg = provider === 'antigravity'
        ? "Cài đặt % ĐÃ DÙNG của Giới hạn Tuần Gemini cho " + accId + ":\n(Ví dụ: nhập 16 để hiển thị 84% left)"
        : "Cài đặt % mức sử dụng cho tài khoản " + accId + ":\n(Ví dụ: 72.24 cho Alibaba Token Plan)";
      const usedInput = prompt(promptMsg, currentVal);
      if (usedInput === null) return;
      const num = parseFloat(usedInput);
      if (isNaN(num)) {
        alert("Vui lòng nhập số % hợp lệ!");
        return;
      }
      const resetInput = prompt("Thời gian đặt lại / chu kỳ mới:\n(Ví dụ: ⏱️ Đặt lại: 2026-11-28 00:00 (UTC+8))", currentReset || "⏱️ Đặt lại: 2026-11-28 00:00 (UTC+8)");
      if (resetInput === null) return;

      fetch('/v1/quota/threshold', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({account_id: accId, used_percent: num, reset_time: resetInput})
      }).then(r => r.json()).then(data => {
        if (data.status === 'ok') {
          loadQuotaSummary(true);
        } else {
          alert('Lỗi: ' + (data.error || 'Không thể lưu ngưỡng'));
        }
      }).catch(e => alert('Lỗi kết nối: ' + e.message));
    }

    async function toggleAccount(id, enable) {
      try {
        await fetch('/v1/quota/toggle', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({account_id: id, enabled: enable})
        });
        loadQuotaSummary(true);
      } catch (err) {
        alert('Lỗi: ' + err.message);
      }
    }

    // --- VIEW 2: AVAILABLE MODELS FLOW (MATCHING SCREENSHOT 2) ---

    async function loadModelsDiagnostics() {
      const box = document.getElementById('modelsCardsGrid');
      try {
        const r = await fetch('/v1/models/diagnostics');
        const d = await r.json();
        allDiagnosticsModels = d.data || [];
        renderModelsGrid();
      } catch (err) {
        box.innerHTML = '<div style="color:#f87171;">Lỗi tải mô hình: ' + err.message + '</div>';
      }
    }

    function setModelVisibilityFilter(filter) {
      modelVisibilityFilter = filter;
      ['all', 'visible', 'hidden'].forEach(f => {
        const b = document.getElementById('pillFilterMod' + (f.charAt(0).toUpperCase() + f.slice(1)));
        if (b) {
          if (f === filter) { b.style.background = '#242d3d'; b.style.color = '#fff'; }
          else { b.style.background = ''; b.style.color = ''; }
        }
      });
      renderModelsGrid();
    }

    function filterModelsGrid() {
      renderModelsGrid();
    }

    function renderModelsGrid() {
      const box = document.getElementById('modelsCardsGrid');
      const q = (document.getElementById('modelSearchInput').value || '').trim().toLowerCase();

      let filtered = allDiagnosticsModels.filter(m => {
        if (modelVisibilityFilter === 'visible' && m.is_hidden) return false;
        if (modelVisibilityFilter === 'hidden' && !m.is_hidden) return false;
        if (q && !m.id.toLowerCase().includes(q) && !m.badge.toLowerCase().includes(q)) return false;
        return true;
      });

      if (filtered.length === 0) {
        box.innerHTML = '<div style="color:var(--muted);padding:14px;background:var(--card-inner);border-radius:8px;">Không tìm thấy mô hình nào khớp với bộ lọc.</div>';
        return;
      }

      let html = '';
      for (const m of filtered) {
        const statusBadge = m.last_status === 'ok' ?
          ('<span class="badge badge-green">' + m.latency_ms + 'ms</span>') :
          (m.last_status === 'error' ? '<span class="badge badge-red">Lỗi</span>' : '<span class="badge" style="background:#1e293b;color:#94a3b8;">Chưa test</span>');

        const suppCount = (m.supported_accounts || []).length;
        const suppText = suppCount > 0 ? (suppCount + ' tài khoản (' + m.supported_accounts.join(', ') + ')') : 'Chưa có tài khoản';

        html += '<div class="setting-card" style="background:#111520;border:1px solid #242c3d;border-radius:10px;padding:14px;display:flex;flex-direction:column;justify-content:space-between;">' +
          '<div>' +
            '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">' +
              '<div style="display:flex;align-items:center;gap:6px;">' +
                '<span style="font-size:16px;">' + (m.is_image_model ? '🎨' : '🤖') + '</span>' +
                '<span class="badge" style="font-size:10.5px;background:rgba(255,255,255,0.06);color:#cbd5e1;">' + m.badge + '</span>' +
              '</div>' +
              statusBadge +
            '</div>' +
            '<div style="font-size:13px;font-weight:700;color:#f1f5f9;margin-bottom:6px;word-break:break-all;">' +
              m.id +
            '</div>' +
            '<div style="font-size:11.5px;color:var(--muted);margin-bottom:10px;">' +
              'Tính tương thích: <strong style="color:#e2e8f0;">' + suppText + '</strong>' +
            '</div>' +
          '</div>' +
          '<div style="display:flex;justify-content:space-between;align-items:center;border-top:1px solid rgba(255,255,255,0.06);padding-top:10px;">' +
            '<div style="display:flex;gap:6px;">' +
              '<button title="Test nhanh mô hình này" style="padding:4px 8px;font-size:11.5px;" onclick="runTestSingleModel(\'' + m.id + '\')">▶️ Test</button>' +
              '<button title="Ẩn hoặc hiện mô hình" style="padding:4px 8px;font-size:11.5px;" onclick="toggleModelVisibility(\'' + m.id + '\', ' + (!m.is_hidden) + ')">' +
                (m.is_hidden ? '👁️ Hiện' : '🚫 Ẩn') +
              '</button>' +
            '</div>' +
            '<button title="Sao chép tên model" style="padding:4px 8px;font-size:11.5px;" onclick="copyText(\'' + m.id + '\')">📋 Copy ID</button>' +
          '</div>' +
        '</div>';
      }
      box.innerHTML = html;
    }

    async function runTestSingleModel(modelId) {
      alert('Đang gửi test ping tới model ' + modelId + '...');
      try {
        const r = await fetch('/v1/models/test', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({model: modelId})
        });
        const d = await r.json();
        if (d.status === 'ok') {
          alert('✔ Test model ' + modelId + ' thành công! Độ trễ: ' + d.latency_ms + 'ms');
        } else {
          alert('❌ Test thất bại: ' + d.error);
        }
        loadModelsDiagnostics();
      } catch (err) {
        alert('Lỗi kết nối test: ' + err.message);
      }
    }

    async function runTestAllModels() {
      if (!confirm('Bắt đầu kiểm tra kết nối song song toàn bộ các mô hình trong danh mục?')) return;
      try {
        await fetch('/v1/models/test', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({all: true})
        });
        alert('Đã khởi chạy kiểm tra hàng loạt! Trạng thái sẽ cập nhật sau vài giây.');
        setTimeout(loadModelsDiagnostics, 3500);
      } catch (err) {
        alert('Lỗi: ' + err.message);
      }
    }

    async function toggleModelVisibility(modelId, hidden) {
      try {
        await fetch('/v1/models/toggle-visibility', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({model: modelId, hidden: hidden})
        });
        loadModelsDiagnostics();
      } catch (err) {
        alert('Lỗi: ' + err.message);
      }
    }

    function copyText(txt) {
      navigator.clipboard.writeText(txt);
      alert('Đã sao chép: ' + txt);
    }

    // --- NAVIGATION FLOW ---

    function switchNav(viewId) {
      document.querySelectorAll('.view-content').forEach(el => el.classList.remove('active'));
      document.querySelectorAll('.nav-btn').forEach(el => el.classList.remove('active'));
      const target = document.getElementById(viewId);
      if (target) target.classList.add('active');
      if (window.event && window.event.currentTarget && window.event.currentTarget.classList) {
        window.event.currentTarget.classList.add('active');
      } else {
        const btn = document.querySelector('.nav-btn[onclick*="' + viewId + '"]');
        if (btn) btn.classList.add('active');
      }

      if (viewId === 'view-endpoint') loadEndpointStatus();
      if (viewId === 'view-quota') loadQuotaSummary();
      if (viewId === 'view-models') loadModelsDiagnostics();
      if (viewId === 'view-keys') loadApiKeys();
      if (viewId === 'view-logs') loadLogs();
      if (viewId === 'view-providers') loadProvidersAndAccounts();
      if (viewId === 'view-compression') loadCompressionConfig();
      if (viewId === 'view-optimization') loadOptimizationConfig();
    }

    function switchProvTab(tabId) {
      document.querySelectorAll('.prov-tab').forEach(el => el.style.display = 'none');
      document.getElementById(tabId).style.display = 'block';
    }

    function toggleSetting(btnId) {
      const btn = document.getElementById(btnId);
      const isCurrentlyOn = btn.classList.contains('on');
      if (isCurrentlyOn) {
        btn.classList.remove('on');
        btn.querySelector('span').textContent = 'Đã tắt';
      } else {
        btn.classList.add('on');
        btn.querySelector('span').textContent = 'Đã bật';
      }
    }

    // --- COMPRESSION FLOW ---

    async function loadCompressionConfig() {
      try {
        const r = await fetch('/v1/compression');
        const d = await r.json();
        selectedCompMode = d.mode || 'lite';
        selectCompMode(selectedCompMode, false);

        const gzipBtn = document.getElementById('toggleGzipBtn');
        if (d.enable_gzip !== false) {
          gzipBtn.classList.add('on');
          gzipBtn.innerHTML = '🗜️ <span>Đã bật</span>';
        } else {
          gzipBtn.classList.remove('on');
          gzipBtn.innerHTML = '🗜️ <span>Đã tắt</span>';
        }
        document.getElementById('statTokensSaved').textContent = Number(d.total_tokens_saved || 0).toLocaleString();
        document.getElementById('statCallsHandled').textContent = Number(d.total_calls_handled || 0).toLocaleString();
      } catch (err) {
        console.error('Failed to load compression config:', err);
      }
    }

    function selectCompMode(mode) {
      selectedCompMode = mode;
      ['off', 'lite', 'caveman', 'rtk', 'ultra'].forEach(m => {
        const el = document.getElementById('compCard-' + m);
        if (el) {
          if (m === mode) el.classList.add('active');
          else el.classList.remove('active');
        }
      });
      document.getElementById('activeCompressionBadge').textContent = mode;
    }

    async function saveCompressionConfig() {
      const msg = document.getElementById('compMsg');
      msg.textContent = 'Đang lưu cấu hình nén...';
      const enableGzip = document.getElementById('toggleGzipBtn').classList.contains('on');
      try {
        const r = await fetch('/v1/compression', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            mode: selectedCompMode,
            compress_code: false,
            max_history_turns: 10,
            enable_gzip: enableGzip
          })
        });
        const d = await r.json();
        if (r.ok) {
          msg.innerHTML = '<span style="color:#10b981">✔ Đã lưu cấu hình nén: ' + d.config.mode + '</span>';
          loadCompressionConfig();
        } else {
          msg.innerHTML = '<span style="color:#f87171">❌ Lỗi: ' + (d.error ? d.error.message : 'failed') + '</span>';
        }
      } catch (err) {
        msg.innerHTML = '<span style="color:#f87171">Lỗi: ' + err.message + '</span>';
      }
    }

    // --- API KEYS FLOW ---

    function openCreateKeyModal() {
      document.getElementById('keyNameInput').value = '';
      resetToggle('toggleAdminBtn', false);
      resetToggle('toggleSelfUsageBtn', true);
      resetToggle('toggleSharedQuotaBtn', false);
      resetToggle('toggleLocalUsageCmdBtn', false);
      document.getElementById('createKeyModal').classList.add('open');
    }

    function closeCreateKeyModal() {
      document.getElementById('createKeyModal').classList.remove('open');
    }

    function resetToggle(btnId, turnOn) {
      const btn = document.getElementById(btnId);
      if (turnOn) {
        btn.classList.add('on');
        btn.querySelector('span').textContent = 'Đã bật';
      } else {
        btn.classList.remove('on');
        btn.querySelector('span').textContent = 'Đã tắt';
      }
    }

    async function submitCreateKey() {
      const name = document.getElementById('keyNameInput').value.trim() || 'Khóa mới';
      const adminAccess = document.getElementById('toggleAdminBtn').classList.contains('on');
      const allowSelfUsage = document.getElementById('toggleSelfUsageBtn').classList.contains('on');
      const allowSharedQuota = document.getElementById('toggleSharedQuotaBtn').classList.contains('on');
      const allowLocalUsageCmd = document.getElementById('toggleLocalUsageCmdBtn').classList.contains('on');

      try {
        const r = await fetch('/v1/keys', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            name: name,
            admin_access: adminAccess,
            allow_self_usage: allowSelfUsage,
            allow_shared_quota: allowSharedQuota,
            allow_local_usage_cmd: allowLocalUsageCmd
          })
        });
        const d = await r.json();
        if (r.ok && d.key) {
          closeCreateKeyModal();
          openNewKeySuccessModal(d.key.key);
          loadApiKeys();
        } else {
          alert('Lỗi tạo khóa: ' + (d.error ? d.error.message : 'Unknown error'));
        }
      } catch (err) {
        alert('Lỗi kết nối: ' + err.message);
      }
    }

    function openNewKeySuccessModal(secretKey) {
      document.getElementById('newSecretKeyInput').value = secretKey;
      document.getElementById('newKeySuccessModal').classList.add('open');
    }

    function closeNewKeySuccessModal() {
      document.getElementById('newKeySuccessModal').classList.remove('open');
    }

    function copyNewSecretKey() {
      const inp = document.getElementById('newSecretKeyInput');
      inp.select();
      navigator.clipboard.writeText(inp.value);
      alert('Đã sao chép khóa API vào clipboard!');
    }

    async function loadApiKeys() {
      const box = document.getElementById('apiKeysTableBox');
      try {
        const r = await fetch('/v1/keys');
        const d = await r.json();
        if (!d.data || d.data.length === 0) {
          box.innerHTML = '<div style="color:var(--muted);padding:14px;background:var(--card-inner);border-radius:8px;">' +
            'Chưa có khóa API nào. Hệ thống đang ở chế độ mở (Open Access). ' +
            'Khi bạn tạo khóa đầu tiên, hệ thống sẽ tự động yêu cầu xác thực Bearer Token để bảo vệ an toàn VPS.</div>';
          return;
        }

        let html = '<table><thead><tr>' +
          '<th>Tên Khóa</th><th>Masked Key</th><th>Quyền Hạn</th><th>Số Request</th><th>Token Sử Dụng</th><th>Tạo Lúc</th><th>Thao Tác</th>' +
          '</tr></thead><tbody>';

        for (const k of d.data) {
          const adminBadge = k.admin_access ? '<span class="badge badge-purple" style="background:#581c87;color:#d8b4fe;">Admin</span>' : '<span style="color:var(--muted);">User</span>';
          html += '<tr>' +
            '<td><strong>' + k.name + '</strong></td>' +
            '<td><code style="color:#34d399;">' + k.masked_key + '</code></td>' +
            '<td>' + adminBadge + '</td>' +
            '<td>' + (k.total_requests || 0) + '</td>' +
            '<td>' + (k.total_tokens || 0) + '</td>' +
            '<td style="color:var(--muted);font-size:11px;">' + (k.created_at || '').replace('T', ' ').slice(0, 19) + '</td>' +
            '<td><button onclick="deleteKey(\'' + k.id + '\')" style="color:#f87171;padding:4px 8px;font-size:11.5px;">Xoá</button></td>' +
            '</tr>';
        }
        html += '</tbody></table>';
        box.innerHTML = html;
      } catch (err) {
        box.innerHTML = '<div style="color:#f87171;">Lỗi tải danh sách khóa: ' + err.message + '</div>';
      }
    }

    async function deleteKey(id) {
      if (!confirm('Bạn có chắc chắn muốn xoá khóa API này?')) return;
      try {
        const r = await fetch('/v1/keys?id=' + encodeURIComponent(id), {method: 'DELETE'});
        if (r.ok) loadApiKeys();
        else alert('Xoá thất bại');
      } catch (err) {
        alert('Lỗi: ' + err.message);
      }
    }

    // --- LOGS FLOW ---

    function setLogFilter(filter) {
      activeLogFilter = filter;
      const btns = ['btnFilterAll', 'btnFilterErrors', 'btnFilterAgy', 'btnFilterClaude', 'btnFilterAli'];
      btns.forEach(id => {
        const b = document.getElementById(id);
        if (b) { b.style.background = ''; b.style.color = ''; }
      });
      const activeBtnMap = {
        'all': 'btnFilterAll',
        'errors': 'btnFilterErrors',
        'antigravity': 'btnFilterAgy',
        'claude-code': 'btnFilterClaude',
        'alibaba': 'btnFilterAli'
      };
      const activeEl = document.getElementById(activeBtnMap[filter]);
      if (activeEl) {
        activeEl.style.background = '#222b3d';
        activeEl.style.color = '#fff';
      }
      loadLogs();
    }

    function toggleAutoRefreshLogs(enabled) {
      if (logRefreshTimer) {
        clearInterval(logRefreshTimer);
        logRefreshTimer = null;
      }
      if (enabled) {
        logRefreshTimer = setInterval(() => {
          const logsTab = document.getElementById('view-logs');
          if (logsTab && logsTab.classList.contains('active')) {
            loadLogs();
          }
        }, 2000);
      }
    }

    async function loadLogs() {
      const box = document.getElementById('logsListBox');
      let url = '/v1/logs?limit=50';
      if (activeLogFilter === 'errors') url += '&errors_only=true';
      else if (activeLogFilter !== 'all') url += '&provider=' + encodeURIComponent(activeLogFilter);

      try {
        const r = await fetch(url);
        const d = await r.json();
        if (!d.data || d.data.length === 0) {
          box.innerHTML = '<div style="color:var(--muted);padding:14px;background:var(--card-inner);border-radius:8px;">Không có bản ghi nhật ký nào.</div>';
          return;
        }

        let html = '<div style="display:flex;flex-direction:column;gap:8px;">';
        for (const log of d.data) {
          const isErr = log.is_error || log.status_code >= 400;
          const borderCol = isErr ? 'rgba(239, 68, 68, 0.4)' : 'rgba(255,255,255,0.06)';
          const bgCol = isErr ? 'rgba(239, 68, 68, 0.05)' : 'var(--card-inner)';
          const statusBadge = isErr ?
            '<span class="badge badge-red">HTTP ' + log.status_code + '</span>' :
            '<span class="badge badge-green">HTTP ' + log.status_code + '</span>';

          const provBadge = log.provider === 'antigravity' ? 'badge-antigravity' :
                            log.provider === 'claude-code' ? 'badge-claude' : 'badge-alibaba';

          const errBlock = isErr && log.error_message ?
            ('<div style="margin-top:6px;padding:8px;background:rgba(239, 68, 68, 0.1);border-radius:6px;font-family:monospace;font-size:11.5px;color:#fca5a5;white-space:pre-wrap;">' +
             htmlEscape(log.error_message) + '</div>') : '';

          html += '<div style="background:' + bgCol + ';border:1px solid ' + borderCol + ';border-radius:8px;padding:10px 14px;">' +
            '<div style="display:flex;justify-content:space-between;align-items:center;font-size:12px;margin-bottom:4px;">' +
              '<div style="display:flex;align-items:center;gap:8px;">' +
                statusBadge +
                '<span class="badge ' + provBadge + '">' + log.provider + '</span>' +
                '<strong>' + (log.resolved_model || log.requested_model) + '</strong>' +
                (log.account ? ('<span style="color:var(--muted);font-size:11px;">(' + log.account + ')</span>') : '') +
              '</div>' +
              '<div style="color:var(--muted);font-size:11px;">' +
                log.latency_ms + 'ms' +
                (log.total_tokens ? (' | ' + log.total_tokens + ' tok') : '') +
                ' | ' + (log.timestamp || '').slice(11, 19) +
              '</div>' +
            '</div>' +
            errBlock +
          '</div>';
        }
        html += '</div>';
        box.innerHTML = html;
      } catch (err) {
        box.innerHTML = '<div style="color:#f87171;">Lỗi tải logs: ' + err.message + '</div>';
      }
    }

    async function clearLogs() {
      if (!confirm('Bạn có muốn xoá sạch toàn bộ nhật ký kết nối?')) return;
      await fetch('/v1/logs', {method: 'DELETE'});
      loadLogs();
    }

    function htmlEscape(str) {
      return (str || '').replace(/[&<>"']/g, m => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
      }[m]));
    }

    // --- PLAYGROUND (CHAT & IMAGE GENERATION) ---

    async function testChatCompletion() {
      const container = document.getElementById('testOutputContainer');
      container.innerHTML = '<pre id="testOutputBox">⏳ Đang gửi request tới API...</pre>';
      const model = document.getElementById('testModelSelect').value;
      const prompt = document.getElementById('testPromptInput').value;

      try {
        const r = await fetch('/v1/chat/completions', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            model: model,
            messages: [{role: 'user', content: prompt}],
            stream: false
          })
        });
        const d = await r.json();

        // Check if response contains image markdown ![...](data:image/...)
        if (d.choices && d.choices[0] && d.choices[0].message) {
          const content = d.choices[0].message.content || '';
          if (content.includes('![Generated Image](data:image/')) {
            const startIdx = content.indexOf('data:image/');
            const endIdx = content.indexOf(')', startIdx);
            if (startIdx !== -1 && endIdx !== -1) {
              const imgUrl = content.substring(startIdx, endIdx);
              container.innerHTML = '<div style="text-align:center;margin-bottom:14px;background:#080b10;padding:16px;border-radius:10px;border:1px solid #283144;">' +
                '<img src="' + imgUrl + '" style="max-width:100%;max-height:480px;border-radius:8px;box-shadow:0 8px 30px rgba(0,0,0,0.6);" />' +
                '<div style="margin-top:8px;color:#10b981;font-weight:600;font-size:13px;">✔ Ảnh tạo thành công bởi Gemini Imagen 3!</div>' +
                '</div>' +
                '<pre>' + htmlEscape(JSON.stringify(d, null, 2)) + '</pre>';
              return;
            }
          }
        }

        container.innerHTML = '<pre>' + htmlEscape(JSON.stringify(d, null, 2)) + '</pre>';
      } catch (err) {
        container.innerHTML = '<pre style="color:#f87171;">Lỗi kết nối: ' + err.message + '</pre>';
      }
    }

    // --- PROVIDERS CONNECTIONS FLOW ---

    async function loadAgyUrl() {
      try {
        const r = await fetch('/v1/oauth/url');
        const d = await r.json();
        document.getElementById('agyOAuthUrlBox').textContent = d.auth_url;
        document.getElementById('agyOpenBrowserBtn').href = d.auth_url;
      } catch (err) {
        document.getElementById('agyOAuthUrlBox').textContent = 'Lỗi: ' + err.message;
      }
    }

    function copyAgyUrl() {
      const text = document.getElementById('agyOAuthUrlBox').textContent;
      navigator.clipboard.writeText(text);
      alert('Đã sao chép URL xác thực Google vào clipboard!');
    }

    async function connectAgy() {
      const inp = document.getElementById('agyCallbackInput').value.trim();
      const msg = document.getElementById('agyMsg');
      if (!inp) { msg.innerHTML = '<span style="color:#f87171">Vui lòng dán URL callback</span>'; return; }
      msg.textContent = 'Đang xử lý OAuth...';
      try {
        const r = await fetch('/v1/oauth/exchange', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({input: inp})
        });
        const d = await r.json();
        if (r.ok) {
          msg.innerHTML = '<span style="color:#10b981">✔ Kết nối Antigravity thành công!</span>';
          loadQuotaSummary(true);
        } else {
          msg.innerHTML = '<span style="color:#f87171">❌ Lỗi: ' + (d.error ? d.error.message : d.message) + '</span>';
        }
      } catch (err) {
        msg.innerHTML = '<span style="color:#f87171">Lỗi: ' + err.message + '</span>';
      }
    }

    let claudeVerifier = '';
    async function loadClaudeUrl() {
      try {
        const r = await fetch('/v1/oauth/claude/url');
        const d = await r.json();
        claudeVerifier = d.code_verifier;
        document.getElementById('claudeOAuthUrlBox').textContent = d.auth_url;
        document.getElementById('claudeOpenBrowserBtn').href = d.auth_url;
      } catch (err) {
        document.getElementById('claudeOAuthUrlBox').textContent = 'Lỗi: ' + err.message;
      }
    }

    async function connectClaude() {
      const code = document.getElementById('claudeCodeInput').value.trim();
      const direct = document.getElementById('claudeDirectInput').value.trim();
      const label = document.getElementById('claudeLabelInput').value.trim() || 'claude-account';
      const msg = document.getElementById('claudeMsg');
      const input = direct || code;
      if (!input) { msg.innerHTML = '<span style="color:#f87171">Vui lòng nhập code hoặc token</span>'; return; }
      msg.textContent = 'Đang kết nối Claude...';
      try {
        const r = await fetch('/v1/oauth/claude/exchange', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({input: input, code_verifier: claudeVerifier, label: label})
        });
        const d = await r.json();
        if (r.ok) {
          msg.innerHTML = '<span style="color:#10b981">✔ Kết nối Claude Code thành công!</span>';
          loadQuotaSummary(true);
        } else {
          msg.innerHTML = '<span style="color:#f87171">❌ Lỗi: ' + (d.error ? d.error.message : d.message) + '</span>';
        }
      } catch (err) {
        msg.innerHTML = '<span style="color:#f87171">Lỗi: ' + err.message + '</span>';
      }
    }

    async function connectAlibaba() {
      const key = document.getElementById('aliKeyInput').value.trim();
      const region = document.getElementById('aliRegionSelect').value;
      const label = document.getElementById('aliLabelInput').value.trim() || 'alibaba-coding';
      const customBase = document.getElementById('aliBaseURLInput').value.trim();
      const msg = document.getElementById('aliMsg');
      if (!key) { msg.innerHTML = '<span style="color:#f87171">Vui lòng nhập API Key Alibaba</span>'; return; }
      msg.textContent = 'Đang lưu Alibaba Token Plan...';
      try {
        const r = await fetch('/v1/providers/alibaba', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({api_key: key, region: region, label: label, base_url: customBase})
        });
        const d = await r.json();
        if (r.ok) {
          msg.innerHTML = '<span style="color:#10b981">✔ Đã lưu Alibaba Token Plan thành công!</span>';
          loadQuotaSummary(true);
        } else {
          msg.innerHTML = '<span style="color:#f87171">❌ Lỗi: ' + (d.error ? d.error.message : d.message) + '</span>';
        }
      } catch (err) {
        msg.innerHTML = '<span style="color:#f87171">Lỗi: ' + err.message + '</span>';
      }
    }

    async function saveStrategy() {
      const st = document.getElementById('strategySelect').value;
      const msg = document.getElementById('strategyMsg');
      msg.textContent = 'Đang lưu...';
      const r = await fetch('/v1/providers/strategy', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({strategy: st})
      });
      const d = await r.json();
      if (r.ok) {
        document.getElementById('activeStrategyBadge').textContent = d.routing_strategy;
        msg.innerHTML = '<span style="color:#10b981">✔ Đã áp dụng: ' + d.routing_strategy + '</span>';
      } else {
        msg.innerHTML = '<span style="color:#f87171">❌ Lỗi</span>';
      }
    }

    // --- VIEW 8: OPTIMIZATION, CACHE, ALIASES & HEDGING FLOW ---

    async function loadOptimizationConfig() {
      await Promise.all([loadCacheStats(), loadAliases(), loadHedgingConfig()]);
    }

    async function loadCacheStats() {
      try {
        const r = await fetch('/v1/cache');
        const d = await r.json();
        document.getElementById('cacheStatEntries').textContent = (d.total_entries || 0) + ' / ' + (d.max_capacity || 1000);
        document.getElementById('cacheStatHits').textContent = d.hits || 0;
        document.getElementById('cacheStatMisses').textContent = d.misses || 0;
        document.getElementById('cacheStatRatio').textContent = ((d.hit_ratio || 0) * 100).toFixed(1) + '%';
        document.getElementById('cacheStatTokens').textContent = (d.total_tokens_saved || 0).toLocaleString();
      } catch (e) {
        console.error('Error loading cache stats:', e);
      }
    }

    async function clearResponseCache() {
      if (!confirm('Bạn có chắc chắn muốn xóa toàn bộ bộ nhớ đệm (Response Cache)?')) return;
      try {
        const r = await fetch('/v1/cache', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({action: 'clear'})
        });
        const d = await r.json();
        alert(d.message || 'Đã xóa bộ nhớ đệm thành công!');
        loadCacheStats();
      } catch (e) {
        alert('Lỗi: ' + e.message);
      }
    }

    async function loadAliases() {
      const box = document.getElementById('aliasesTableBox');
      try {
        const r = await fetch('/v1/aliases');
        const d = await r.json();
        const aliases = d.aliases || {};
        const keys = Object.keys(aliases).sort();
        if (keys.length === 0) {
          box.innerHTML = '<div style="color:var(--muted);padding:10px;">Chưa có bí danh nào được cấu hình.</div>';
          return;
        }

        let html = '<table><thead><tr><th>Tên Bí Danh (Client gửi)</th><th>Mô Hình Đích Thực Tế</th><th>Trạng Thái</th><th>Thao Tác</th></tr></thead><tbody>';
        for (const k of keys) {
          const target = aliases[k];
          html += '<tr>' +
            '<td><code style="color:#38bdf8;font-weight:700;">' + htmlEscape(k) + '</code></td>' +
            '<td><code style="color:#c084fc;">' + htmlEscape(target) + '</code></td>' +
            '<td><span class="badge badge-green">Hoạt động</span></td>' +
            '<td>' +
              '<button style="padding:3px 8px;font-size:11px;color:#f87171;" onclick="deleteAlias(\'' + htmlEscape(k) + '\')">🗑️ Xóa</button>' +
            '</td>' +
          '</tr>';
        }
        html += '</tbody></table>';
        box.innerHTML = html;
      } catch (e) {
        box.innerHTML = '<div style="color:#f87171;">Lỗi tải bí danh: ' + e.message + '</div>';
      }
    }

    function openAddAliasModal() {
      document.getElementById('aliasNameInput').value = '';
      document.getElementById('aliasTargetInput').value = '';
      document.getElementById('aliasModalMsg').textContent = '';
      document.getElementById('addAliasModal').classList.add('open');
    }

    function closeAddAliasModal() {
      document.getElementById('addAliasModal').classList.remove('open');
    }

    async function submitAddAlias() {
      const name = document.getElementById('aliasNameInput').value.trim();
      const target = document.getElementById('aliasTargetInput').value.trim();
      const msg = document.getElementById('aliasModalMsg');
      if (!name || !target) {
        msg.innerHTML = '<span style="color:#f87171;">Vui lòng nhập đầy đủ tên bí danh và mô hình đích!</span>';
        return;
      }
      try {
        const r = await fetch('/v1/aliases', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({alias: name, target_model: target})
        });
        const d = await r.json();
        if (r.ok) {
          closeAddAliasModal();
          loadAliases();
        } else {
          msg.innerHTML = '<span style="color:#f87171;">Lỗi: ' + (d.error || 'Không thể lưu bí danh') + '</span>';
        }
      } catch (e) {
        msg.innerHTML = '<span style="color:#f87171;">Lỗi kết nối: ' + e.message + '</span>';
      }
    }

    async function deleteAlias(alias) {
      if (!confirm('Xóa bí danh "' + alias + '"?')) return;
      try {
        const r = await fetch('/v1/aliases?alias=' + encodeURIComponent(alias), {
          method: 'DELETE'
        });
        const d = await r.json();
        if (r.ok) {
          loadAliases();
        } else {
          alert('Lỗi: ' + (d.error || 'Không thể xóa'));
        }
      } catch (e) {
        alert('Lỗi: ' + e.message);
      }
    }

    async function loadHedgingConfig() {
      try {
        const r = await fetch('/v1/hedging');
        const d = await r.json();
        const btn = document.getElementById('toggleHedgedBtn');
        const badge = document.getElementById('hedgedActiveBadge');
        if (d.enabled) {
          btn.classList.add('on');
          btn.querySelector('span').textContent = 'Đã bật';
          badge.textContent = 'Đang bật (' + d.delay_ms + 'ms)';
          badge.className = 'badge badge-green';
        } else {
          btn.classList.remove('on');
          btn.querySelector('span').textContent = 'Đã tắt';
          badge.textContent = 'Đang tắt';
          badge.className = 'badge badge-antigravity';
        }
        document.getElementById('hedgeDelayInput').value = d.delay_ms || 1500;
      } catch (e) {
        console.error('Error loading hedging config:', e);
      }
    }

    async function saveHedgedConfig() {
      const btn = document.getElementById('toggleHedgedBtn');
      const enabled = btn.classList.contains('on');
      const delay = parseInt(document.getElementById('hedgeDelayInput').value, 10) || 1500;
      const msg = document.getElementById('hedgedMsg');
      msg.textContent = 'Đang lưu...';
      try {
        const r = await fetch('/v1/hedging', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({enabled: enabled, delay_ms: delay})
        });
        const d = await r.json();
        if (r.ok) {
          msg.innerHTML = '<span style="color:#10b981;">✔ Đã lưu cấu hình Hedged Requests!</span>';
          loadHedgingConfig();
        } else {
          msg.innerHTML = '<span style="color:#f87171;">Lỗi: ' + (d.error || 'Thất bại') + '</span>';
        }
      } catch (e) {
        msg.innerHTML = '<span style="color:#f87171;">Lỗi kết nối: ' + e.message + '</span>';
      }
    }

    // --- VIEW 9: ENDPOINT API & TUNNELS FLOW ---
    let currentEndpointData = null;

    function switchEndpointSubTab(subId) {
      document.querySelectorAll('.ep-subtab').forEach(el => el.style.display = 'none');
      const target = document.getElementById('epSubTab-' + subId);
      if (target) target.style.display = 'block';
      ['api', 'mcp', 'a2a', 'ctx'].forEach(id => {
        const b = document.getElementById('epSubBtn-' + id);
        if (b) {
          if (id === subId) {
            b.style.background = '#1e2638';
            b.style.color = '#fff';
            b.style.borderColor = '#334155';
          } else {
            b.style.background = 'transparent';
            b.style.color = 'var(--muted)';
            b.style.borderColor = 'transparent';
          }
        }
      });
    }

    function copyEndpointField(elId) {
      const el = document.getElementById(elId);
      if (!el) return;
      const txt = (el.textContent || el.value || '').trim();
      navigator.clipboard.writeText(txt);
      const notice = document.getElementById('epSaveNotice');
      if (notice) {
        notice.innerHTML = '<span style="color:#10b981;">✔ Đã sao chép vào bộ nhớ tạm: <code>' + htmlEscape(txt.substring(0, 80)) + '</code></span>';
      } else {
        alert('Đã sao chép: ' + txt);
      }
    }

    async function loadEndpointStatus() {
      try {
        const r = await fetch('/v1/endpoint');
        const d = await r.json();
        renderEndpointStatus(d);
      } catch (e) {
        console.error('Error loading endpoint status:', e);
      }
    }

    function renderEndpointStatus(d) {
      if (!d) return;
      currentEndpointData = d;
      document.getElementById('epActivePublicURL').textContent = d.active_public_url || 'https://omni.huongque.com/v1';
      document.getElementById('epActiveLocalURL').textContent = d.active_local_url || 'http://localhost:8787/v1';
      document.getElementById('epHostID').textContent = d.host_id || '569fb55e';
      document.getElementById('epLanServerURL').textContent = d.lan_server_url || '127.0.0.1:8787/v1';
      document.getElementById('epActiveTunnelsCount').textContent = (d.active_tunnels_count || 0) + '/' + (d.total_tunnels_count || 4) + ' đang hoạt động';

      const cfg = d.config || {};
      document.getElementById('epCloudSubUrl').textContent = cfg.public_url || 'https://omni.huongque.com/v1';
      if (cfg.cloudflare_tunnel_url) {
        document.getElementById('epCfSubUrl').textContent = cfg.cloudflare_tunnel_url;
      }
      if (cfg.tailscale_url) {
        document.getElementById('epTsSubUrl').textContent = cfg.tailscale_url;
      }
      if (cfg.ngrok_url) {
        document.getElementById('epNgrokSubUrl').textContent = cfg.ngrok_url;
      }
      if (cfg.ngrok_auth_token) {
        document.getElementById('epNgrokTokenInput').value = cfg.ngrok_auth_token;
      }

      const tunnels = d.tunnels || [];
      for (const t of tunnels) {
        if (t.id === 'cloud') {
          const b = document.getElementById('epBadgeCloud');
          const btn = document.getElementById('epBtnCloud');
          if (t.active) {
            b.className = 'badge badge-green';
            b.textContent = '● Đang hoạt động';
            btn.style.background = 'rgba(239,68,68,0.14)';
            btn.style.color = '#f87171';
            btn.style.borderColor = 'rgba(239,68,68,0.35)';
            btn.textContent = '☁️ Tắt đám mây';
          } else {
            b.className = 'badge';
            b.style.background = '#1e293b';
            b.style.color = '#94a3b8';
            b.textContent = 'Đã đóng';
            btn.style.background = 'linear-gradient(90deg,#ec4899,#8b5cf6)';
            btn.style.color = '#fff';
            btn.style.borderColor = 'transparent';
            btn.textContent = '☁️ Bật đám mây';
          }
        } else if (t.id === 'cloudflare') {
          const b = document.getElementById('epBadgeCloudflare');
          const btn = document.getElementById('epBtnCloudflare');
          if (t.active) {
            b.className = 'badge badge-green';
            b.textContent = '● Đang hoạt động';
            btn.style.background = 'rgba(239,68,68,0.14)';
            btn.style.color = '#f87171';
            btn.textContent = '☁️ Tắt Tunnel';
          } else {
            b.className = 'badge';
            b.style.background = '#1e293b';
            b.style.color = '#94a3b8';
            b.textContent = t.status_label || 'Đã đóng';
            btn.style.background = 'linear-gradient(90deg,#ec4899,#8b5cf6)';
            btn.style.color = '#fff';
            btn.textContent = '☁️ Bật Tunnel';
          }
        } else if (t.id === 'tailscale') {
          const b = document.getElementById('epBadgeTailscale');
          const btn = document.getElementById('epBtnTailscale');
          if (t.active) {
            b.className = 'badge badge-green';
            b.textContent = '● Đang hoạt động';
            btn.style.background = 'rgba(239,68,68,0.14)';
            btn.style.color = '#f87171';
            btn.textContent = 'Tắt Funnel';
          } else {
            b.className = 'badge';
            b.style.background = '#1e293b';
            b.style.color = '#94a3b8';
            b.textContent = t.status_label || 'Chưa cài đặt';
            btn.style.background = 'linear-gradient(90deg,#ec4899,#8b5cf6)';
            btn.style.color = '#fff';
            btn.textContent = '🚀 Cài đặt & Bật';
          }
        } else if (t.id === 'ngrok') {
          const b = document.getElementById('epBadgeNgrok');
          const btn = document.getElementById('epBtnNgrok');
          if (t.active) {
            b.className = 'badge badge-green';
            b.textContent = '● Đang hoạt động';
            btn.style.background = 'rgba(239,68,68,0.14)';
            btn.style.color = '#f87171';
            btn.textContent = '🌐 Tắt đường hầm';
          } else if (t.needs_auth) {
            b.className = 'badge';
            b.style.background = 'rgba(245,158,11,0.15)';
            b.style.color = '#fbbf24';
            b.textContent = 'Cần xác thực';
            btn.style.background = 'linear-gradient(90deg,#ec4899,#8b5cf6)';
            btn.style.color = '#fff';
            btn.textContent = '🌐 Bật đường hầm';
          } else {
            b.className = 'badge';
            b.style.background = '#1e293b';
            b.style.color = '#94a3b8';
            b.textContent = 'Đã đóng';
            btn.style.background = 'linear-gradient(90deg,#ec4899,#8b5cf6)';
            btn.style.color = '#fff';
            btn.textContent = '🌐 Bật đường hầm';
          }
        }
      }

      // Custom System Prompt state
      const sysBtn = document.getElementById('epCustomSysToggleBtn');
      const sysBox = document.getElementById('epCustomSysPromptBox');
      const sysInput = document.getElementById('epCustomSysPromptInput');
      if (cfg.custom_system_prompt !== undefined) {
        sysInput.value = cfg.custom_system_prompt || '';
      }
      if (cfg.custom_system_prompt_on) {
        sysBtn.classList.add('on');
        sysBtn.querySelector('span').textContent = 'Đã bật';
        sysBox.style.display = 'block';
      } else {
        sysBtn.classList.remove('on');
        sysBtn.querySelector('span').textContent = 'Đã tắt';
        sysBox.style.display = 'none';
      }

      // Update MCP and A2A snippets with active public URL
      const pubUrl = d.active_public_url || 'https://omni.huongque.com/v1';
      const basePub = pubUrl.replace(/\/v1\/?$/, '');
      document.getElementById('epMcpConfigSnippet').textContent = JSON.stringify({
        mcpServers: {
          "omni-parallel-dispatcher": {
            command: "omni-mcp",
            env: {
              OMNI_GATEWAY_URL: pubUrl + "/chat/completions",
              OMNI_DEFAULT_MODEL: "antigravity/gemini-4-argon-high"
            }
          }
        }
      }, null, 2);
      document.getElementById('epA2ADiscoveryURL').textContent = basePub + '/.well-known/agent.json';
      document.getElementById('epA2ADispatchURL').textContent = basePub + '/v1/a2a';
    }

    async function toggleEndpointTunnel(tunnelId) {
      let currentlyActive = false;
      if (currentEndpointData && currentEndpointData.tunnels) {
        const found = currentEndpointData.tunnels.find(x => x.id === tunnelId);
        if (found) currentlyActive = found.active;
      }
      if (tunnelId === 'ngrok' && !currentlyActive) {
        const tok = document.getElementById('epNgrokTokenInput').value.trim();
        if (!tok) {
          document.getElementById('epNgrokAuthBox').style.display = 'block';
          document.getElementById('epNgrokTokenInput').focus();
          alert('Vui lòng nhập Ngrok Authtoken ở khung bên dưới trước khi bật đường hầm ngrok!');
          return;
        }
      }
      try {
        const r = await fetch('/v1/endpoint/tunnel', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            tunnel_id: tunnelId,
            enable: !currentlyActive,
            ngrok_auth_token: document.getElementById('epNgrokTokenInput').value.trim()
          })
        });
        const d = await r.json();
        if (r.ok && d.endpoint) {
          renderEndpointStatus(d.endpoint);
          if (d.notice) {
            document.getElementById('epSaveNotice').innerHTML = '<span style="color:#fbbf24;">ℹ️ ' + htmlEscape(d.notice) + '</span>';
          }
        } else {
          alert('Lỗi: ' + (d.error ? d.error.message : 'Không thể chuyển trạng thái đường hầm'));
        }
      } catch (e) {
        alert('Lỗi kết nối: ' + e.message);
      }
    }

    async function editPublicDomainPrompt() {
      const curr = (currentEndpointData && currentEndpointData.config && currentEndpointData.config.public_url) || 'https://omni.huongque.com/v1';
      const val = prompt('Nhập địa chỉ Endpoint Công khai trên VPS của bạn:\n(Ví dụ: https://omni.huongque.com/v1 hoặc http://IP-VPS:8787/v1)', curr);
      if (val === null || !val.trim()) return;
      try {
        const r = await fetch('/v1/endpoint', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({public_url: val.trim()})
        });
        const d = await r.json();
        if (r.ok) {
          renderEndpointStatus(d);
          document.getElementById('epSaveNotice').innerHTML = '<span style="color:#10b981;">✔ Đã cập nhật Endpoint Công khai thành công!</span>';
        }
      } catch (e) {
        alert('Lỗi: ' + e.message);
      }
    }

    function toggleNgrokAuthBox() {
      const box = document.getElementById('epNgrokAuthBox');
      box.style.display = box.style.display === 'none' ? 'block' : 'none';
    }

    async function saveNgrokAuthToken() {
      const tok = document.getElementById('epNgrokTokenInput').value.trim();
      try {
        const r = await fetch('/v1/endpoint', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({ngrok_auth_token: tok})
        });
        const d = await r.json();
        if (r.ok) {
          renderEndpointStatus(d);
          document.getElementById('epSaveNotice').innerHTML = '<span style="color:#10b981;">✔ Đã lưu Ngrok Authtoken!</span>';
        }
      } catch (e) {
        alert('Lỗi: ' + e.message);
      }
    }

    async function toggleCustomSystemPrompt() {
      const btn = document.getElementById('epCustomSysToggleBtn');
      const nextState = !btn.classList.contains('on');
      const promptText = document.getElementById('epCustomSysPromptInput').value;
      try {
        const r = await fetch('/v1/endpoint', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            custom_system_prompt_on: nextState,
            custom_system_prompt: promptText
          })
        });
        const d = await r.json();
        if (r.ok) {
          renderEndpointStatus(d);
          document.getElementById('epSaveNotice').innerHTML = '<span style="color:#10b981;">✔ Đã ' + (nextState ? 'bật' : 'tắt') + ' lời nhắc hệ thống tùy chỉnh!</span>';
        }
      } catch (e) {
        alert('Lỗi: ' + e.message);
      }
    }

    async function saveCustomSystemPrompt() {
      const promptText = document.getElementById('epCustomSysPromptInput').value;
      try {
        const r = await fetch('/v1/endpoint', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            custom_system_prompt_on: true,
            custom_system_prompt: promptText
          })
        });
        const d = await r.json();
        if (r.ok) {
          renderEndpointStatus(d);
          document.getElementById('epSaveNotice').innerHTML = '<span style="color:#10b981;">✔ Đã lưu lời nhắc hệ thống tùy chỉnh!</span>';
        }
      } catch (e) {
        alert('Lỗi: ' + e.message);
      }
    }

    // Initialize on load
    checkAuthStatus();
    loadAgyUrl();
    loadClaudeUrl();
    loadEndpointStatus();
    if (window.location.pathname.includes('/endpoint') || window.location.search.includes('tab=endpoint')) {
      switchNav('view-endpoint');
    }
    toggleAutoRefreshLogs(true);
  </script>
</body>
</html>`
