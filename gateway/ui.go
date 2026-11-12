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
    .mobile-topbar,
    .mobile-quick-pills,
    .mobile-bottom-nav,
    .mobile-drawer-backdrop,
    .mobile-close-drawer-btn {
      display: none;
    }
    .kpi-summary-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
      gap: 12px;
      margin-bottom: 16px;
    }
    .acc-exec-grid {
      display: grid;
      grid-template-columns: repeat(4, 1fr);
      gap: 6px;
      padding-top: 2px;
      text-align: center;
    }
    .ep-subtabs-bar {
      display: inline-flex;
      gap: 4px;
      background: #111520;
      border: 1px solid #242c3d;
      padding: 4px;
      border-radius: 10px;
      margin-bottom: 18px;
      max-width: 100%;
      overflow-x: auto;
      scrollbar-width: none;
    }
    .ep-subtabs-bar::-webkit-scrollbar { display: none; }
    .ep-subtabs-bar .btn { white-space: nowrap; }
    .ep-url-row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 6px 0;
      gap: 10px;
    }
    .ep-url-label {
      display: flex;
      align-items: center;
      gap: 8px;
      min-width: 95px;
      white-space: nowrap;
    }
    .ep-url-value {
      flex: 1;
      min-width: 0;
      font-family: monospace;
      font-size: 13px;
      font-weight: 700;
      color: #fff;
      background: rgba(8, 11, 16, 0.55);
      border: 1px solid rgba(255, 255, 255, 0.06);
      padding: 5px 10px;
      border-radius: 6px;
      overflow-x: auto;
      white-space: nowrap;
      scrollbar-width: none;
    }
    .ep-url-value::-webkit-scrollbar { display: none; }
    .ep-local-row,
    .ep-tunnel-row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 13px 4px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.06);
      flex-wrap: wrap;
      gap: 10px;
    }
    .responsive-search-3col {
      display: grid;
      grid-template-columns: 260px 1fr auto;
      gap: 10px;
      margin-top: 14px;
      align-items: center;
    }
    .responsive-search-2col {
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 8px;
      margin-top: 12px;
    }
    .responsive-pg-grid {
      display: grid;
      grid-template-columns: 1fr auto auto;
      gap: 8px;
      margin-top: 14px;
      align-items: center;
    }
    @media (max-width: 960px) {
      .app-layout {
        flex-direction: column;
      }
      .desktop-main-topbar {
        display: none !important;
      }
      .mobile-topbar {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 10px 14px;
        background: rgba(13, 17, 23, 0.95);
        backdrop-filter: blur(12px);
        border-bottom: 1px solid var(--border);
        position: sticky;
        top: 0;
        z-index: 250;
      }
      .mobile-hamburger-btn {
        background: #171d2b;
        border: 1px solid #2b354f;
        color: #fff;
        padding: 6px 10px;
        border-radius: 8px;
        font-size: 16px;
        line-height: 1;
      }
      .mobile-quick-pills {
        display: flex;
        gap: 6px;
        padding: 8px 12px;
        background: #0f141f;
        border-bottom: 1px solid var(--border);
        overflow-x: auto;
        scrollbar-width: none;
        -ms-overflow-style: none;
        position: sticky;
        top: 51px;
        z-index: 240;
      }
      .mobile-quick-pills::-webkit-scrollbar {
        display: none;
      }
      .mobile-pill-btn {
        background: #161c2a;
        border: 1px solid #263044;
        color: var(--muted);
        padding: 6px 11px;
        border-radius: 999px;
        font-size: 11.5px;
        font-weight: 600;
        white-space: nowrap;
        flex-shrink: 0;
      }
      .mobile-pill-btn.active {
        background: rgba(56, 189, 248, 0.16);
        color: #38bdf8;
        border-color: rgba(56, 189, 248, 0.45);
      }
      .mobile-drawer-backdrop.open,
      .mobile-drawer-backdrop.show {
        display: block;
        position: fixed;
        inset: 0;
        background: rgba(0, 0, 0, 0.72);
        backdrop-filter: blur(3px);
        z-index: 350;
      }
      .sidebar {
        position: fixed;
        top: 0;
        left: 0;
        bottom: 0;
        width: 285px;
        min-width: 285px;
        max-width: 85vw;
        height: 100dvh;
        z-index: 400;
        transform: translateX(-105%);
        transition: transform 0.22s cubic-bezier(0.16, 1, 0.3, 1);
        border-right: 1px solid var(--border);
      }
      .sidebar.mobile-open {
        transform: translateX(0);
      }
      .mobile-close-drawer-btn {
        display: inline-flex;
        background: transparent;
        border: 1px solid #2b354f;
        color: #cbd5e1;
        padding: 4px 8px;
        border-radius: 6px;
        font-size: 13px;
      }
      .sidebar-nav {
        flex-direction: column;
        overflow-y: auto;
        overflow-x: hidden;
      }
      .main-content {
        padding: 14px 12px 82px 12px;
      }
      .mobile-bottom-nav {
        display: grid;
        grid-template-columns: repeat(5, 1fr);
        position: fixed;
        bottom: 0;
        left: 0;
        right: 0;
        background: rgba(15, 20, 31, 0.97);
        backdrop-filter: blur(12px);
        border-top: 1px solid #263044;
        padding: 6px 4px calc(6px + env(safe-area-inset-bottom));
        z-index: 260;
        box-shadow: 0 -4px 20px rgba(0, 0, 0, 0.5);
      }
      .mobile-bnav-btn {
        background: transparent;
        border: none;
        color: var(--muted);
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        gap: 3px;
        padding: 4px 2px;
        font-size: 10.5px;
        font-weight: 600;
        border-radius: 8px;
      }
      .mobile-bnav-btn:hover {
        background: transparent;
        border: none;
      }
      .mobile-bnav-btn .bnav-icon {
        font-size: 16px;
        line-height: 1.1;
      }
      .mobile-bnav-btn.active {
        color: #38bdf8;
      }
      .grid-2 {
        grid-template-columns: 1fr !important;
        gap: 12px;
      }
      .responsive-search-3col,
      .responsive-search-2col,
      .responsive-pg-grid {
        grid-template-columns: 1fr !important;
      }
      .card {
        padding: 14px 12px !important;
        border-radius: 12px;
        margin-bottom: 14px;
      }
      h2 {
        font-size: 15px !important;
      }
    }
    @media (max-width: 600px) {
      .kpi-summary-grid {
        grid-template-columns: repeat(2, 1fr) !important;
        gap: 8px !important;
      }
      .kpi-summary-grid > div:last-child:nth-child(odd) {
        grid-column: span 2;
      }
      .kpi-summary-grid .setting-card {
        padding: 10px 12px !important;
        margin-bottom: 0 !important;
      }
      .acc-exec-grid {
        grid-template-columns: repeat(2, 1fr) !important;
        gap: 8px !important;
      }
      #quotaCardsGrid,
      #modelsCardsGrid,
      #combosGridBox,
      #saAccountSlotsGrid,
      #saSwarmResultsBox div[style*="grid-template-columns"],
      #saAutomationsListBox > div,
      #saVaultHistoryGrid > div {
        grid-template-columns: 1fr !important;
      }
      .ep-subtabs-bar {
        display: flex !important;
        width: 100%;
        overflow-x: auto;
        scrollbar-width: none;
      }
      .ep-subtabs-bar::-webkit-scrollbar {
        display: none;
      }
      .ep-subtabs-bar .btn {
        flex: 1 0 auto;
        justify-content: center;
        padding: 6px 10px !important;
        font-size: 11.5px !important;
      }
      .ep-url-row {
        gap: 8px;
        padding: 6px 0;
      }
      .ep-url-label {
        min-width: 74px;
        font-weight: 600;
      }
      .ep-url-value {
        font-size: 12px;
        padding: 5px 8px;
      }
      .ep-local-row > div:last-child,
      .ep-tunnel-row > div:last-child {
        width: 100%;
        justify-content: space-between;
      }
      .setting-row {
        flex-wrap: wrap;
      }
      table {
        display: block;
        overflow-x: auto;
        white-space: nowrap;
        -webkit-overflow-scrolling: touch;
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
  <!-- MOBILE STICKY TOP HEADER -->
  <header class="mobile-topbar">
    <div style="display:flex;align-items:center;gap:10px;">
      <button class="mobile-hamburger-btn" onclick="toggleMobileSidebar()" aria-label="Menu">☰</button>
      <div>
        <div style="font-size:15px;font-weight:700;color:#fff;display:flex;align-items:center;gap:6px;">
          <span>OmniRoute-Go</span>
          <span class="badge badge-green" style="font-size:9.5px;padding:2px 6px;">v2.0</span>
        </div>
        <div style="font-size:10.5px;color:#38bdf8;font-family:monospace;">omni.bucloud.vip/v1</div>
      </div>
    </div>
    <div style="display:flex;align-items:center;gap:6px;">
      <span class="badge badge-green" style="font-size:10.5px;padding:3px 7px;">🟢 Online</span>
      <span class="badge" style="background:#1e293b;color:#cbd5e1;cursor:pointer;font-size:10.5px;padding:3px 7px;" onclick="openChangePasswordModal()">🛡️ Admin</span>
    </div>
  </header>

  <!-- MOBILE QUICK HORIZONTAL PILLS (NO SCROLLBAR) -->
  <div class="mobile-quick-pills">
    <button class="mobile-pill-btn active" data-view="view-quota" onclick="switchNav('view-quota')">📊 Hạn Mức</button>
    <button class="mobile-pill-btn" data-view="view-subagents" onclick="switchNav('view-subagents')">🐝 50 Sub-Agents</button>
    <button class="mobile-pill-btn" data-view="view-combos" onclick="switchNav('view-combos')">🔗 Combo</button>
    <button class="mobile-pill-btn" data-view="view-endpoint" onclick="switchNav('view-endpoint')">🎯 Điểm Cuối</button>
    <button class="mobile-pill-btn" data-view="view-models" onclick="switchNav('view-models')">🤖 Mô Hình</button>
    <button class="mobile-pill-btn" data-view="view-providers" onclick="switchNav('view-providers')">🔌 Kết Nối</button>
    <button class="mobile-pill-btn" data-view="view-keys" onclick="switchNav('view-keys')">🔑 API Keys</button>
    <button class="mobile-pill-btn" data-view="view-vectordb" onclick="switchNav('view-vectordb')">🧠 Vector DB</button>
    <button class="mobile-pill-btn" data-view="view-optimization" onclick="switchNav('view-optimization')">🚀 Tối Ưu</button>
    <button class="mobile-pill-btn" data-view="view-compression" onclick="switchNav('view-compression')">⚡ Nén Token</button>
    <button class="mobile-pill-btn" data-view="view-logs" onclick="switchNav('view-logs')">📋 Nhật Ký</button>
    <button class="mobile-pill-btn" data-view="view-playground" onclick="switchNav('view-playground')">🧪 Playground</button>
  </div>

  <!-- MOBILE OFF-CANVAS BACKDROP -->
  <div id="mobileDrawerBackdrop" class="mobile-drawer-backdrop" onclick="closeMobileSidebar()"></div>

  <div class="app-layout">
    <!-- SIDEBAR VERTICAL MENU -->
    <aside id="mainSidebar" class="sidebar">
      <div class="sidebar-header">
        <div style="display:flex;justify-content:space-between;align-items:center;">
          <div class="brand-title">
            <span>OmniRoute-Go</span>
            <span class="badge badge-green" style="font-size:10px;">v2.0</span>
          </div>
          <button class="mobile-close-drawer-btn" onclick="closeMobileSidebar()">✕ Đóng</button>
        </div>
        <div class="brand-sub">
          AI Routing Engine đa nhà cung cấp: Antigravity, Claude Code, Alibaba Token Plan
        </div>
        <div style="margin-top:10px;">
          <span id="sidebarEndpointBadge" class="badge" style="background:#0f172a;color:#38bdf8;border:1px solid #1e293b;font-family:monospace;font-size:11px;">omni.bucloud.vip/v1</span>
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
        <button class="nav-btn" onclick="switchNav('view-combos')">
          <span class="nav-icon">🔗</span>
          <span class="nav-text">Combo Đa Mô Hình</span>
        </button>
        <button class="nav-btn" onclick="switchNav('view-subagents')">
          <span class="nav-icon">🐝</span>
          <span class="nav-text">Sub-Agents Song Song</span>
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
        <button class="nav-btn" onclick="switchNav('view-vectordb')">
          <span class="nav-icon">🧠</span>
          <span class="nav-text">Vector DB (Trí Nhớ AI)</span>
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
      <div class="desktop-main-topbar" style="display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;padding-bottom:12px;border-bottom:1px solid var(--border);">
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
    <div class="kpi-summary-grid">
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
      <div class="setting-card" style="padding:14px;border-left:3px solid #c084fc;">
        <div style="font-size:11px;color:#c084fc;font-weight:600;display:flex;align-items:center;gap:6px;">
          <span>💰</span> GIÁ TRỊ API ƯỚC TÍNH
        </div>
        <div id="kpiTotalValueUSD" style="font-size:24px;font-weight:700;color:#c084fc;margin-top:4px;">$0.00</div>
      </div>
    </div>

    <!-- Filter Pills & Action Row -->
    <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;margin-bottom:12px;">
      <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;">
        <span style="font-size:11.5px;color:var(--muted);font-weight:600;">HẠNG:</span>
        <button id="pillTierAll" class="btn" style="padding:4px 9px;font-size:11px;background:#242d3d;color:#fff;" onclick="filterQuotaTier('all')">Tất cả</button>
        <button id="pillTierUltra" class="btn" style="padding:4px 9px;font-size:11px;" onclick="filterQuotaTier('Ultra')">Ultra</button>
        <button id="pillTierPro" class="btn" style="padding:4px 9px;font-size:11px;" onclick="filterQuotaTier('Pro')">Pro</button>
        <button id="pillTierLite" class="btn" style="padding:4px 9px;font-size:11px;" onclick="filterQuotaTier('Lite')">Lite</button>
      </div>
      <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;">
        <button onclick="resetAccountLatency('all')" title="Xoá thống kê độ trễ trung bình cũ từ các bài viết dài để đo lại từ đầu" style="border-color:rgba(245,158,11,0.4);color:#fbbf24;">⚡ Reset Độ Trễ Cũ</button>
        <button onclick="loadQuotaSummary(true)">🔄 Làm mới tất cả</button>
      </div>
    </div>

    <!-- BẢNG GIÁM SÁT THỜI GIAN THỰC: ĐANG CHẠY TÀI KHOẢN NÀO & MODEL NÀO -->
    <div id="liveActiveMonitorBox" class="setting-card" style="padding:12px 14px;margin-bottom:14px;border:1px solid #2b354f;background:#0e131f;">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;margin-bottom:8px;">
        <div style="display:flex;align-items:center;gap:8px;">
          <span style="font-weight:700;font-size:13px;color:#38bdf8;">📡 Giám Sát Thời Gian Thực: Tài Khoản &amp; Model Đang Chạy / Vừa Chạy</span>
          <span id="liveActiveCountBadge" class="badge badge-green">🟢 0 Request đang chạy</span>
        </div>
        <button style="padding:3px 9px;font-size:11px;" onclick="switchNav('view-logs')">📋 Xem toàn bộ Nhật ký</button>
      </div>
      <div id="liveInFlightList" style="font-size:12px;color:#cbd5e1;margin-bottom:8px;">
        Đang kiểm tra trạng thái thực thi...
      </div>
      <div id="liveRecentRequestsMini" style="border-top:1px solid rgba(255,255,255,0.06);padding-top:8px;font-size:11.5px;color:var(--muted);"></div>
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
          <input id="modelSearchInput" type="text" placeholder="🔍 Lọc mô hình (carbon, argon, image, opus, qwen)..." oninput="filterModelsGrid()" style="padding:6px 12px;font-size:12px;" />
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
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;">
        <h2>⚙️ Cơ chế Điều Phối (Routing Strategy) & Circuit Breaker</h2>
        <span id="activeStrategyBadge" class="badge badge-antigravity">round-robin</span>
      </div>
      <div style="display:flex;gap:10px;align-items:center;margin-top:6px;flex-wrap:wrap;">
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
      <div style="display:flex;gap:8px;margin-bottom:14px;border-bottom:1px solid var(--border);padding-bottom:8px;flex-wrap:wrap;">
        <button class="btn" style="border:none;" onclick="switchProvTab('tab-antigravity')">1. Google Antigravity</button>
        <button class="btn" style="border:none;" onclick="switchProvTab('tab-claude')">2. Claude Code</button>
        <button class="btn" style="border:none;" onclick="switchProvTab('tab-alibaba')">3. Alibaba Token Plan</button>
      </div>

      <!-- Tab Antigravity -->
      <div id="tab-antigravity" class="prov-tab">
        <p class="hint" style="margin:0 0 10px 0;">
          Hỗ trợ Gemini 3 Flash/Pro, Gemini 4 Carbon/Argon, <strong>Gemini 3 Pro Image (Imagen 3)</strong> và Claude 4.5/4.6 trên Antigravity CCPA.
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

    <!-- Connected Accounts List in Providers View -->
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;flex-wrap:wrap;gap:8px;">
        <div>
          <h2>📋 Danh Sách Tài Khoản Đã Kết Nối Trong Pool</h2>
          <div style="font-size:12px;color:var(--muted);">Hiển thị đầy đủ email và trạng thái tất cả tài khoản đang hoạt động trên Gateway</div>
        </div>
        <button onclick="loadProvidersAndAccounts()">🔄 Làm mới danh sách</button>
      </div>
      <div id="providersAccountsListBox">Đang tải danh sách tài khoản...</div>
    </div>
  </div>

  <!-- VIEW 4: API KEYS MANAGEMENT & POSTGRESQL -->
  <div id="view-keys" class="view-content">
    <!-- PostgreSQL Database Card -->
    <div class="card" style="border:1px solid #263147;background:#121722;">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>🐘 Cơ Sở Dữ Liệu PostgreSQL (Lưu Trữ Khóa API &amp; Lịch Sử Prompt Nhân Viên)</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Lưu trữ bền vững toàn bộ API Key của nhân viên, số lượng token, nội dung Prompt câu hỏi và câu trả lời AI vào PostgreSQL.
          </div>
        </div>
        <span id="pgStatusBadge" class="badge badge-yellow">🟡 Local JSON Mode</span>
      </div>

      <div class="responsive-pg-grid" style="margin-top:14px;">
        <input id="pgDsnInput" type="text" placeholder="postgres://omniroute:omniroute_secret@omni-postgres:5432/omniroute?sslmode=disable" style="font-family:monospace;font-size:12.5px;" />
        <button onclick="fillDefaultDockerPostgres()" title="Điền chuỗi kết nối PostgreSQL 18 mặc định trong docker-compose.yml">🐳 Docker DSN</button>
        <button class="btn-primary" onclick="savePostgresConfig()">🔌 Lưu &amp; Kết Nối PostgreSQL</button>
      </div>
      <div id="pgStatusDetail" class="hint" style="margin-top:8px;">
        Khi chạy trên VPS bằng <code>docker compose up -d</code>, container PostgreSQL 18 (<code>pgvector:pg18</code>) sẽ tự động kết nối và tạo bảng <code>omni_api_keys</code>, <code>omni_prompt_logs</code> &amp; <code>omni_vector_memories</code>.
      </div>
    </div>

    <!-- Employee API Keys Card -->
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>🔑 Quản Lý Khóa API Nhân Viên (Employee API Keys)</h2>
          <div style="font-size:12.5px;color:var(--muted);">Cấp cho mỗi nhân viên 1 mã API Key riêng (ví dụ: <code>Nguyễn Văn A - Frontend</code>) để làm việc và nhấn <strong>"🔍 Xem Prompt đã dùng"</strong> để hỗ trợ kỹ thuật.</div>
        </div>
        <button class="btn-purple" onclick="openCreateKeyModal()">+ Cấp Khóa API Nhân Viên</button>
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

  <!-- VIEW 6: ERROR & CONNECTION LOGS + EMPLOYEE PROMPT AUDIT -->
  <div id="view-logs" class="view-content">
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>📋 Nhật Ký Kết Nối &amp; Giám Sát Prompt Nhân Viên (PostgreSQL Audit)</h2>
          <div style="font-size:12.5px;color:var(--muted);">Kiểm tra chi tiết từng nhân viên đã gửi Prompt gì, dùng Model nào, tốn bao nhiêu Token và câu trả lời AI để hỗ trợ tốt hơn.</div>
        </div>
        <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
          <span id="logPgModeBadge" class="badge badge-antigravity">Local / PG</span>
          <label style="font-size:12px;color:var(--muted);display:flex;align-items:center;gap:5px;cursor:pointer;">
            <input type="checkbox" id="autoRefreshLogsCheckbox" onchange="toggleAutoRefreshLogs(this.checked)" checked /> Tự động làm mới (2s)
          </label>
          <button onclick="loadLogs()">🔄 Làm Mới</button>
          <button onclick="clearLogs()" style="color:#f87171;">🗑️ Xoá Bộ Đệm</button>
        </div>
      </div>

      <!-- Employee & Prompt Search Bar -->
      <div class="responsive-search-3col" style="margin-top:14px;">
        <select id="logEmployeeFilterSelect" onchange="loadLogs()">
          <option value="all">👥 Tất cả Nhân viên / API Key</option>
        </select>
        <input id="logPromptSearchInput" type="text" placeholder="🔎 Tìm theo nội dung Prompt nhân viên đã hỏi, câu trả lời hoặc tên model..." oninput="debouncedLoadLogs()" />
        <button onclick="resetEmployeeLogFilter()">Đặt lại bộ lọc</button>
      </div>

      <!-- Provider Filters -->
      <div style="display:flex;gap:8px;margin-top:10px;flex-wrap:wrap;">
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
              <option value="gemini-4-carbon-high">gemini-4-carbon-high (Carbon 4 High Thinking)</option>
              <option value="gemini-4-carbon-medium">gemini-4-carbon-medium</option>
              <option value="gemini-4-argon-high">gemini-4-argon-high (Argon High Thinking)</option>
              <option value="gemini-4-argon-medium">gemini-4-argon-medium</option>
              <option value="gemini-3.1-pro-high">gemini-3.1-pro-high (Gemini 3.1 Pro High)</option>
              <option value="gemini-3.1-pro-low">gemini-3.1-pro-low (Gemini 3.1 Pro Low)</option>
              <option value="gemini-3.8-flash-high">gemini-3.8-flash-high</option>
              <option value="antigravity/claude-opus-4-6-thinking">antigravity/claude-opus-4-6-thinking</option>
            </optgroup>
            <optgroup label="🔗 Combo Đa Mô Hình (Pipeline, Hội Đồng MoA &amp; Night Discount)">
              <option value="auto/seo-master">auto/seo-master (👑 Quản Lý SEO: Carbon 4 + Argon 4 + Opus 5.5 ➔ Gemini 3.1 Pro / 3.8 Flash)</option>
              <option value="auto/team-coding">auto/team-coding (🔗 Pipeline: Opus 5.5 Kiến trúc ➔ Carbon 4 / Argon 4 Code ➔ Qwen 3.8 Max)</option>
              <option value="auto/council-pro">auto/council-pro (🧠 Hội Đồng Song Song: Carbon 4 + Argon 4 + Opus 5.5 + Qwen 3.8 Max)</option>
              <option value="auto/night-coding">auto/night-coding (⚡ Night 40-50% Off: Qwen 3.8 Max ➔ DeepSeek V4 Pro ➔ Carbon 4 / Argon 4)</option>
              <option value="auto/best-coding">auto/best-coding (🛡️ Failover: Carbon 4 &amp; Argon 4 ➔ Claude Opus ➔ Qwen3 Coder)</option>
              <option value="auto/reasoning">auto/reasoning (🔗 Suy Luận Đa Tầng: Carbon 4 ➔ Claude Opus Thinking ➔ DeepSeek R1)</option>
              <option value="auto/fast">auto/fast (⚡ Tốc Độ Nhanh: Carbon 4 Low ➔ Gemini 3.8 Flash Low ➔ Qwen 3.8 Flash ➔ Haiku 5.5)</option>
            </optgroup>
            <optgroup label="Claude Code (Anthropic CLI &amp; Pro/Max)">
              <option value="claude-code/claude-opus-5-5">claude-code/claude-opus-5-5 (Opus 5.5 Default)</option>
              <option value="claude-code/claude-fable-5-1">claude-code/claude-fable-5-1 (Fable 5.1)</option>
              <option value="claude-code/claude-sonnet-5-5">claude-code/claude-sonnet-5-5 (Sonnet 5.5)</option>
              <option value="claude-code/claude-haiku-5-5">claude-code/claude-haiku-5-5 (Haiku 5.5)</option>
              <option value="claude-code/claude-sonnet-5">claude-code/claude-sonnet-5 (Sonnet 5)</option>
              <option value="claude-code/claude-opus-5">claude-code/claude-opus-5 (Opus 5)</option>
              <option value="claude-code/claude-fable-5">claude-code/claude-fable-5 (Fable 5)</option>
              <option value="claude-code/claude-opus-4-8">claude-code/claude-opus-4-8 (Opus 4.8)</option>
              <option value="claude-code/claude-opus-4-7">claude-code/claude-opus-4-7 (Opus 4.7)</option>
              <option value="claude-code/claude-opus-4-6-thinking">claude-code/claude-opus-4-6-thinking (Opus 4.6 Thinking)</option>
              <option value="claude-code/claude-sonnet-4-6">claude-code/claude-sonnet-4-6 (Sonnet 4.6)</option>
              <option value="claude-code/claude-haiku-4-5">claude-code/claude-haiku-4-5 (Haiku 4.5)</option>
            </optgroup>
            <optgroup label="Alibaba Cloud Token Plan (⚡ Night Discount)">
              <option value="alibaba/auto">alibaba/auto (Qwen Auto Reasoning)</option>
              <option value="alibaba/qwen3.8-max">alibaba/qwen3.8-max (⚡ 40% Off Nights)</option>
              <option value="alibaba/qwen3.8-flash">alibaba/qwen3.8-flash (⚡ 40% Off Nights)</option>
              <option value="alibaba/qwen3.7-plus">alibaba/qwen3.7-plus</option>
              <option value="alibaba/qwen3.7-max">alibaba/qwen3.7-max</option>
              <option value="alibaba/qwen3.6-flash">alibaba/qwen3.6-flash</option>
              <option value="alibaba/deepseek-v4.1-flash-1017">alibaba/deepseek-v4.1-flash-1017 (⚡ 50% Off Nights)</option>
              <option value="alibaba/deepseek-v4.1-flash">alibaba/deepseek-v4.1-flash (⚡ 50% Off Nights)</option>
              <option value="alibaba/deepseek-v4-pro-0813">alibaba/deepseek-v4-pro-0813 (⚡ Night 50% Off)</option>
              <option value="alibaba/deepseek-v4-pro">alibaba/deepseek-v4-pro</option>
              <option value="alibaba/deepseek-v4-flash-0731">alibaba/deepseek-v4-flash-0731 (⚡ Night 50% Off)</option>
              <option value="alibaba/glm-5.3">alibaba/glm-5.3</option>
              <option value="alibaba/glm-5.2">alibaba/glm-5.2</option>
              <option value="alibaba/qwen3-coder-plus">alibaba/qwen3-coder-plus</option>
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
    <div class="ep-subtabs-bar" style="display:inline-flex;gap:4px;background:#111520;border:1px solid #242c3d;padding:4px;border-radius:10px;margin-bottom:18px;max-width:100%;">
      <button id="epSubBtn-api" class="btn" style="padding:6px 12px;font-size:12px;background:#1e2638;color:#fff;border-color:#334155;white-space:nowrap;" onclick="switchEndpointSubTab('api')">🌐 API</button>
      <button id="epSubBtn-mcp" class="btn" style="padding:6px 12px;font-size:12px;background:transparent;border-color:transparent;color:var(--muted);white-space:nowrap;" onclick="switchEndpointSubTab('mcp')">🔌 MCP</button>
      <button id="epSubBtn-a2a" class="btn" style="padding:6px 12px;font-size:12px;background:transparent;border-color:transparent;color:var(--muted);white-space:nowrap;" onclick="switchEndpointSubTab('a2a')">🕸️ A2A</button>
      <button id="epSubBtn-ctx" class="btn" style="padding:6px 12px;font-size:12px;background:transparent;border-color:transparent;color:var(--muted);white-space:nowrap;" onclick="switchEndpointSubTab('ctx')">🗄️ Nguồn ngữ cảnh &amp; VPS</button>
    </div>

    <!-- SUB-TAB 1: API ENDPOINT & TUNNELS -->
    <div id="epSubTab-api" class="ep-subtab">
      <div class="card" style="background:#121620;border:1px solid #232a3b;padding:18px;">
        <h2 style="font-size:16px;font-weight:700;color:#fff;margin-bottom:14px;">Endpoint API</h2>

        <!-- ENDPOINT ĐANG HOẠT ĐỘNG BOX -->
        <div style="background:rgba(127, 29, 29, 0.12);border:1px solid rgba(239, 68, 68, 0.28);border-radius:10px;padding:13px 14px;margin-bottom:16px;">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px;flex-wrap:wrap;gap:8px;">
            <span style="font-size:10.5px;font-weight:700;letter-spacing:0.06em;color:#f87171;text-transform:uppercase;">ENDPOINT ĐANG HOẠT ĐỘNG</span>
            <button style="padding:4px 10px;font-size:11px;background:#1a202e;white-space:nowrap;" onclick="editPublicDomainPrompt()">✏️ Đổi tên miền VPS</button>
          </div>

          <!-- Công khai -->
          <div class="ep-url-row">
            <div class="ep-url-label">
              <span style="width:7px;height:7px;border-radius:50%;background:#10b981;display:inline-block;flex-shrink:0;"></span>
              <span style="font-size:12.5px;color:#cbd5e1;">Công khai</span>
            </div>
            <div class="ep-url-value" id="epActivePublicURL">https://omni.bucloud.vip/v1</div>
            <button title="Sao chép Endpoint Công khai" style="padding:5px 9px;font-size:12px;flex-shrink:0;" onclick="copyEndpointField('epActivePublicURL')">📋</button>
          </div>

          <!-- Cục bộ -->
          <div class="ep-url-row">
            <div class="ep-url-label">
              <span style="width:7px;height:7px;border-radius:50%;background:#10b981;display:inline-block;flex-shrink:0;"></span>
              <span style="font-size:12.5px;color:#cbd5e1;">Cục bộ</span>
            </div>
            <div class="ep-url-value" id="epActiveLocalURL">http://localhost:8787/v1</div>
            <button title="Sao chép Endpoint Cục bộ" style="padding:5px 9px;font-size:12px;flex-shrink:0;" onclick="copyEndpointField('epActiveLocalURL')">📋</button>
          </div>
        </div>

        <!-- MÁY CHỦ CỤC BỘ ROW -->
        <div class="ep-local-row" style="display:flex;justify-content:space-between;align-items:center;padding:12px 4px;border-bottom:1px solid rgba(255,255,255,0.06);margin-bottom:16px;flex-wrap:wrap;gap:10px;">
          <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
            <span style="color:#10b981;font-size:16px;">🖥️</span>
            <span style="font-weight:700;font-size:13.5px;color:#fff;white-space:nowrap;">Máy chủ cục bộ</span>
            <span id="epHostID" style="font-family:monospace;font-size:12px;color:#64748b;background:rgba(255,255,255,0.04);padding:2px 6px;border-radius:5px;">569fb55e</span>
            <span id="epLanServerURL" style="font-family:monospace;font-size:12.5px;color:#94a3b8;white-space:nowrap;">172.17.0.2:8787/v1</span>
            <button title="Sao chép IP máy chủ" style="padding:2px 6px;font-size:11px;background:transparent;border:none;color:#94a3b8;" onclick="copyEndpointField('epLanServerURL')">📋</button>
          </div>
          <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
            <span class="badge badge-green" style="padding:4px 10px;white-space:nowrap;">● Đang chạy</span>
            <button style="padding:6px 12px;font-size:12px;white-space:nowrap;" onclick="copyEndpointField('epLanServerURL')">📋 Sao chép</button>
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
        <div class="ep-tunnel-row" style="display:flex;justify-content:space-between;align-items:center;padding:13px 4px;border-bottom:1px solid rgba(255,255,255,0.05);flex-wrap:wrap;gap:10px;">
          <div style="display:flex;align-items:center;gap:10px;">
            <span style="color:#38bdf8;font-size:16px;">☁️</span>
            <div>
              <div style="font-weight:600;font-size:13.5px;color:#f1f5f9;">Cloud OmniRoute</div>
              <div id="epCloudSubUrl" style="font-size:11px;color:var(--muted);font-family:monospace;word-break:break-all;">https://omni.bucloud.vip/v1</div>
            </div>
          </div>
          <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
            <span id="epBadgeCloud" class="badge badge-green">● Đang hoạt động</span>
            <button id="epBtnCloud" style="padding:6px 12px;font-size:12px;background:rgba(239,68,68,0.14);color:#f87171;border-color:rgba(239,68,68,0.35);white-space:nowrap;" onclick="toggleEndpointTunnel('cloud')">☁️ Tắt đám mây</button>
          </div>
        </div>

        <!-- 2. Cloudflare Quick Tunnel -->
        <div class="ep-tunnel-row" style="display:flex;justify-content:space-between;align-items:center;padding:13px 4px;border-bottom:1px solid rgba(255,255,255,0.05);flex-wrap:wrap;gap:10px;">
          <div style="display:flex;align-items:center;gap:10px;">
            <span style="color:#f59e0b;font-size:16px;">🟠</span>
            <div>
              <div style="font-weight:600;font-size:13.5px;color:#f1f5f9;">Cloudflare Quick Tunnel</div>
              <div id="epCfSubUrl" style="font-size:11px;color:var(--muted);font-family:monospace;">Tự động cấp phát tên miền *.trycloudflare.com miễn phí</div>
            </div>
          </div>
          <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
            <span id="epBadgeCloudflare" class="badge" style="background:#1e293b;color:#94a3b8;">Đã đóng</span>
            <button id="epBtnCloudflare" style="padding:6px 12px;font-size:12px;background:linear-gradient(90deg,#ec4899,#8b5cf6);border:none;color:#fff;white-space:nowrap;" onclick="toggleEndpointTunnel('cloudflare')">☁️ Bật Tunnel</button>
          </div>
        </div>

        <!-- 3. Tailscale Funnel -->
        <div class="ep-tunnel-row" style="display:flex;justify-content:space-between;align-items:center;padding:13px 4px;border-bottom:1px solid rgba(255,255,255,0.05);flex-wrap:wrap;gap:10px;">
          <div style="display:flex;align-items:center;gap:10px;">
            <span style="color:#818cf8;font-size:16px;">🟣</span>
            <div>
              <div style="font-weight:600;font-size:13.5px;color:#f1f5f9;">Tailscale Funnel</div>
              <div id="epTsSubUrl" style="font-size:11px;color:var(--muted);font-family:monospace;"> Chia sẻ bảo mật qua mạng lưới Tailscale Funnel (*.ts.net)</div>
            </div>
          </div>
          <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
            <span id="epBadgeTailscale" class="badge" style="background:#1e293b;color:#94a3b8;">Chưa cài đặt</span>
            <button id="epBtnTailscale" style="padding:6px 12px;font-size:12px;background:linear-gradient(90deg,#ec4899,#8b5cf6);border:none;color:#fff;white-space:nowrap;" onclick="toggleEndpointTunnel('tailscale')">🚀 Cài đặt &amp; Bật</button>
          </div>
        </div>

        <!-- 4. Đường hầm ngrok -->
        <div style="padding:13px 4px;border-bottom:1px solid rgba(255,255,255,0.06);">
          <div class="ep-tunnel-row" style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
            <div style="display:flex;align-items:center;gap:10px;">
              <span style="color:#a855f7;font-size:16px;">🟣</span>
              <div>
                <div style="font-weight:600;font-size:13.5px;color:#f1f5f9;">Đường hầm ngrok</div>
                <div id="epNgrokSubUrl" style="font-size:11px;color:var(--muted);font-family:monospace;">Cổng public tốc độ cao qua ngrok</div>
              </div>
            </div>
            <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">
              <span id="epBadgeNgrok" class="badge" style="background:rgba(245,158,11,0.15);color:#fbbf24;border:1px solid rgba(245,158,11,0.3);">Cần xác thực</span>
              <button id="epBtnNgrok" style="padding:6px 12px;font-size:12px;background:linear-gradient(90deg,#ec4899,#8b5cf6);border:none;color:#fff;white-space:nowrap;" onclick="toggleEndpointTunnel('ngrok')">🌐 Bật đường hầm</button>
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
        "OMNI_GATEWAY_URL": "https://omni.bucloud.vip/v1/chat/completions",
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
              <div class="setting-desc" id="epA2ADiscoveryURL" style="font-family:monospace;color:#38bdf8;">https://omni.bucloud.vip/.well-known/agent.json</div>
            </div>
            <button onclick="copyEndpointField('epA2ADiscoveryURL')">📋 Sao chép</button>
          </div>
          <div class="setting-row">
            <div class="setting-info">
              <div class="setting-title">A2A Task Dispatch Endpoint</div>
              <div class="setting-desc" id="epA2ADispatchURL" style="font-family:monospace;color:#34d399;">https://omni.bucloud.vip/v1/a2a</div>
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
        <pre id="epVpsDockerCmd">git clone https://github.com/danlaptrinh/omniroute-go.git &amp;&amp; cd omniroute-go
docker compose up -d --build</pre>
        <div style="font-size:12px;font-weight:600;color:#e2e8f0;margin:12px 0 6px;">2. Cấu hình Nginx Reverse Proxy cho tên miền công khai (SSE Streaming chuẩn):</div>
        <pre id="epVpsNginxConf">server {
    listen 80;
    server_name omni.bucloud.vip;

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

  <!-- VIEW 10: VECTOR DATABASE & LONG-TERM AI MEMORY -->
  <div id="view-vectordb" class="view-content">
    <div class="card" style="border:1px solid #2b354f;background:#121723;">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>🧠 Cơ Sở Dữ Liệu Vector &amp; Trí Nhớ Dài Hạn AI (PGVector + Hybrid Semantic RAG)</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Giúp AI càng ngày càng thông minh hơn và ghi nhớ lâu hơn: tự động học từ lịch sử giải quyết vấn đề của nhân viên &amp; tài liệu dự án, sau đó tự động truy xuất ngữ cảnh tương đồng Cosine (Semantic RAG) bơm vào mỗi câu hỏi mới.
          </div>
        </div>
        <span id="vecEngineBadge" class="badge badge-green">🟢 256-dim Cosine Vector DB</span>
      </div>

      <!-- KPI Cards -->
      <div class="kpi-summary-grid" style="margin-top:16px;margin-bottom:0;">
        <div class="setting-card" style="padding:14px;border-left:3px solid #38bdf8;">
          <div style="font-size:11px;color:var(--muted);font-weight:600;">TỔNG VECTOR GHI NHỚ</div>
          <div id="vecStatTotal" style="font-size:25px;font-weight:700;color:#f1f5f9;margin-top:4px;">0</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #a855f7;">
          <div style="font-size:11px;color:#c084fc;font-weight:600;">📌 TRI THỨC CỐ ĐỊNH (PINNED)</div>
          <div id="vecStatPinned" style="font-size:25px;font-weight:700;color:#c084fc;margin-top:4px;">0</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #10b981;">
          <div style="font-size:11px;color:#34d399;font-weight:600;">🤖 TỰ HỌC TỪ NHÂN VIÊN</div>
          <div id="vecStatAuto" style="font-size:25px;font-weight:700;color:#10b981;margin-top:4px;">0</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #f59e0b;">
          <div style="font-size:11px;color:#fbbf24;font-weight:600;">⚡ LƯỢT BƠM NGỮ CẢNH RAG</div>
          <div id="vecStatRagHits" style="font-size:25px;font-weight:700;color:#fbbf24;margin-top:4px;">0</div>
        </div>
      </div>

      <!-- Cấu hình Auto-Learn & Auto-RAG -->
      <div class="setting-card" style="margin-top:16px;">
        <div class="setting-row">
          <div class="setting-info">
            <div class="setting-title">Tự động học từ Prompt &amp; Câu trả lời của Nhân viên (Auto-Learn Memory)</div>
            <div class="setting-desc">Mỗi khi nhân viên hỏi và nhận được câu trả lời thành công, hệ thống tự động mã hóa Vector 256 chiều và lưu vào bảng <code>omni_vector_memories</code> (tự động chống trùng lặp nếu độ tương đồng &gt; 93%).</div>
          </div>
          <button id="toggleVecAutoLearnBtn" class="toggle-btn on" onclick="toggleSetting('toggleVecAutoLearnBtn')">
            🧠 <span>Đã bật</span>
          </button>
        </div>

        <div class="setting-row">
          <div class="setting-info">
            <div class="setting-title">Tự động truy xuất Vector &amp; Bơm ngữ cảnh RAG thông minh (Auto-RAG Injection)</div>
            <div class="setting-desc">Khi bất kỳ nhân viên nào đặt câu hỏi liên quan đến vấn đề từng giải quyết hoặc tài liệu dự án đã ghim, Gateway tự động tìm Top-K vector gần nhất và đính kèm vào System Context để AI trả lời chuẩn xác ngay lập tức.</div>
          </div>
          <button id="toggleVecAutoRagBtn" class="toggle-btn on" onclick="toggleSetting('toggleVecAutoRagBtn')">
            🚀 <span>Đã bật</span>
          </button>
        </div>

        <div style="margin-top:12px;padding-top:12px;border-top:1px solid rgba(255,255,255,0.06);display:flex;flex-wrap:wrap;align-items:center;gap:16px;">
          <div style="display:flex;align-items:center;gap:8px;">
            <span style="font-size:12.5px;color:#e2e8f0;font-weight:600;">Số Vector lấy ra (Top-K):</span>
            <input id="vecTopKInput" type="number" value="3" min="1" max="10" style="width:80px;padding:6px 10px;" />
          </div>
          <div style="display:flex;align-items:center;gap:8px;">
            <span style="font-size:12.5px;color:#e2e8f0;font-weight:600;">Ngưỡng tương đồng Cosine (0.10 - 0.90):</span>
            <input id="vecMinSimInput" type="number" value="0.24" min="0.05" max="0.95" step="0.02" style="width:95px;padding:6px 10px;" />
          </div>
          <button class="btn-primary" onclick="saveVectorDBConfigUI()">💾 Lưu Cấu Hình Vector DB</button>
          <span id="vecConfigMsg" class="hint"></span>
        </div>
      </div>
    </div>

    <!-- Nạp Tri Thức Cốt Lõi / Tài Liệu Dự Án -->
    <div class="card">
      <h2>📌 Nạp Tri Thức Cốt Lõi &amp; Tài Liệu Dự Án (Pinned Knowledge Base)</h2>
      <div style="font-size:12.5px;color:var(--muted);margin-bottom:12px;">
        Thêm kiến thức dự án (ví dụ: kiến trúc server BuCloud, quy chuẩn đặt tên biến, cấu hình Docker/DB nội bộ...) để toàn bộ nhân viên khi hỏi AI đều được AI ghi nhớ và áp dụng chính xác.
      </div>
      <div class="grid-2">
        <div>
          <label class="form-label">Tiêu đề Tri thức / Chủ đề</label>
          <input id="vecAddTitle" type="text" placeholder="Ví dụ: Quy chuẩn triển khai Docker &amp; PostgreSQL dự án BuCloud" />
        </div>
        <div>
          <label class="form-label">Danh mục (Category)</label>
          <select id="vecAddCategory">
            <option value="knowledge">📌 Tri thức cốt lõi / Tài liệu công ty</option>
            <option value="bucloud">☁️ Dự án BuCloud / Hạ tầng VPS</option>
            <option value="coding">💻 Quy chuẩn Code &amp; Kiến trúc</option>
          </select>
        </div>
      </div>
      <div class="grid-2" style="margin-top:10px;">
        <div>
          <label class="form-label">Câu hỏi / Từ khóa tình huống nhận diện</label>
          <textarea id="vecAddPrompt" rows="3" placeholder="Ví dụ: Khi nhân viên hỏi về cách deploy BuCloud, cổng kết nối DB hoặc lỗi kết nối API..."></textarea>
        </div>
        <div>
          <label class="form-label">Kiến thức chuẩn / Giải pháp AI cần ghi nhớ</label>
          <textarea id="vecAddAnswer" rows="3" placeholder="Ví dụ: Hệ thống sử dụng PostgreSQL 18 (pgvector), cổng 8787 cho AI Gateway, xác thực bằng Bearer sk-omni-..."></textarea>
        </div>
      </div>
      <div class="btn-row" style="justify-content:flex-end;margin-top:10px;">
        <button class="btn-purple" onclick="addPinnedVectorMemory()">+ Lưu Vào Vector Database (Ghim Cố Định)</button>
      </div>
    </div>

    <!-- Tìm kiếm Ngữ nghĩa Vector & Danh sách Trí nhớ -->
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>🔎 Tìm Kiếm Ngữ Nghĩa Vector (Cosine Similarity Search) &amp; Kho Trí Nhớ</h2>
          <div style="font-size:12.5px;color:var(--muted);">Thử nhập một câu hỏi để xem Vector Database tính toán điểm tương đồng Cosine (%) và trích xuất ký ức như thế nào.</div>
        </div>
        <div style="display:flex;gap:8px;">
          <button onclick="loadVectorDB()">🔄 Làm mới</button>
          <button onclick="clearAutoVectorMemories()" style="color:#f87171;">🗑️ Xoá Vector Tự Học (Giữ lại Ghim)</button>
        </div>
      </div>

      <div class="responsive-search-2col" style="margin-top:12px;">
        <input id="vecSearchQueryInput" type="text" placeholder="🔎 Nhập câu hỏi hoặc chủ đề để tìm kiếm Vector tương đồng Cosine..." onkeydown="if(event.key==='Enter')searchVectorMemoriesUI()" />
        <button class="btn-primary" onclick="searchVectorMemoriesUI()">🔍 Tìm Kiếm Vector</button>
      </div>

      <div id="vectorMemoriesListBox" style="margin-top:14px;">Đang tải dữ liệu Vector Database...</div>
    </div>
  </div>

  <!-- VIEW 11: MULTI-MODEL COMBOS & COLLABORATIVE PIPELINE BUILDER -->
  <div id="view-combos" class="view-content">
    <div class="card" style="border:1px solid #2b354f;background:#121723;">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:12px;">
        <div>
          <h2>🔗 Combo Đa Mô Hình &amp; Phối Hợp Làm Việc (Multi-Model Collaborative Engine)</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Kết nối nhiều model từ <strong>Google Antigravity</strong>, <strong>Claude Code (Opus 5.5 / Fable 5.1)</strong> và <strong>Alibaba Cloud Token Plan (⚡ Night Discount)</strong> để phối hợp làm việc cùng nhau theo dây chuyền (Pipeline), Hội đồng song song (Mixture of Agents), hoặc Tối ưu giảm phí ban đêm.
          </div>
        </div>
        <div style="display:flex;gap:8px;align-items:center;">
          <span class="badge badge-green">🟢 4 Chế Độ Phối Hợp</span>
          <button onclick="loadCombosView()">🔄 Làm mới</button>
        </div>
      </div>

      <!-- 4 Modes Explanation Cards -->
      <div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(220px, 1fr));gap:12px;margin-top:16px;">
        <div class="setting-card" style="padding:13px;border-left:3px solid #a855f7;">
          <div style="font-weight:700;font-size:13px;color:#c084fc;">🔗 1. Pipeline Nối Tiếp (Kiến Trúc ➔ Code)</div>
          <div style="font-size:11.5px;color:var(--muted);margin-top:4px;">Model Bước 1 (vd: <code>Claude Opus 5.5</code>) lập kiến trúc &amp; thuật toán ➔ truyền kết quả sang Model Bước 2 (vd: <code>Gemini 4 Carbon / Argon</code> / <code>Qwen 3.8 Max</code>) viết code hoàn chỉnh.</div>
        </div>
        <div class="setting-card" style="padding:13px;border-left:3px solid #38bdf8;">
          <div style="font-weight:700;font-size:13px;color:#38bdf8;">🧠 2. Hội Đồng Song Song (Mixture of Agents)</div>
          <div style="font-size:11.5px;color:var(--muted);margin-top:4px;">Chạy đồng thời các model bằng Goroutines (<code>Carbon 4</code> + <code>Argon 4</code> + <code>Opus 5.5</code> + <code>Qwen 3.8 Max</code>) ➔ Model Chủ tịch tổng hợp câu trả lời xuất sắc nhất.</div>
        </div>
        <div class="setting-card" style="padding:13px;border-left:3px solid #10b981;">
          <div style="font-weight:700;font-size:13px;color:#34d399;">⚡ 3. Tối Ưu Ban Đêm (Night 40-50% Off)</div>
          <div style="font-size:11.5px;color:var(--muted);margin-top:4px;">Ưu tiên các model giảm giá mạnh ban đêm của Alibaba (<code>Qwen 3.8 Max</code> ⚡40%, <code>DeepSeek V4.1 Flash</code> ⚡50%, <code>DeepSeek V4 Pro</code> ⚡50%) + dự phòng <code>Carbon 4 / Argon 4</code>.</div>
        </div>
        <div class="setting-card" style="padding:13px;border-left:3px solid #f59e0b;">
          <div style="font-weight:700;font-size:13px;color:#fbbf24;">🛡️ 4. Failover Chuyển Tiếp Tức Thì</div>
          <div style="font-size:11.5px;color:var(--muted);margin-top:4px;">Chạy Model số 1 ưu tiên (vd: <code>Carbon 4</code>); ngay khi hết hạn mức hoặc quá tải sẽ tự động chuyển sang Model 2 ➔ Model 3 xuyên suốt 3 nhà cung cấp.</div>
        </div>
      </div>
    </div>

    <!-- Active Combos Grid -->
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:14px;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>📦 Danh Sách Các Combo Đa Mô Hình Đang Hoạt Động</h2>
          <div style="font-size:12px;color:var(--muted);">Sử dụng trực tiếp tên ID Combo (ví dụ: <code>auto/team-coding</code>, <code>auto/council-pro</code>, <code>auto/night-coding</code>) trong IDE (Cursor, Claude Code, Antigravity, Cline) như một model thông thường.</div>
        </div>
      </div>
      <div id="combosGridBox" style="display:grid;grid-template-columns:repeat(auto-fill, minmax(360px, 1fr));gap:14px;">
        Đang tải danh sách Combo...
      </div>
    </div>

    <!-- Combo Builder Form -->
    <div class="card" style="border:1px solid #2b354f;">
      <h2>🛠️ Tạo Mới / Tuỳ Chỉnh Combo Kết Nối Nhiều Model</h2>
      <div style="font-size:12.5px;color:var(--muted);margin-bottom:14px;">
        Thiết kế quy trình làm việc giữa nhiều mô hình hoặc sửa lại bất kỳ Combo nào hiện có:
      </div>

      <div class="grid-2">
        <div>
          <label class="form-label">Mã ID Combo (dùng để gọi API trong IDE)</label>
          <input id="comboIdInput" type="text" placeholder="ví dụ: auto/my-super-coder hoặc auto/team-coding" />
        </div>
        <div>
          <label class="form-label">Chế độ phối hợp giữa các Model (Combo Mode)</label>
          <select id="comboModeSelect">
            <option value="pipeline">🔗 Pipeline Nối Tiếp: Bước 1 (Kiến trúc/Phân tích) ➔ Bước 2 (Viết Code/Hoàn thiện)</option>
            <option value="consensus">🧠 Hội Đồng Song Song (Mixture of Agents): Chạy đồng thời &amp; Tổng hợp</option>
            <option value="night-saver">⚡ Tối Ưu Giảm Giá Ban Đêm (Night 40%-50% Off + Dự phòng)</option>
            <option value="failover">🛡️ Failover Chuyển Tiếp Tức Thì (Model 1 ➔ Model 2 ➔ Model 3)</option>
          </select>
        </div>
      </div>

      <div class="grid-2" style="margin-top:10px;">
        <div>
          <label class="form-label">Tên hiển thị Combo</label>
          <input id="comboNameInput" type="text" placeholder="Ví dụ: Combo Kiến Trúc Opus 5.5 ➔ Code Qwen 3.8 Max" />
        </div>
        <div>
          <label class="form-label">Mô tả nhiệm vụ</label>
          <input id="comboDescInput" type="text" placeholder="Ví dụ: Kết hợp Claude Opus 5.5 thiết kế hệ thống và Qwen 3.8 Max (⚡40% Off) viết code" />
        </div>
      </div>

      <!-- 4 Model Steps Selector -->
      <div style="margin-top:14px;padding:14px;background:#0b0f17;border:1px solid #242c3d;border-radius:10px;">
        <div style="font-weight:700;font-size:13px;color:#e2e8f0;margin-bottom:10px;">🔗 Cấu Hình Chuỗi Model Tham Gia Làm Việc:</div>
        <div class="grid-2">
          <div>
            <label class="form-label" style="color:#c084fc;">Bước 1: Model Chủ Đạo / Kiến Trúc Sư (Architect / Primary)</label>
            <select id="comboStep1Select" class="combo-model-select"></select>
          </div>
          <div>
            <label class="form-label" style="color:#38bdf8;">Bước 2: Model Thực Thi / Lập Trình Viên (Executor / Chuyên gia 2)</label>
            <select id="comboStep2Select" class="combo-model-select"></select>
          </div>
        </div>
        <div class="grid-2" style="margin-top:10px;">
          <div>
            <label class="form-label" style="color:#34d399;">Bước 3: Model Tổng Hợp / Chuyên gia 3 (Synthesizer / Fallback)</label>
            <select id="comboStep3Select" class="combo-model-select"></select>
          </div>
          <div>
            <label class="form-label" style="color:#fbbf24;">Bước 4: Model Dự Phòng Cuối (Backup Model — Tuỳ chọn)</label>
            <select id="comboStep4Select" class="combo-model-select"></select>
          </div>
        </div>

        <div style="margin-top:12px;">
          <label class="form-label">Lời nhắc phối hợp giữa các Model (Handoff / Synthesis Prompt — Dùng cho Pipeline &amp; Hội Đồng MoA)</label>
          <textarea id="comboHandoffInput" rows="2" placeholder="Ví dụ: Dựa trên bản phân tích kiến trúc từ Model Bước 1 ở trên, hãy viết code chi tiết, đầy đủ và tối ưu hiệu năng nhất..."></textarea>
        </div>
      </div>

      <div class="btn-row" style="justify-content:flex-end;margin-top:14px;">
        <button onclick="resetComboBuilderForm()">Đặt lại Form</button>
        <button class="btn-purple" onclick="saveComboFromUI()">💾 Lưu Combo Đa Mô Hình</button>
      </div>
      <div id="comboSaveMsg" class="hint"></div>
    </div>
  </div>

  <!-- VIEW 12: PARALLEL SUB-AGENTS SWARM ENGINE (50 ANTIGRAVITY ACCOUNTS) -->
  <div id="view-subagents" class="view-content">
    <div class="card" style="border:1px solid #2b354f;background:#121723;">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:12px;">
        <div>
          <h2>🐝 Cơ Chế Chạy Nhiều Sub-Agents Cùng Lúc (50-Account Parallel Goroutine Swarm)</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Phân bổ <strong>1 Sub-Agent = 1 Tài khoản Antigravity độc lập</strong> (Least In-Flight + Account Pinning). Khi có 50 tài khoản trong Pool, bạn có thể kích hoạt <strong>50 Sub-Agents chạy song song 100% cùng 1 mili-giây</strong> mà không bao giờ bị đụng Rate-Limit hay nghẽn hàng đợi.
          </div>
        </div>
        <div style="display:flex;gap:8px;align-items:center;">
          <span class="badge badge-green">🟢 100% Parallel Goroutines</span>
          <button onclick="loadSubAgentsView()">🔄 Làm mới trạng thái</button>
        </div>
      </div>

      <!-- KPI Cards -->
      <div class="kpi-summary-grid" style="margin-top:16px;margin-bottom:0;">
        <div class="setting-card" style="padding:14px;border-left:3px solid #fb923c;">
          <div style="font-size:11px;color:var(--muted);font-weight:600;">TÀI KHOẢN ANTIGRAVITY PRO (MỤC TIÊU 50)</div>
          <div id="saKpiAgyCount" style="font-size:25px;font-weight:700;color:#fb923c;margin-top:4px;">0 / 50</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #10b981;">
          <div style="font-size:11px;color:#34d399;font-weight:600;">SLOT SẴN SÀNG CHẠY SONG SONG</div>
          <div id="saKpiReadyCount" style="font-size:25px;font-weight:700;color:#10b981;margin-top:4px;">0</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #a855f7;">
          <div style="font-size:11px;color:#c084fc;font-weight:600;">🔥 SUB-AGENTS ĐANG CHẠY CÙNG LÚC</div>
          <div id="saKpiActiveNow" style="font-size:25px;font-weight:700;color:#c084fc;margin-top:4px;">0</div>
        </div>
        <div class="setting-card" style="padding:14px;border-left:3px solid #38bdf8;">
          <div style="font-size:11px;color:#38bdf8;font-weight:600;">⚡ TỔNG SUB-AGENTS ĐÃ THỰC THI</div>
          <div id="saKpiTotalRun" style="font-size:25px;font-weight:700;color:#38bdf8;margin-top:4px;">0</div>
        </div>
      </div>

      <!-- Kiến trúc Phân Tầng Quản Lý Dự Án SEO & Đàn Sub-Agents Antigravity Pro -->
      <div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(260px, 1fr));gap:12px;margin-top:14px;">
        <div class="setting-card" style="padding:13px;border-left:3px solid #f59e0b;background:rgba(245,158,11,0.06);">
          <div style="font-weight:700;font-size:13px;color:#fbbf24;">👑 Tầng 1: Quản Lý Dự Án SEO (Lead Orchestrator)</div>
          <div style="font-size:11.5px;color:#e2e8f0;margin-top:5px;line-height:1.5;">
            • <strong>Ban ngày (07:00–19:00):</strong> <code>antigravity/gemini-4-carbon-high</code>, <code>antigravity/gemini-4-argon-high</code> &amp; <code>claude-code/claude-opus-5-5</code><br/>
            • <strong>Buổi tối (19:00–07:00):</strong> Tự động ưu tiên <code>alibaba/qwen3.8-max</code> (<span style="color:#34d399;font-weight:700;">⚡ 40% Off Nights</span>) làm Quản Lý Dự Án SEO &amp; chia việc cho đàn Sub-Agents.
          </div>
        </div>
        <div class="setting-card" style="padding:13px;border-left:3px solid #10b981;background:rgba(16,185,129,0.06);">
          <div style="font-weight:700;font-size:13px;color:#34d399;">✍️ Tầng 2A: Đội Sub-Agents Viết Bài Chuẩn SEO</div>
          <div style="font-size:11.5px;color:#e2e8f0;margin-top:5px;line-height:1.5;">
            • <strong>Model chuyên trách:</strong> <code>antigravity/gemini-3.1-pro-high</code> (<strong>Gemini 3.1 Pro High</strong>)<br/>
            • Chạy song song trên <strong>50 tài khoản Antigravity Pro</strong> để viết bài chuẩn SEO E-E-A-T, Content Pillar, Semantic Keywords mà không tốn hạn mức Argon/Opus.
          </div>
        </div>
        <div class="setting-card" style="padding:13px;border-left:3px solid #38bdf8;background:rgba(56,189,248,0.06);">
          <div style="font-weight:700;font-size:13px;color:#38bdf8;">💻 Tầng 2B: Đội Sub-Agents Code &amp; Kỹ Thuật SEO</div>
          <div style="font-size:11.5px;color:#e2e8f0;margin-top:5px;line-height:1.5;">
            • <strong>Model chuyên trách:</strong> <code>antigravity/gemini-3.8-flash-high</code> (<strong>Gemini 3.8 Flash High</strong>)<br/>
            • Chạy song song tốc độ cực cao trên <strong>50 tài khoản Antigravity Pro</strong> để viết Code, Schema JSON-LD, tối ưu Core Web Vitals &amp; Automation Script.
          </div>
        </div>
      </div>
    </div>

    <!-- Ma Trận 50 Tài Khoản & Slot Sub-Agent -->
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;flex-wrap:wrap;gap:10px;">
        <div>
          <h2>🎛️ Ma Trận Phân Tải Tài Khoản Thời Gian Thực (1 Slot = 1 Sub-Agent)</h2>
          <div style="font-size:12px;color:var(--muted);">Bộ cân bằng tải <code>Least-In-Flight + Atomic Round-Robin</code> tự động gán mỗi Sub-Agent vào một tài khoản đang rảnh (<code>In-Flight = 0</code>) để tối đa hóa tốc độ khi chạy 50 tài khoản.</div>
        </div>
        <button onclick="switchNav('view-providers')">+ Thêm Tài Khoản Mới</button>
      </div>
      <div id="saAccountSlotsGrid" style="display:grid;grid-template-columns:repeat(auto-fill, minmax(250px, 1fr));gap:10px;">
        Đang tải ma trận tài khoản...
      </div>
    </div>

    <!-- Bảng Điều Khiển Kích Hoạt Đàn Sub-Agents Song Song -->
    <div class="card" style="border:1px solid #2b354f;">
      <h2>🚀 Kích Hoạt Quản Lý Dự Án SEO &amp; Đàn Sub-Agents Chạy Cùng Lúc (50 Antigravity Pro Swarm)</h2>
      <div style="font-size:12.5px;color:var(--muted);margin-bottom:14px;">
        <strong>Bước 1:</strong> Model Quản Lý Dự Án SEO (Ban ngày: <code>antigravity/gemini-4-carbon-high</code>, <code>antigravity/gemini-4-argon-high</code> &amp; <code>claude-code/claude-opus-5-5</code> | Buổi tối: <code>alibaba/qwen3.8-max</code> ⚡40% Off) lập kế hoạch tổng thể.<br/>
        <strong>Bước 2:</strong> Phóng đồng loạt <strong>N Sub-Agents chạy song song</strong> trên 50 tài khoản Antigravity Pro: <code>antigravity/gemini-3.1-pro-high</code> (<strong>Gemini 3.1 Pro High</strong>) viết bài chuẩn SEO &amp; <code>antigravity/gemini-3.8-flash-high</code> (<strong>Gemini 3.8 Flash High</strong>) viết code kỹ thuật.
      </div>

      <div class="grid-2">
        <div>
          <label class="form-label">Số lượng Sub-Agents chạy đồng thời (trên Pool 50 Acc Antigravity Pro)</label>
          <select id="saAgentCountSelect">
            <option value="3">3 Sub-Agents chạy cùng lúc (3 Tài khoản song song)</option>
            <option value="5" selected>5 Sub-Agents chạy cùng lúc (5 Tài khoản song song)</option>
            <option value="8">8 Sub-Agents chạy cùng lúc (8 Tài khoản song song)</option>
            <option value="10">10 Sub-Agents chạy cùng lúc (10 Tài khoản song song)</option>
            <option value="20">20 Sub-Agents chạy cùng lúc (20 Tài khoản song song)</option>
            <option value="30">30 Sub-Agents chạy cùng lúc (30 Tài khoản song song)</option>
            <option value="50">50 Sub-Agents chạy cùng lúc (Toàn bộ 50 Tài khoản Antigravity Pro)</option>
          </select>
        </div>
        <div>
          <label class="form-label">Chế độ Phân Công Đội Sub-Agents (Viết Bài SEO &amp; Code)</label>
          <select id="saSwarmRoleModeSelect">
            <option value="seo-and-code" selected>👑 Kết Hợp: Quản Lý SEO ➔ Viết Bài (Gemini 3.1 Pro High) + Code (Gemini 3.8 Flash High)</option>
            <option value="content-only">✍️ Chuyên Viết Bài Hàng Loạt: Quản Lý SEO ➔ 100% Sub-Agents Gemini 3.1 Pro High</option>
            <option value="coding-only">💻 Chuyên Viết Code Hàng Loạt: Quản Lý SEO ➔ 100% Sub-Agents Gemini 3.8 Flash High</option>
            <option value="night-seo">🌙 Chế Độ Đêm (⚡ 40% Off): Quản Lý SEO Qwen 3.8 Max ➔ Gemini 3.1 Pro High &amp; 3.8 Flash High</option>
          </select>
        </div>
      </div>

      <div style="margin-top:10px;">
        <label class="form-label">👑 Model Quản Lý Dự Án SEO (Tier 1 Lead Manager — Lập kế hoạch &amp; Điều phối)</label>
        <select id="saModelSelect">
          <option value="auto-manager" selected>👑 Tự động theo giờ: Ngày (gemini-4-carbon-high / gemini-4-argon-high &amp; claude-opus-5-5) | Tối (alibaba/qwen3.8-max ⚡ 40% Off)</option>
          <option value="antigravity/gemini-4-carbon-high">antigravity/gemini-4-carbon-high (Gemini 4 Carbon High - Quản Lý Dự Án SEO)</option>
          <option value="antigravity/gemini-4-argon-high">antigravity/gemini-4-argon-high (Gemini 4 Argon High - Quản Lý Dự Án SEO)</option>
          <option value="claude-code/claude-opus-5-5">claude-code/claude-opus-5-5 (Claude Opus 5.5 - Quản Lý Dự Án SEO)</option>
          <option value="alibaba/qwen3.8-max">alibaba/qwen3.8-max (Alibaba Qwen 3.8 Max ⚡ 40% Off Nights - Quản Lý SEO Buổi Tối)</option>
        </select>
      </div>

      <div style="margin-top:12px;">
        <label class="form-label">Chủ đề dự án SEO / Yêu cầu bài viết &amp; Code (Hoặc nhập mỗi dòng 1 từ khóa/bài viết để 50 Sub-Agents viết cùng lúc)</label>
        <textarea id="saMasterGoalInput" rows="4" placeholder="Ví dụ: Chiến dịch SEO Tổng Thể & Hệ Thống Web Cloud VPS BuCloud 2026: Lập bộ từ khóa Semantic, viết chuỗi bài Pillar & Cluster chuẩn E-E-A-T, tạo mã nguồn Landing Page chuẩn Core Web Vitals và Schema JSON-LD..."></textarea>
      </div>

      <div style="display:flex;justify-content:space-between;align-items:center;margin-top:12px;flex-wrap:wrap;gap:10px;">
        <label style="font-size:12px;color:var(--muted);display:flex;align-items:center;gap:6px;cursor:pointer;">
          <input type="checkbox" id="saLineByLineCheckbox" style="width:auto;" /> Mỗi dòng là 1 nhiệm vụ Sub-Agent riêng biệt (Custom Tasks Mode)
        </label>
        <button id="saLaunchBtn" class="btn-primary" onclick="launchSubAgentSwarmUI()">🚀 Phóng Đàn Sub-Agents Chạy Song Song Ngay</button>
      </div>
      <div id="saLaunchStatus" class="hint" style="margin-top:8px;"></div>

      <!-- Kết quả thực thi Đàn Sub-Agents -->
      <div id="saSwarmResultsBox" style="margin-top:16px;"></div>
    </div>

    <!-- LẬP LỊCH TỰ ĐỘNG HÓA BAN ĐÊM (ORCA AUTOMATIONS ENGINE) -->
    <div class="card" style="border:1px solid #334155;">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;margin-bottom:10px;">
        <div>
          <h2>🌙 Lập Lịch Tự Động Hóa Định Kỳ (Orca Automations Engine)</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Tự động kích hoạt Đàn Sub-Agents chạy vào ban đêm (19:00–07:00) để tận dụng ưu đãi <strong style="color:#34d399;">⚡ Giảm 40%–50% Alibaba Qwen 3.8 Max / DeepSeek V4</strong> kết hợp cùng 50 tài khoản Antigravity Pro.
          </div>
        </div>
        <button class="btn-primary" onclick="toggleNewAutomationForm()">+ Tạo Lịch Tự Động Hóa Mới</button>
      </div>

      <!-- Form tạo lịch tự động hóa -->
      <div id="newAutomationFormBox" style="display:none;background:#0d121d;border:1px dashed #38bdf8;border-radius:10px;padding:14px;margin-bottom:14px;">
        <div style="font-weight:700;font-size:13px;color:#38bdf8;margin-bottom:10px;">✨ Thiết lập Chiến dịch Tự động hóa mới</div>
        <div class="grid-2">
          <div>
            <label class="form-label">Tên Chiến Dịch</label>
            <input id="autoSchedNameInput" type="text" placeholder="Ví dụ: SEO Đêm 40% Off - BuCloud VPS &amp; Hosting" />
          </div>
          <div>
            <label class="form-label">Khung giờ chạy tự động</label>
            <select id="autoSchedWindowSelect">
              <option value="night-22h" selected>🌙 22:00 Mỗi Đêm (Khung giờ vàng giảm 40%-50% token cost)</option>
              <option value="nightly_21h">🌙 21:00 Mỗi Đêm (Tối ưu giờ bắt đầu ban đêm)</option>
              <option value="interval_12h">⏰ Mỗi 12 Giờ Một Lần (Chạy 2 lần / ngày)</option>
              <option value="interval_6h">⚡ Mỗi 6 Giờ Một Lần (Chạy liên tục 4 lần / ngày)</option>
            </select>
          </div>
        </div>
        <div class="grid-2" style="margin-top:10px;">
          <div>
            <label class="form-label">Số Sub-Agents chạy đồng thời</label>
            <input id="autoSchedCountInput" type="number" min="1" max="50" value="8" />
          </div>
          <div>
            <label class="form-label">Model Quản Lý Dự Án (Lead Orchestrator)</label>
            <select id="autoSchedModelSelect">
              <option value="alibaba/qwen3.8-max" selected>alibaba/qwen3.8-max (⚡ 40% Off Nights - Tối ưu chi phí)</option>
              <option value="antigravity/gemini-4-carbon-high">antigravity/gemini-4-carbon-high (Carbon High Thinking)</option>
              <option value="antigravity/gemini-4-argon-high">antigravity/gemini-4-argon-high (Argon High Thinking)</option>
              <option value="claude-code/claude-opus-5-5">claude-code/claude-opus-5-5 (Claude Opus 5.5)</option>
            </select>
          </div>
        </div>
        <div style="margin-top:10px;">
          <label class="form-label">Mục tiêu chiến dịch (Master Goal)</label>
          <textarea id="autoSchedGoalInput" rows="3" placeholder="Nhập mục tiêu chiến dịch SEO, viết bài cluster hoặc kiểm tra mã nguồn tự động hàng đêm..."></textarea>
        </div>
        <div class="btn-row" style="justify-content:flex-end;margin-top:10px;">
          <button onclick="toggleNewAutomationForm()">Hủy</button>
          <button class="btn-primary" onclick="submitCreateAutomation()">💾 Lưu Lịch Tự Động Hóa</button>
        </div>
      </div>

      <!-- Danh sách lịch tự động hóa -->
      <div id="saAutomationsListBox">
        Đang tải danh sách lịch tự động...
      </div>
    </div>

    <!-- KHO LƯU TRỮ CHIẾN DỊCH (AI VAULT & 1-CLICK EXPORT) -->
    <div class="card" style="border:1px solid #1e293b;">
      <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;margin-bottom:12px;">
        <div>
          <h2>🗄️ Kho Lưu Trữ Chiến Dịch AI Vault (Bền Vững Trên Đĩa &amp; Xuất 1-Click)</h2>
          <div style="font-size:12.5px;color:var(--muted);">
            Toàn bộ các chiến dịch 50 Sub-Agents được lưu bền vững vào <code>omni_subagent_vault.json</code>. Bấm tải về ngay báo cáo Markdown chuẩn SEO hoặc định dạng JSON.
          </div>
        </div>
        <div style="display:flex;gap:6px;">
          <a href="/v1/subagents?export=latest&amp;format=md" target="_blank" class="btn" style="text-decoration:none;display:inline-flex;align-items:center;gap:5px;font-size:11.5px;color:#38bdf8;">📥 Xuất Chiến Dịch Vừa Chạy (.MD)</a>
          <a href="/v1/subagents?export=latest&amp;format=json" target="_blank" class="btn" style="text-decoration:none;display:inline-flex;align-items:center;gap:5px;font-size:11.5px;color:#34d399;">📥 Xuất JSON (.JSON)</a>
        </div>
      </div>
      <div id="saVaultHistoryGrid">
        Đang tải lịch sử kho lưu trữ...
      </div>
    </div>

    <!-- Nhập Hàng Loạt 50 Tài Khoản Antigravity -->
    <div class="card">
      <h2>📥 Nhập Nhanh Hàng Loạt 50 Tài Khoản Antigravity (Bulk Account Importer)</h2>
      <div style="font-size:12.5px;color:var(--muted);margin-bottom:10px;">
        Ngoài việc bấm kết nối từng tài khoản ở mục <strong>🔌 Kết Nối Nhà Cung Cấp</strong>, bạn có thể dán danh sách nhiều URL Callback OAuth, Refresh Token (<code>1//0g...</code> hoặc <code>email@gmail.com|1//0g...</code>) hoặc Access Token (<code>ya29...</code>) — mỗi dòng 1 tài khoản — để nạp nhanh đủ 50 tài khoản vào Pool:
      </div>
      <textarea id="saBulkImportInput" rows="4" placeholder="Mỗi dòng 1 tài khoản:&#10;http://127.0.0.1:20128/callback?code=4/0A...&#10;acc1@gmail.com|1//0gxxxxxxx...&#10;ya29.a0AfH6SM..."></textarea>
      <div class="btn-row" style="justify-content:flex-end;margin-top:10px;">
        <button class="btn-purple" onclick="submitBulkImportAccounts()">📥 Nạp Hàng Loạt Vào Pool Antigravity</button>
      </div>
      <div id="saBulkImportMsg" class="hint"></div>
    </div>
  </div>
    </main>
  </div>

  <!-- MOBILE FIXED BOTTOM NAVIGATION BAR -->
  <nav class="mobile-bottom-nav" aria-label="Điều hướng di động">
    <button class="mobile-bnav-btn active" data-view="view-quota" onclick="switchNav('view-quota')">
      <span class="bnav-icon">📊</span>
      <span>Hạn Mức</span>
    </button>
    <button class="mobile-bnav-btn" data-view="view-subagents" onclick="switchNav('view-subagents')">
      <span class="bnav-icon">🐝</span>
      <span>50 Agents</span>
    </button>
    <button class="mobile-bnav-btn" data-view="view-combos" onclick="switchNav('view-combos')">
      <span class="bnav-icon">🔗</span>
      <span>Combo</span>
    </button>
    <button class="mobile-bnav-btn" data-view="view-endpoint" onclick="switchNav('view-endpoint')">
      <span class="bnav-icon">🎯</span>
      <span>Điểm Cuối</span>
    </button>
    <button class="mobile-bnav-btn" onclick="toggleMobileSidebar()">
      <span class="bnav-icon">☰</span>
      <span>Menu</span>
    </button>
  </nav>

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
          <label class="form-label">Tên Nhân Viên / Tên Khóa</label>
          <input id="keyNameInput" type="text" placeholder="ví dụ: Nguyễn Văn A - Dev Backend (hoặc Khóa sản xuất)" />
          <div class="hint">Đặt tên theo nhân viên hoặc phòng ban để dễ dàng kiểm tra lịch sử Prompt họ đã dùng</div>
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

  <!-- MODAL: CHI TIẾT PROMPT NHÂN VIÊN & CÂU TRẢ LỜI AI -->
  <div id="promptDetailModal" class="modal-backdrop">
    <div class="modal-card" style="max-width:780px;">
      <div class="modal-header">
        <div class="traffic-lights">
          <span class="dot dot-red"></span>
          <span class="dot dot-yellow"></span>
          <span class="dot dot-green"></span>
        </div>
        <div class="modal-title" id="promptModalTitle">Chi Tiết Prompt Nhân Viên &amp; Phản Hồi AI</div>
        <button class="modal-close" onclick="closePromptDetailModal()">✕</button>
      </div>
      <div class="modal-body" style="max-height:78vh;overflow-y:auto;">
        <div id="promptModalMeta" style="display:flex;flex-wrap:wrap;gap:8px;margin-bottom:14px;padding:10px 12px;background:#111520;border:1px solid #242c3d;border-radius:8px;font-size:12px;"></div>

        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px;">
          <label class="form-label" style="margin:0;color:#38bdf8;">💬 Nội dung Prompt Nhân viên đã gửi:</label>
          <button style="padding:3px 9px;font-size:11px;" onclick="copyPromptModalText('promptModalUserText')">📋 Sao chép Prompt</button>
        </div>
        <pre id="promptModalUserText" style="max-height:260px;white-space:pre-wrap;word-break:break-word;background:#0a0e17;border:1px solid #1e293b;color:#f1f5f9;font-size:12.5px;margin-bottom:14px;"></pre>

        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px;">
          <label class="form-label" style="margin:0;color:#34d399;">🤖 Câu trả lời AI đã phản hồi:</label>
          <button style="padding:3px 9px;font-size:11px;" onclick="copyPromptModalText('promptModalAiText')">📋 Sao chép Câu trả lời</button>
        </div>
        <pre id="promptModalAiText" style="max-height:280px;white-space:pre-wrap;word-break:break-word;background:#0a0e17;border:1px solid #1e293b;color:#cbd5e1;font-size:12.5px;"></pre>
      </div>
      <div class="modal-footer" style="justify-content:flex-end;">
        <button class="btn-primary" onclick="closePromptDetailModal()">Đóng</button>
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

    function handleSessionExpired() {
      const overlay = document.getElementById('loginOverlay');
      if (overlay && !overlay.classList.contains('open')) {
        checkAuthStatus();
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
        if (r.status === 401) { handleSessionExpired(); return; }
        const d = await r.json();
        currentQuotaSummary = d;

        document.getElementById('kpiTotal').textContent = d.total_accounts || 0;
        document.getElementById('kpiCritical').textContent = d.critical_accounts || 0;
        document.getElementById('kpiWarning').textContent = d.warning_accounts || 0;
        document.getElementById('kpiHealthy').textContent = d.healthy_accounts || 0;
        const kpiVal = document.getElementById('kpiTotalValueUSD');
        if (kpiVal) kpiVal.textContent = '$' + (d.total_estimated_value_usd || 0).toFixed(2);
        document.getElementById('quotaAccountCountLabel').textContent = 'Giới hạn của nhà cung cấp (' + (d.total_accounts || 0) + ' tài khoản)';

        renderQuotaCards();
        renderLiveExecutionMonitor(d);
      } catch (err) {
        box.innerHTML = '<div style="color:#f87171;">Lỗi tải hạn mức: ' + err.message + '</div>';
      }
    }

    let latestAccountLogByEmail = {};

    function normalizeModelSlugJS(m) {
      let s = (m || '').trim().toLowerCase();
      const prefixes = ['antigravity/', 'claude-code/', 'alibaba/', 'models/', 'google/', 'agy/', 'anthropic/', 'cc/', 'dashscope/', 'bailian/'];
      for (let i = 0; i < prefixes.length; i++) {
        if (s.startsWith(prefixes[i])) {
          s = s.slice(prefixes[i].length).trim();
          break;
        }
      }
      return s;
    }

    function formatModelRoleBadge(rawModel, latestLogEntry) {
      const raw = (rawModel || '').trim();
      if (!raw) return 'Model';
      let sep = '';
      if (raw.indexOf('➔') !== -1) sep = '➔';
      else if (raw.indexOf('->') !== -1) sep = '->';

      if (sep) {
        const parts = raw.split(sep);
        const reqPart = (parts[0] || '').trim();
        const actPart = parts.slice(1).join(' ' + sep + ' ').trim();
        if (reqPart && actPart && normalizeModelSlugJS(reqPart) !== normalizeModelSlugJS(actPart)) {
          return '<span style="color:#cbd5e1;">' + htmlEscape(reqPart) + '</span> ' +
            '<span style="display:inline-block;padding:1px 6px;border-radius:4px;background:rgba(245,158,11,0.18);border:1px solid rgba(245,158,11,0.45);color:#fbbf24;font-weight:700;">➔ thực chạy: ' + htmlEscape(actPart) + '</span>';
        }
        return htmlEscape(actPart || reqPart);
      }

      if (latestLogEntry && latestLogEntry.requested_model) {
        const normRaw = normalizeModelSlugJS(raw);
        const normLogRes = normalizeModelSlugJS(latestLogEntry.resolved_model || '');
        const normLogReq = normalizeModelSlugJS(latestLogEntry.requested_model || '');
        if (normRaw === normLogRes || normRaw === normLogReq) {
          return formatLogModelTruth(latestLogEntry);
        }
      }
      return htmlEscape(raw);
    }

    function formatLogModelTruth(e) {
      if (!e) return '';
      const reqM = (e.requested_model || '').trim();
      const resM = (e.resolved_model || '').trim();
      const upM = (e.upstream_model || '').trim();

      if (resM.indexOf('➔') !== -1 || resM.indexOf('->') !== -1) {
        const baseBadge = formatModelRoleBadge(resM, null);
        if (upM && resM.toLowerCase().indexOf(upM.toLowerCase()) === -1) {
          return baseBadge + ' <span style="color:#38bdf8;font-size:10.5px;">[' + htmlEscape(upM) + ']</span>';
        }
        return baseBadge;
      }

      const normReq = normalizeModelSlugJS(reqM);
      const normRes = normalizeModelSlugJS(resM);
      const normUp = normalizeModelSlugJS(upM);

      let actualDisplay = resM || upM || reqM;
      if (resM && upM && normRes !== normUp && resM.toLowerCase().indexOf('[' + upM.toLowerCase() + ']') === -1) {
        actualDisplay = resM + ' [' + upM + ']';
      }

      const hasDiff = (reqM && resM && normReq !== normRes) || (reqM && upM && normReq !== normUp);
      if (hasDiff) {
        return '<span style="color:#cbd5e1;">' + htmlEscape(reqM) + '</span> ' +
          '<span style="display:inline-block;padding:1px 6px;border-radius:4px;background:rgba(245,158,11,0.18);border:1px solid rgba(245,158,11,0.45);color:#fbbf24;font-weight:700;">➔ thực chạy: ' + htmlEscape(actualDisplay) + '</span>';
      }
      return '<span>' + htmlEscape(actualDisplay || reqM) + '</span>';
    }

    async function renderLiveExecutionMonitor(quotaData) {
      const inFlightBox = document.getElementById('liveInFlightList');
      const badgeEl = document.getElementById('liveActiveCountBadge');
      const recentBox = document.getElementById('liveRecentRequestsMini');
      if (!inFlightBox) return;

      const accounts = (quotaData && quotaData.accounts) ? quotaData.accounts : [];
      const activeAccs = accounts.filter(a => (a.in_flight || 0) > 0);
      const totalInFlight = activeAccs.reduce((sum, a) => sum + (a.in_flight || 0), 0);

      if (badgeEl) {
        if (totalInFlight > 0) {
          badgeEl.className = 'badge badge-purple';
          badgeEl.innerHTML = '🔥 Đang chạy ' + totalInFlight + ' Request thực tế';
        } else {
          badgeEl.className = 'badge badge-green';
          badgeEl.innerHTML = '🟢 0 Request đang chạy (Tất cả đang rảnh)';
        }
      }

      if (activeAccs.length > 0) {
        inFlightBox.innerHTML = activeAccs.map(a => {
          const accKey = (a.email || a.id || '').toLowerCase();
          return '<div style="display:inline-flex;align-items:center;gap:8px;flex-wrap:wrap;background:rgba(168,85,247,0.16);border:1px solid #a855f7;border-radius:8px;padding:6px 11px;margin-right:8px;margin-bottom:4px;">' +
            '<span style="color:#c084fc;font-weight:700;">🔥 ĐANG XỬ LÝ:</span>' +
            '<strong style="color:#fff;font-family:monospace;">' + htmlEscape(a.email || a.id) + '</strong>' +
            '<span style="color:#38bdf8;font-weight:700;">➔ ' + formatModelRoleBadge(a.active_model || a.last_model || 'Model', a.active_model ? null : latestAccountLogByEmail[accKey]) + '</span>' +
            '<span class="badge badge-purple">' + a.in_flight + ' req</span>' +
          '</div>';
        }).join('');
      } else {
        inFlightBox.innerHTML = '<span style="color:#34d399;font-weight:600;">🟢 Hiện tại không có lệnh nào đang chạy ngầm — Toàn bộ ' + accounts.length + ' tài khoản đều đang rảnh 100% (Idle &amp; Sẵn sàng nhận request mới).</span>';
      }

      if (recentBox) {
        try {
          const lr = await fetch('/v1/logs?limit=25');
          const ld = await lr.json();
          const rawList = (ld && (ld.data || ld.entries)) ? (ld.data || ld.entries) : [];
          const nextLogMap = {};
          for (let i = 0; i < rawList.length; i++) {
            const item = rawList[i];
            const k = (item && item.account ? item.account : '').toLowerCase();
            if (k && !nextLogMap[k]) {
              nextLogMap[k] = item;
            }
          }
          latestAccountLogByEmail = nextLogMap;
          renderQuotaCards();

          const entries = rawList.slice(0, 5);
          if (entries.length === 0) {
            recentBox.innerHTML = 'Chưa có lịch sử gọi model nào gần đây.';
          } else {
            let rows = '<div style="font-weight:700;color:#e2e8f0;margin-bottom:5px;">⚡ 5 Lượt Chạy Thực Tế Gần Nhất (Hiển thị 100% sự thật Model yêu cầu ➔ Model thực chạy):</div>' +
              '<div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(290px, 1fr));gap:6px;">';
            entries.forEach(e => {
              const latColor = e.latency_ms > 7000 ? '#f87171' : (e.latency_ms > 3000 ? '#fbbf24' : '#34d399');
              rows += '<div style="background:#131926;border:1px solid #232c3d;border-radius:6px;padding:6px 9px;display:flex;justify-content:space-between;align-items:center;gap:6px;">' +
                '<div style="min-width:0;flex:1;">' +
                  '<div style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">' +
                    '<span style="color:#64748b;font-family:monospace;font-size:10.5px;">[' + htmlEscape((e.timestamp || '').split(' ')[0] || '') + ']</span> ' +
                    '<strong style="color:#38bdf8;font-family:monospace;">' + htmlEscape(e.account || e.provider || 'auto') + '</strong>' +
                  '</div>' +
                  '<div style="color:#fbbf24;font-family:monospace;font-size:11px;font-weight:600;margin-top:2px;line-height:1.4;word-break:break-word;">🤖 ' + formatLogModelTruth(e) + '</div>' +
                '</div>' +
                '<div style="text-align:right;white-space:nowrap;">' +
                  '<div style="color:' + latColor + ';font-weight:700;font-family:monospace;">' + (e.latency_ms || 0) + ' ms</div>' +
                  '<div style="font-size:10px;color:#94a3b8;">' + (e.total_tokens || 0) + ' tok</div>' +
                '</div>' +
              '</div>';
            });
            rows += '</div>';
            recentBox.innerHTML = rows;
          }
        } catch (_) {}
      }
    }

    async function resetAccountLatency(accId) {
      try {
        await fetch('/v1/quota/threshold', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({action: 'reset_latency', account_id: accId || 'all'})
        });
        loadQuotaSummary(false);
      } catch (e) {
        alert('Lỗi reset độ trễ: ' + e.message);
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
        const borderCol = (acc.in_flight > 0) ? '#a855f7' : (isCrit ? 'rgba(239, 68, 68, 0.4)' : '#242c3d');
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
        const rawValURL = (acc.validation_url || '').trim();
        const safeValURL = rawValURL.startsWith('https://') ? htmlEscape(rawValURL) : '';
        if (acc.error_message && !safeValURL) {
          modelBarsHTML += '<div style="padding:8px;background:rgba(239, 68, 68, 0.1);border-radius:6px;color:#fca5a5;font-size:11.5px;margin-top:6px;">⚠️ ' + htmlEscape(acc.error_message) + '</div>';
        }

        const safeUsed = (acc.quota_used_percent !== undefined && acc.quota_used_percent !== null) ? acc.quota_used_percent : (acc.provider === 'alibaba' ? 72.24 : 16);
        const safeReset = (acc.reset_time_text || '').replace(/'/g, "\\'");
        const reqSuccess = acc.success_count || 0;
        const reqFail = acc.failure_count || 0;
        const realTokens = (acc.total_tokens || 0).toLocaleString();
        const avgLat = acc.avg_latency_ms ? (acc.avg_latency_ms + ' ms') : '—';
        const latColor = !acc.avg_latency_ms ? '#a78bfa' : (acc.avg_latency_ms > 7000 ? '#f87171' : (acc.avg_latency_ms > 3000 ? '#fbbf24' : '#34d399'));
        const isBusyNow = (acc.in_flight || 0) > 0;
        const routeBadge = !acc.enabled
          ? '<span style="color:#94a3b8;font-weight:600;">⏸️ Đã tắt (Không nhận request)</span>'
          : (acc.validation_url
              ? '<span style="color:#f87171;font-weight:700;">🔒 Bị Google chặn (Cần xác minh)</span>'
              : (acc.in_cooldown
                  ? '<span style="color:#f59e0b;font-weight:700;">⏳ Cooldown</span>'
                  : (isBusyNow
                      ? '<span style="color:#c084fc;font-weight:700;">🔥 ĐANG XỬ LÝ (' + acc.in_flight + ' req)</span>'
                      : '<span style="color:#34d399;font-weight:700;">🟢 Sẵn sàng (Đang rảnh)</span>')));

        const accLogKey = (acc.email || acc.id || '').toLowerCase();
        const activeOrLastModelText = isBusyNow
          ? ('<span style="color:#c084fc;font-weight:700;line-height:1.4;word-break:break-word;">🔥 Đang chạy: ' + formatModelRoleBadge(acc.active_model || 'Model', null) + '</span>')
          : (acc.last_model
              ? ('<span style="color:#fbbf24;font-weight:600;line-height:1.4;word-break:break-word;">🤖 Vừa chạy: ' + formatModelRoleBadge(acc.last_model, latestAccountLogByEmail[accLogKey]) + '</span>' + (acc.last_latency_ms ? (' <span style="color:#94a3b8;">(' + acc.last_latency_ms + ' ms)</span>') : ''))
              : '<span style="color:#64748b;">Chưa chạy model nào</span>');

        const validationCalloutHTML = safeValURL
          ? ('<div style="margin-bottom:8px;padding:8px 9px;background:rgba(239,68,68,0.12);border:1px solid rgba(248,113,113,0.45);border-radius:7px;">' +
              '<div style="color:#fecaca;font-size:11px;line-height:1.45;margin-bottom:7px;">⚠️ Google yêu cầu xác minh bảo mật Gmail này. Hãy mở trình duyệt đang đăng nhập đúng Gmail này rồi bấm nút dưới, sau đó bấm &quot;Làm mới tất cả&quot;:</div>' +
              '<a href="' + safeValURL + '" target="_blank" rel="noopener noreferrer" style="display:block;text-align:center;padding:6px 10px;border-radius:6px;background:linear-gradient(135deg,#ef4444,#f59e0b);color:#fff;font-weight:700;font-size:11.5px;text-decoration:none;box-shadow:0 2px 8px rgba(239,68,68,0.3);">🛡️ Xác Minh Tài Khoản Google (Mở Khóa 403) ↗</a>' +
            '</div>')
          : '';

        const realExecStatsHTML =
          '<div style="background:#0b0f17;border:1px solid ' + (isBusyNow ? '#a855f7' : 'rgba(56,189,248,0.18)') + ';border-radius:8px;padding:8px 10px;margin-bottom:12px;font-size:11.5px;">' +
            '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:4px;">' +
              '<span style="color:var(--muted);">Trạng thái hiện tại:</span>' +
              routeBadge +
            '</div>' +
            '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px;padding-bottom:5px;border-bottom:1px dashed rgba(255,255,255,0.07);font-family:monospace;font-size:11px;">' +
              activeOrLastModelText +
            '</div>' +
            validationCalloutHTML +
            '<div class="acc-exec-grid">' +
              '<div>' +
                '<div style="color:var(--muted);font-size:10px;">TOKEN ĐÃ CHẠY</div>' +
                '<div style="color:#38bdf8;font-weight:700;font-size:12.5px;font-family:monospace;">' + realTokens + '</div>' +
              '</div>' +
              '<div>' +
                '<div style="color:var(--muted);font-size:10px;">REQUEST THỰC TẾ</div>' +
                '<div style="color:#e2e8f0;font-weight:700;font-size:12.5px;font-family:monospace;">' + reqSuccess + ' <span style="color:#64748b;font-size:10.5px;">(' + reqFail + ' lỗi)</span></div>' +
              '</div>' +
              '<div>' +
                '<div style="color:var(--muted);font-size:10px;">GIÁ TRỊ API</div>' +
                '<div style="color:#c084fc;font-weight:700;font-size:12.5px;font-family:monospace;">$' + ((acc.estimated_value_usd || 0).toFixed(2)) + '</div>' +
              '</div>' +
              '<div>' +
                '<div style="color:var(--muted);font-size:10px;">ĐỘ TRỄ TB <span style="cursor:pointer;color:#fbbf24;" title="Reset độ trễ tài khoản này" onclick="resetAccountLatency(\'' + htmlEscape(acc.id) + '\')">↺</span></div>' +
                '<div style="color:' + latColor + ';font-weight:700;font-size:12.5px;font-family:monospace;">' + avgLat + '</div>' +
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
            '<div style="display:flex;justify-content:space-between;align-items:center;font-size:12.5px;color:#38bdf8;font-weight:600;margin-bottom:9px;font-family:monospace;">' +
              '<span title="' + htmlEscape(acc.email || '') + '">' + htmlEscape(acc.email || acc.masked_email) + '</span>' +
              (acc.last_used_at ? '<span style="font-size:10.5px;color:#64748b;font-weight:400;">Chạy lúc: ' + acc.last_used_at + '</span>' : '') +
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
              '<button style="padding:3px 7px;font-size:10.5px;color:#f87171;" onclick="deleteConnectedAccount(\'' + htmlEscape(acc.id) + '\', \'' + htmlEscape(acc.email || acc.id) + '\')">🗑️ Xoá</button>' +
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
        if (r.status === 401) { handleSessionExpired(); return; }
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
                '<span class="badge" style="font-size:10.5px;' + ((m.badge && m.badge.includes('Off')) ? 'background:rgba(16,185,129,0.18);color:#34d399;border:1px solid rgba(16,185,129,0.35);' : 'background:rgba(255,255,255,0.06);color:#cbd5e1;') + '">' + m.badge + '</span>' +
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

    // --- NAVIGATION FLOW & MOBILE DRAWER ---

    function toggleMobileSidebar() {
      const sb = document.getElementById('mainSidebar');
      const bd = document.getElementById('mobileDrawerBackdrop');
      if (!sb) return;
      const isOpen = sb.classList.toggle('mobile-open');
      if (bd) bd.classList.toggle('show', isOpen);
    }

    function closeMobileSidebar() {
      const sb = document.getElementById('mainSidebar');
      const bd = document.getElementById('mobileDrawerBackdrop');
      if (sb) sb.classList.remove('mobile-open');
      if (bd) bd.classList.remove('show');
    }

    function switchNav(viewId) {
      document.querySelectorAll('.view-content').forEach(el => el.classList.remove('active'));
      document.querySelectorAll('.nav-btn').forEach(el => el.classList.remove('active'));
      document.querySelectorAll('.mobile-pill-btn').forEach(el => el.classList.remove('active'));
      document.querySelectorAll('.mobile-bnav-btn[data-view]').forEach(el => el.classList.remove('active'));

      const target = document.getElementById(viewId);
      if (target) target.classList.add('active');

      const sideBtn = document.querySelector('.nav-btn[onclick*="' + viewId + '"]');
      if (sideBtn) sideBtn.classList.add('active');

      const pillBtn = document.querySelector('.mobile-pill-btn[data-view="' + viewId + '"]');
      if (pillBtn) {
        pillBtn.classList.add('active');
        try { pillBtn.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' }); } catch (e) {}
      }

      const bnavBtn = document.querySelector('.mobile-bnav-btn[data-view="' + viewId + '"]');
      if (bnavBtn) bnavBtn.classList.add('active');

      closeMobileSidebar();

      if (viewId === 'view-endpoint') loadEndpointStatus();
      if (viewId === 'view-quota') loadQuotaSummary();
      if (viewId === 'view-models') loadModelsDiagnostics();
      if (viewId === 'view-combos') loadCombosView();
      if (viewId === 'view-subagents') loadSubAgentsView();
      if (viewId === 'view-keys') loadApiKeys();
      if (viewId === 'view-logs') loadLogs();
      if (viewId === 'view-providers') loadProvidersAndAccounts();
      if (viewId === 'view-compression') loadCompressionConfig();
      if (viewId === 'view-optimization') loadOptimizationConfig();
      if (viewId === 'view-vectordb') loadVectorDB();
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
        if (r.status === 401) { handleSessionExpired(); return; }
        const d = await r.json();
        if (d.postgres) {
          renderPostgresBanner(d.postgres);
        }
        populateEmployeeFilterDropdown(d.data || []);

        if (!d.data || d.data.length === 0) {
          box.innerHTML = '<div style="color:var(--muted);padding:14px;background:var(--card-inner);border-radius:8px;">' +
            'Chưa có khóa API nhân viên nào. Hệ thống đang ở chế độ mở (Open Access). ' +
            'Khi bạn cấp khóa API đầu tiên cho nhân viên, hệ thống sẽ tự động yêu cầu xác thực Bearer Token và ghi nhận chi tiết từng Prompt theo tên nhân viên.</div>';
          return;
        }

        let html = '<table><thead><tr>' +
          '<th>Nhân Viên / Tên Khóa</th><th>Mã Key (Masked)</th><th>Quyền Hạn</th><th>Số Request</th><th>Token Đã Dùng</th><th>Hoạt Động Cuối</th><th>Giám Sát &amp; Thao Tác</th>' +
          '</tr></thead><tbody>';

        for (const k of d.data) {
          const adminBadge = k.admin_access ? '<span class="badge badge-purple" style="background:#581c87;color:#d8b4fe;">Admin</span>' : '<span class="badge badge-antigravity">Nhân viên</span>';
          const lastTime = (k.last_used_at || k.created_at || '').replace('T', ' ').slice(0, 19);
          html += '<tr>' +
            '<td><strong>👤 ' + htmlEscape(k.name) + '</strong></td>' +
            '<td><code style="color:#34d399;">' + htmlEscape(k.masked_key) + '</code></td>' +
            '<td>' + adminBadge + '</td>' +
            '<td><strong>' + (k.total_requests || 0).toLocaleString() + '</strong></td>' +
            '<td style="color:#38bdf8;font-weight:700;">' + (k.total_tokens || 0).toLocaleString() + ' tok</td>' +
            '<td style="color:var(--muted);font-size:11px;">' + lastTime + '</td>' +
            '<td style="display:flex;gap:6px;flex-wrap:wrap;">' +
              '<button class="btn-primary" onclick="inspectEmployeePrompts(\'' + htmlEscape(k.id) + '\', \'' + htmlEscape(k.name) + '\')" style="padding:4px 9px;font-size:11.5px;">🔍 Xem Prompt đã dùng</button>' +
              '<button onclick="deleteKey(\'' + htmlEscape(k.id) + '\')" style="color:#f87171;padding:4px 8px;font-size:11.5px;">Xoá</button>' +
            '</td>' +
            '</tr>';
        }
        html += '</tbody></table>';
        box.innerHTML = html;
      } catch (err) {
        box.innerHTML = '<div style="color:#f87171;">Lỗi tải danh sách khóa: ' + err.message + '</div>';
      }
    }

    function renderPostgresBanner(pg) {
      const badge = document.getElementById('pgStatusBadge');
      const detail = document.getElementById('pgStatusDetail');
      const logBadge = document.getElementById('logPgModeBadge');
      const dsnInput = document.getElementById('pgDsnInput');

      if (dsnInput && !dsnInput.value && pg.dsn_masked) {
        dsnInput.placeholder = pg.dsn_masked;
      }

      if (pg.connected) {
        const verTag = pg.server_version ? ('PostgreSQL ' + pg.server_version) : 'PostgreSQL 18';
        if (badge) {
          badge.className = 'badge badge-green';
          badge.textContent = '🟢 ' + verTag + ' Connected (' + (pg.total_prompt_logs || 0) + ' prompts)';
        }
        if (logBadge) {
          logBadge.className = 'badge badge-green';
          logBadge.textContent = '🐘 ' + verTag + ' (' + (pg.total_prompt_logs || 0) + ' bản ghi)';
        }
        if (detail) {
          detail.innerHTML = '<span style="color:#34d399;">✔ Đã kết nối ' + htmlEscape(verTag) + ': <code>' + htmlEscape(pg.dsn_masked || '') + '</code> — Đang lưu trữ bền vững <strong>' +
            (pg.total_keys_in_db || 0) + ' khóa nhân viên</strong> và <strong>' + (pg.total_prompt_logs || 0) + ' lịch sử Prompt</strong>.</span>';
        }
      } else {
        if (badge) {
          badge.className = 'badge badge-yellow';
          badge.textContent = '🟡 Chế độ Local JSON (Chưa kết nối PostgreSQL)';
        }
        if (logBadge) {
          logBadge.className = 'badge badge-yellow';
          logBadge.textContent = '💾 Bộ đệm Local';
        }
        if (detail && pg.last_error) {
          detail.innerHTML = '<span style="color:#fbbf24;">⚠️ Chưa kết nối được PostgreSQL (' + htmlEscape(pg.last_error) + '). Hệ thống đang tự động dùng Local JSON + RAM dự phòng.</span>';
        }
      }
    }

    function fillDefaultDockerPostgres() {
      const inp = document.getElementById('pgDsnInput');
      if (inp) {
        inp.value = 'postgres://omniroute:omniroute_secret@omni-postgres:5432/omniroute?sslmode=disable';
      }
    }

    async function savePostgresConfig() {
      const inp = document.getElementById('pgDsnInput');
      const detail = document.getElementById('pgStatusDetail');
      const dsn = inp ? inp.value.trim() : '';
      if (detail) detail.textContent = '⏳ Đang kiểm tra kết nối PostgreSQL và khởi tạo bảng...';
      try {
        const r = await fetch('/v1/database', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({dsn: dsn})
        });
        const d = await r.json();
        renderPostgresBanner(d);
        if (d.connected) {
          loadApiKeys();
          loadLogs();
        }
      } catch (err) {
        if (detail) detail.innerHTML = '<span style="color:#f87171;">Lỗi: ' + htmlEscape(err.message) + '</span>';
      }
    }

    function populateEmployeeFilterDropdown(keys) {
      const sel = document.getElementById('logEmployeeFilterSelect');
      if (!sel) return;
      const currentVal = sel.value || 'all';
      let html = '<option value="all">👥 Tất cả Nhân viên / API Key</option>';
      for (const k of keys) {
        html += '<option value="' + htmlEscape(k.id) + '">👤 ' + htmlEscape(k.name) + ' (' + htmlEscape(k.masked_key) + ')</option>';
      }
      sel.innerHTML = html;
      sel.value = currentVal;
    }

    function inspectEmployeePrompts(keyId, employeeName) {
      switchNav('view-logs');
      const sel = document.getElementById('logEmployeeFilterSelect');
      if (sel) {
        let found = false;
        for (let i = 0; i < sel.options.length; i++) {
          if (sel.options[i].value === keyId) {
            found = true;
            break;
          }
        }
        if (!found) {
          const opt = document.createElement('option');
          opt.value = keyId;
          opt.textContent = '👤 ' + employeeName;
          sel.appendChild(opt);
        }
        sel.value = keyId;
      }
      loadLogs();
    }

    function resetEmployeeLogFilter() {
      const sel = document.getElementById('logEmployeeFilterSelect');
      const search = document.getElementById('logPromptSearchInput');
      if (sel) sel.value = 'all';
      if (search) search.value = '';
      setLogFilter('all');
    }

    let logSearchTimer = null;
    function debouncedLoadLogs() {
      if (logSearchTimer) clearTimeout(logSearchTimer);
      logSearchTimer = setTimeout(() => loadLogs(), 300);
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

    let cachedPromptLogs = [];

    async function loadLogs() {
      const box = document.getElementById('logsListBox');
      let url = '/v1/logs?limit=80';
      if (activeLogFilter === 'errors') url += '&errors_only=true';
      else if (activeLogFilter !== 'all') url += '&provider=' + encodeURIComponent(activeLogFilter);

      const empSel = document.getElementById('logEmployeeFilterSelect');
      if (empSel && empSel.value && empSel.value !== 'all') {
        url += '&api_key_id=' + encodeURIComponent(empSel.value);
      }
      const qInp = document.getElementById('logPromptSearchInput');
      if (qInp && qInp.value.trim()) {
        url += '&q=' + encodeURIComponent(qInp.value.trim());
      }

      try {
        const r = await fetch(url);
        if (r.status === 401) { handleSessionExpired(); return; }
        const d = await r.json();
        if (d.postgres) {
          renderPostgresBanner(d.postgres);
        }
        cachedPromptLogs = d.data || [];

        if (!cachedPromptLogs || cachedPromptLogs.length === 0) {
          box.innerHTML = '<div style="color:var(--muted);padding:14px;background:var(--card-inner);border-radius:8px;">Không có bản ghi nhật ký / Prompt nào khớp bộ lọc hiện tại.</div>';
          return;
        }

        let html = '<div style="display:flex;flex-direction:column;gap:10px;">';
        for (let idx = 0; idx < cachedPromptLogs.length; idx++) {
          const log = cachedPromptLogs[idx];
          const isErr = log.is_error || log.status_code >= 400;
          const borderCol = isErr ? 'rgba(239, 68, 68, 0.4)' : 'rgba(255,255,255,0.08)';
          const bgCol = isErr ? 'rgba(239, 68, 68, 0.05)' : 'var(--card-inner)';
          const statusBadge = isErr ?
            '<span class="badge badge-red">HTTP ' + log.status_code + '</span>' :
            '<span class="badge badge-green">HTTP ' + log.status_code + '</span>';

          const provBadge = log.provider === 'antigravity' ? 'badge-antigravity' :
                            log.provider === 'claude-code' ? 'badge-claude' : 'badge-alibaba';

          const empName = log.employee_name || 'Admin / Local';
          const empBadge = '<span class="badge" style="background:rgba(56,189,248,0.14);color:#38bdf8;border:1px solid rgba(56,189,248,0.3);">👤 ' +
            htmlEscape(empName) + (log.masked_key ? (' · ' + htmlEscape(log.masked_key)) : '') + '</span>';

          const promptSnippet = log.prompt_text ?
            ('<div style="margin-top:8px;padding:8px 10px;background:#0b0f18;border:1px solid #1e293b;border-radius:6px;font-size:12px;color:#e2e8f0;">' +
              '<span style="color:#38bdf8;font-weight:700;">💬 Prompt: </span>' +
              htmlEscape(log.prompt_text.length > 260 ? (log.prompt_text.slice(0, 260) + '...') : log.prompt_text) +
            '</div>') : '';

          const respSnippet = log.response_text ?
            ('<div style="margin-top:6px;padding:7px 10px;background:rgba(16,185,129,0.05);border:1px solid rgba(16,185,129,0.18);border-radius:6px;font-size:11.5px;color:#cbd5e1;">' +
              '<span style="color:#34d399;font-weight:700;">🤖 Trả lời AI: </span>' +
              htmlEscape(log.response_text.length > 220 ? (log.response_text.slice(0, 220) + '...') : log.response_text) +
            '</div>') : '';

          const errBlock = isErr && log.error_message ?
            ('<div style="margin-top:6px;padding:8px;background:rgba(239, 68, 68, 0.1);border-radius:6px;font-family:monospace;font-size:11.5px;color:#fca5a5;white-space:pre-wrap;">' +
             htmlEscape(log.error_message) + '</div>') : '';

          html += '<div style="background:' + bgCol + ';border:1px solid ' + borderCol + ';border-radius:8px;padding:11px 14px;">' +
            '<div style="display:flex;justify-content:space-between;align-items:center;font-size:12px;flex-wrap:wrap;gap:8px;">' +
              '<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">' +
                statusBadge +
                empBadge +
                '<span class="badge ' + provBadge + '">' + htmlEscape(log.provider) + '</span>' +
                '<strong style="font-family:monospace;">' + formatLogModelTruth(log) + '</strong>' +
                (log.account ? ('<span style="color:var(--muted);font-size:11px;">(' + htmlEscape(log.account) + ')</span>') : '') +
              '</div>' +
              '<div style="display:flex;align-items:center;gap:10px;">' +
                '<span style="color:var(--muted);font-size:11px;">' +
                  log.latency_ms + 'ms' +
                  (log.total_tokens ? (' | <strong style="color:#38bdf8;">' + log.total_tokens + ' tok</strong>') : '') +
                  ' | ' + (log.timestamp || '').replace('T', ' ').slice(0, 19) +
                '</span>' +
                ((log.prompt_text || log.response_text || log.error_message) ?
                  ('<button style="padding:3px 8px;font-size:11px;" onclick="openPromptDetailModal(' + idx + ')">👁️ Chi tiết</button>') : '') +
              '</div>' +
            '</div>' +
            promptSnippet +
            respSnippet +
            errBlock +
          '</div>';
        }
        html += '</div>';
        box.innerHTML = html;
      } catch (err) {
        box.innerHTML = '<div style="color:#f87171;">Lỗi tải logs: ' + err.message + '</div>';
      }
    }

    function openPromptDetailModal(idx) {
      const log = cachedPromptLogs[idx];
      if (!log) return;
      const empName = log.employee_name || 'Admin / Local';
      document.getElementById('promptModalTitle').textContent = 'Chi Tiết Prompt — ' + empName;
      document.getElementById('promptModalMeta').innerHTML =
        '<span><strong>👤 Nhân viên:</strong> ' + htmlEscape(empName) + '</span>' +
        (log.masked_key ? ('<span>| <strong>🔑 Key:</strong> <code>' + htmlEscape(log.masked_key) + '</code></span>') : '') +
        '<span>| <strong>🤖 Model:</strong> ' + formatLogModelTruth(log) + '</span>' +
        '<span>| <strong>⚡ Token:</strong> ' + (log.total_tokens || 0) + ' (Prompt: ' + (log.prompt_tokens || 0) + ', Output: ' + (log.completion_tokens || 0) + ')</span>' +
        '<span>| <strong>⏱️ Độ trễ:</strong> ' + (log.latency_ms || 0) + 'ms</span>' +
        '<span>| <strong>🕒 Lúc:</strong> ' + htmlEscape((log.timestamp || '').replace('T', ' ').slice(0, 19)) + '</span>';

      document.getElementById('promptModalUserText').textContent = log.prompt_text || '(Không có nội dung prompt)';
      document.getElementById('promptModalAiText').textContent = log.response_text || log.error_message || '(Chưa ghi nhận nội dung phản hồi)';
      document.getElementById('promptDetailModal').classList.add('open');
    }

    function closePromptDetailModal() {
      document.getElementById('promptDetailModal').classList.remove('open');
    }

    function copyPromptModalText(elId) {
      const el = document.getElementById(elId);
      if (!el) return;
      navigator.clipboard.writeText(el.textContent || '');
      alert('Đã sao chép nội dung vào clipboard!');
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
        const sbBadge = document.getElementById('sidebarEndpointBadge');
        if (sbBadge && window.location && window.location.host) {
          sbBadge.textContent = window.location.host + '/v1';
        }
        const originParam = (window.location && window.location.origin) ? ('?origin=' + encodeURIComponent(window.location.origin)) : '';
        const r = await fetch('/v1/oauth/url' + originParam);
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
          msg.innerHTML = '<span style="color:#10b981">✔ Kết nối Antigravity thành công (' + htmlEscape(d.email || '') + ')!</span>';
          document.getElementById('agyCallbackInput').value = '';
          loadQuotaSummary(true);
          loadProvidersAndAccounts();
        } else {
          msg.innerHTML = '<span style="color:#f87171">❌ Lỗi: ' + (d.error ? d.error.message : d.message) + '</span>';
        }
      } catch (err) {
        msg.innerHTML = '<span style="color:#f87171">Lỗi: ' + err.message + '</span>';
      }
    }

    let claudeVerifier = localStorage.getItem('omni_claude_verifier') || '';
    async function loadClaudeUrl() {
      try {
        const r = await fetch('/v1/oauth/claude/url');
        const d = await r.json();
        claudeVerifier = d.code_verifier;
        localStorage.setItem('omni_claude_verifier', d.code_verifier);
        document.getElementById('claudeOAuthUrlBox').textContent = d.auth_url;
        document.getElementById('claudeOpenBrowserBtn').href = d.auth_url;
      } catch (err) {
        document.getElementById('claudeOAuthUrlBox').textContent = 'Lỗi: ' + err.message;
      }
    }

    async function connectClaude() {
      const code = document.getElementById('claudeCodeInput').value.trim();
      const direct = document.getElementById('claudeDirectInput').value.trim();
      const label = document.getElementById('claudeLabelInput').value.trim();
      const msg = document.getElementById('claudeMsg');
      const input = direct || code;
      if (!input) { msg.innerHTML = '<span style="color:#f87171">Vui lòng nhập code#state hoặc token</span>'; return; }
      msg.textContent = 'Đang kết nối Claude...';
      try {
        const verifierToSend = claudeVerifier || localStorage.getItem('omni_claude_verifier') || '';
        const r = await fetch('/v1/oauth/claude/exchange', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({input: input, code_verifier: verifierToSend, label: label})
        });
        const d = await r.json();
        if (r.ok) {
          msg.innerHTML = '<span style="color:#10b981">✔ Kết nối Claude Code thành công (' + htmlEscape(d.email || d.id || '') + ')!</span>';
          document.getElementById('claudeCodeInput').value = '';
          document.getElementById('claudeDirectInput').value = '';
          loadClaudeUrl();
          loadQuotaSummary(true);
          loadProvidersAndAccounts();
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
          loadProvidersAndAccounts();
        } else {
          msg.innerHTML = '<span style="color:#f87171">❌ Lỗi: ' + (d.error ? d.error.message : d.message) + '</span>';
        }
      } catch (err) {
        msg.innerHTML = '<span style="color:#f87171">Lỗi: ' + err.message + '</span>';
      }
    }

    async function loadProvidersAndAccounts() {
      const box = document.getElementById('providersAccountsListBox');
      if (!box) return;
      try {
        const r = await fetch('/v1/providers');
        if (r.status === 401) { handleSessionExpired(); return; }
        const d = await r.json();
        if (d.routing_strategy) {
          const sb = document.getElementById('activeStrategyBadge');
          const sel = document.getElementById('strategySelect');
          if (sb) sb.textContent = d.routing_strategy;
          if (sel) sel.value = d.routing_strategy;
        }
        const accs = d.accounts || [];
        if (accs.length === 0) {
          box.innerHTML = '<div style="color:var(--muted);padding:12px;">Chưa có tài khoản nào được kết nối.</div>';
          return;
        }
        let html = '<div style="display:flex;flex-direction:column;gap:8px;">';
        for (const a of accs) {
          const provBadge = a.provider === 'antigravity' ? '<span class="badge badge-antigravity">Google Antigravity</span>'
            : (a.provider === 'claude-code' ? '<span class="badge badge-purple">Claude Code</span>' : '<span class="badge badge-green">Alibaba Token Plan</span>');
          html += '<div style="display:flex;justify-content:space-between;align-items:center;padding:10px 14px;background:var(--card-inner);border:1px solid var(--border);border-radius:8px;flex-wrap:wrap;gap:10px;">' +
            '<div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;">' +
              provBadge +
              '<strong style="color:#38bdf8;font-family:monospace;font-size:13px;">' + htmlEscape(a.email || a.label || a.id) + '</strong>' +
              (a.project_id ? '<code style="font-size:11px;color:#94a3b8;background:#0b0f17;padding:2px 6px;border-radius:4px;">Project: ' + htmlEscape(a.project_id) + '</code>' : '') +
              '<span style="font-size:11.5px;color:#34d399;">🟢 ' + (a.requests_ok || 0) + ' req (' + Number(a.total_tokens || 0).toLocaleString() + ' tokens)</span>' +
            '</div>' +
            '<button style="padding:4px 10px;font-size:11.5px;color:#f87171;" onclick="deleteConnectedAccount(\'' + htmlEscape(a.id) + '\', \'' + htmlEscape(a.email || a.id) + '\')">🗑️ Xoá tài khoản</button>' +
          '</div>';
        }
        html += '</div>';
        box.innerHTML = html;
      } catch (err) {
        box.innerHTML = '<div style="color:#f87171;">Lỗi tải danh sách: ' + err.message + '</div>';
      }
    }

    async function deleteConnectedAccount(id, email) {
      if (!confirm('Bạn có chắc chắn muốn xoá tài khoản ' + email + ' khỏi Gateway?')) return;
      try {
        await fetch('/v1/oauth/account?id=' + encodeURIComponent(id) + '&email=' + encodeURIComponent(email), {method: 'DELETE'});
        loadQuotaSummary(true);
        loadProvidersAndAccounts();
      } catch (err) {
        alert('Lỗi xoá tài khoản: ' + err.message);
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
        if (r.status === 401) { handleSessionExpired(); return; }
        const d = await r.json();
        renderEndpointStatus(d);
      } catch (e) {
        console.error('Error loading endpoint status:', e);
      }
    }

    function renderEndpointStatus(d) {
      if (!d) return;
      currentEndpointData = d;
      document.getElementById('epActivePublicURL').textContent = d.active_public_url || 'https://omni.bucloud.vip/v1';
      document.getElementById('epActiveLocalURL').textContent = d.active_local_url || 'http://localhost:8787/v1';
      document.getElementById('epHostID').textContent = d.host_id || '569fb55e';
      document.getElementById('epLanServerURL').textContent = d.lan_server_url || '127.0.0.1:8787/v1';
      document.getElementById('epActiveTunnelsCount').textContent = (d.active_tunnels_count || 0) + '/' + (d.total_tunnels_count || 4) + ' đang hoạt động';

      const cfg = d.config || {};
      document.getElementById('epCloudSubUrl').textContent = cfg.public_url || 'https://omni.bucloud.vip/v1';
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
      const pubUrl = d.active_public_url || 'https://omni.bucloud.vip/v1';
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
      const curr = (currentEndpointData && currentEndpointData.config && currentEndpointData.config.public_url) || 'https://omni.bucloud.vip/v1';
      const val = prompt('Nhập địa chỉ Endpoint Công khai trên VPS của bạn:\n(Ví dụ: https://omni.bucloud.vip/v1 hoặc http://IP-VPS:8787/v1)', curr);
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

    // --- VECTOR DATABASE & LONG-TERM AI MEMORY FLOW ---

    async function loadVectorDB() {
      const box = document.getElementById('vectorMemoriesListBox');
      try {
        const r = await fetch('/v1/vector');
        if (r.status === 401) { handleSessionExpired(); return; }
        const d = await r.json();
        renderVectorDBStatus(d);
        renderVectorMemoriesList(d.memories || []);
      } catch (err) {
        if (box) box.innerHTML = '<div style="color:#f87171;">Lỗi tải Vector DB: ' + htmlEscape(err.message) + '</div>';
      }
    }

    function renderVectorDBStatus(d) {
      if (!d) return;
      const elTotal = document.getElementById('vecStatTotal');
      const elPinned = document.getElementById('vecStatPinned');
      const elAuto = document.getElementById('vecStatAuto');
      const elHits = document.getElementById('vecStatRagHits');
      const elBadge = document.getElementById('vecEngineBadge');

      if (elTotal) elTotal.textContent = Number(d.total_memories || 0).toLocaleString();
      if (elPinned) elPinned.textContent = Number(d.pinned_count || 0).toLocaleString();
      if (elAuto) elAuto.textContent = Number(d.auto_learned_count || 0).toLocaleString();
      if (elHits && d.config) elHits.textContent = Number(d.config.total_rag_hits || 0).toLocaleString();
      if (elBadge && d.engine) elBadge.textContent = '🟢 ' + d.engine;

      if (d.config) {
        const learnBtn = document.getElementById('toggleVecAutoLearnBtn');
        const ragBtn = document.getElementById('toggleVecAutoRagBtn');
        if (learnBtn) {
          if (d.config.auto_learn_prompts !== false) {
            learnBtn.classList.add('on');
            learnBtn.innerHTML = '🧠 <span>Đã bật</span>';
          } else {
            learnBtn.classList.remove('on');
            learnBtn.innerHTML = '🧠 <span>Đã tắt</span>';
          }
        }
        if (ragBtn) {
          if (d.config.auto_inject_rag !== false) {
            ragBtn.classList.add('on');
            ragBtn.innerHTML = '🚀 <span>Đã bật</span>';
          } else {
            ragBtn.classList.remove('on');
            ragBtn.innerHTML = '🚀 <span>Đã tắt</span>';
          }
        }
        const topKInp = document.getElementById('vecTopKInput');
        const simInp = document.getElementById('vecMinSimInput');
        if (topKInp && d.config.top_k) topKInp.value = d.config.top_k;
        if (simInp && d.config.min_similarity) simInp.value = d.config.min_similarity;
      }
    }

    function renderVectorMemoriesList(memories) {
      const box = document.getElementById('vectorMemoriesListBox');
      if (!box) return;
      if (!memories || memories.length === 0) {
        box.innerHTML = '<div style="color:var(--muted);padding:14px;background:var(--card-inner);border-radius:8px;">' +
          'Kho Vector Database hiện chưa có bản ghi nào. Bạn có thể thêm <strong>Tri thức cốt lõi</strong> ở trên hoặc để nhân viên trò chuyện với AI để hệ thống tự động học và ghi nhớ!</div>';
        return;
      }

      let html = '<div style="display:flex;flex-direction:column;gap:10px;">';
      for (const m of memories) {
        const typeBadge = m.pinned
          ? '<span class="badge badge-purple" style="background:#581c87;color:#e9d5ff;">📌 Tri thức Ghim (' + htmlEscape(m.category || 'knowledge') + ')</span>'
          : '<span class="badge badge-antigravity">🤖 Tự học từ Prompt</span>';

        const simBadge = m.similarity
          ? ('<span class="badge badge-green">🎯 Cosine Similarity: ' + (m.similarity * 100).toFixed(1) + '%</span>')
          : '';

        const empBadge = m.employee_name
          ? ('<span style="color:#38bdf8;font-size:11.5px;">👤 ' + htmlEscape(m.employee_name) + '</span>')
          : '';

        html += '<div style="background:var(--card-inner);border:1px solid rgba(255,255,255,0.08);border-radius:8px;padding:12px 14px;">' +
          '<div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;margin-bottom:6px;">' +
            '<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">' +
              typeBadge +
              simBadge +
              '<strong>' + htmlEscape(m.title || 'Vector Memory') + '</strong>' +
              empBadge +
            '</div>' +
            '<div style="display:flex;align-items:center;gap:10px;">' +
              '<span style="color:var(--muted);font-size:11px;">Hits: ' + (m.hit_count || 1) + ' | ' + htmlEscape((m.created_at || '').replace('T', ' ').slice(0, 19)) + '</span>' +
              '<button onclick="deleteVectorMemoryUI(\'' + htmlEscape(m.id) + '\')" style="color:#f87171;padding:3px 8px;font-size:11px;">Xoá</button>' +
            '</div>' +
          '</div>' +
          (m.prompt_text ? ('<div style="font-size:12px;color:#cbd5e1;background:#0a0e17;padding:8px 10px;border-radius:6px;border:1px solid #1e293b;margin-top:6px;"><strong style="color:#38bdf8;">Câu hỏi / Ngữ cảnh:</strong> ' + htmlEscape(m.prompt_text) + '</div>') : '') +
          (m.answer_text ? ('<div style="font-size:12px;color:#e2e8f0;background:rgba(16,185,129,0.06);padding:8px 10px;border-radius:6px;border:1px solid rgba(16,185,129,0.2);margin-top:6px;white-space:pre-wrap;"><strong style="color:#34d399;">Kiến thức / Giải pháp đã nhớ:</strong> ' + htmlEscape(m.answer_text.length > 450 ? (m.answer_text.slice(0, 450) + '...') : m.answer_text) + '</div>') : '') +
        '</div>';
      }
      html += '</div>';
      box.innerHTML = html;
    }

    async function saveVectorDBConfigUI() {
      const autoLearn = document.getElementById('toggleVecAutoLearnBtn').classList.contains('on');
      const autoRag = document.getElementById('toggleVecAutoRagBtn').classList.contains('on');
      const topK = parseInt(document.getElementById('vecTopKInput').value || '3', 10);
      const minSim = parseFloat(document.getElementById('vecMinSimInput').value || '0.24');
      const msg = document.getElementById('vecConfigMsg');

      try {
        const r = await fetch('/v1/vector', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            action: 'config',
            enabled: true,
            auto_learn_prompts: autoLearn,
            auto_inject_rag: autoRag,
            top_k: topK,
            min_similarity: minSim
          })
        });
        const d = await r.json();
        renderVectorDBStatus(d);
        if (msg) msg.innerHTML = '<span style="color:#34d399;">✔ Đã lưu cấu hình Vector Database!</span>';
      } catch (err) {
        if (msg) msg.innerHTML = '<span style="color:#f87171;">Lỗi: ' + htmlEscape(err.message) + '</span>';
      }
    }

    async function addPinnedVectorMemory() {
      const title = document.getElementById('vecAddTitle').value.trim();
      const category = document.getElementById('vecAddCategory').value;
      const promptText = document.getElementById('vecAddPrompt').value.trim();
      const answerText = document.getElementById('vecAddAnswer').value.trim();

      if (!promptText && !answerText) {
        alert('Vui lòng nhập nội dung câu hỏi/tình huống hoặc kiến thức cần ghi nhớ!');
        return;
      }

      try {
        const r = await fetch('/v1/vector', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            action: 'add',
            title: title,
            category: category,
            prompt_text: promptText,
            answer_text: answerText,
            pinned: true
          })
        });
        if (r.ok) {
          document.getElementById('vecAddTitle').value = '';
          document.getElementById('vecAddPrompt').value = '';
          document.getElementById('vecAddAnswer').value = '';
          loadVectorDB();
        } else {
          const d = await r.json();
          alert('Lỗi: ' + (d.error ? d.error.message : 'Không thể lưu'));
        }
      } catch (err) {
        alert('Lỗi kết nối: ' + err.message);
      }
    }

    async function searchVectorMemoriesUI() {
      const q = document.getElementById('vecSearchQueryInput').value.trim();
      if (!q) {
        loadVectorDB();
        return;
      }
      try {
        const r = await fetch('/v1/vector', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            action: 'search',
            query: q,
            top_k: 15
          })
        });
        const d = await r.json();
        renderVectorMemoriesList(d.matches || []);
      } catch (err) {
        alert('Lỗi tìm kiếm vector: ' + err.message);
      }
    }

    async function deleteVectorMemoryUI(id) {
      if (!confirm('Bạn có muốn xoá bản ghi Vector Memory này?')) return;
      await fetch('/v1/vector?id=' + encodeURIComponent(id), {method: 'DELETE'});
      loadVectorDB();
    }

    async function clearAutoVectorMemories() {
      if (!confirm('Xoá toàn bộ các Vector tự học (nhưng vẫn giữ nguyên các Tri thức Cốt lõi đã ghim)?')) return;
      await fetch('/v1/vector?id=all_auto', {method: 'DELETE'});
      loadVectorDB();
    }

    // --- MULTI-MODEL COMBOS & COLLABORATIVE PIPELINE FLOW ---
    let currentCombosList = [];

    const comboSelectModelGroups = [
      {
        label: 'Google Antigravity (Gen 4 Carbon, Argon & Gen 3)',
        models: [
          {id: 'antigravity/gemini-4-carbon-high', name: 'antigravity/gemini-4-carbon-high (Carbon 4 High Thinking)'},
          {id: 'antigravity/gemini-4-carbon-medium', name: 'antigravity/gemini-4-carbon-medium'},
          {id: 'antigravity/gemini-4-carbon-low', name: 'antigravity/gemini-4-carbon-low (Carbon 4 Low - Fast)'},
          {id: 'antigravity/gemini-4-argon-high', name: 'antigravity/gemini-4-argon-high (Argon 4 High Thinking)'},
          {id: 'antigravity/gemini-4-argon-medium', name: 'antigravity/gemini-4-argon-medium'},
          {id: 'antigravity/gemini-4-argon-low', name: 'antigravity/gemini-4-argon-low'},
          {id: 'antigravity/gemini-3.1-pro-high', name: 'antigravity/gemini-3.1-pro-high'},
          {id: 'antigravity/gemini-3.8-flash-high', name: 'antigravity/gemini-3.8-flash-high'},
          {id: 'antigravity/claude-opus-4-6-thinking', name: 'antigravity/claude-opus-4-6-thinking'}
        ]
      },
      {
        label: 'Claude Code CLI (Opus 5.5, Fable 5.1, Sonnet 5.5)',
        models: [
          {id: 'claude-code/claude-opus-5-5', name: 'claude-code/claude-opus-5-5 (Opus 5.5 Default)'},
          {id: 'claude-code/claude-opus-5-5-thinking', name: 'claude-code/claude-opus-5-5-thinking (Opus 5.5 Extra Effort)'},
          {id: 'claude-code/claude-fable-5-1', name: 'claude-code/claude-fable-5-1 (Fable 5.1)'},
          {id: 'claude-code/claude-sonnet-5-5', name: 'claude-code/claude-sonnet-5-5 (Sonnet 5.5)'},
          {id: 'claude-code/claude-haiku-5-5', name: 'claude-code/claude-haiku-5-5 (Haiku 5.5)'},
          {id: 'claude-code/claude-sonnet-5', name: 'claude-code/claude-sonnet-5 (Sonnet 5)'},
          {id: 'claude-code/claude-opus-5', name: 'claude-code/claude-opus-5 (Opus 5)'},
          {id: 'claude-code/claude-fable-5', name: 'claude-code/claude-fable-5 (Fable 5)'},
          {id: 'claude-code/claude-opus-4-8', name: 'claude-code/claude-opus-4-8 (Opus 4.8)'},
          {id: 'claude-code/claude-opus-4-7', name: 'claude-code/claude-opus-4-7 (Opus 4.7)'},
          {id: 'claude-code/claude-opus-4-6-thinking', name: 'claude-code/claude-opus-4-6-thinking (Opus 4.6 Thinking)'},
          {id: 'claude-code/claude-sonnet-4-6', name: 'claude-code/claude-sonnet-4-6 (Sonnet 4.6)'},
          {id: 'claude-code/claude-haiku-4-5', name: 'claude-code/claude-haiku-4-5 (Haiku 4.5)'}
        ]
      },
      {
        label: 'Alibaba Cloud Token Plan (⚡ Night Discount 40%-50% Off)',
        models: [
          {id: 'alibaba/qwen3.8-max', name: 'alibaba/qwen3.8-max (⚡ 40% Off Nights)'},
          {id: 'alibaba/qwen3.8-flash', name: 'alibaba/qwen3.8-flash (⚡ 40% Off Nights)'},
          {id: 'alibaba/deepseek-v4.1-flash-1017', name: 'alibaba/deepseek-v4.1-flash-1017 (⚡ 50% Off Nights)'},
          {id: 'alibaba/deepseek-v4.1-flash', name: 'alibaba/deepseek-v4.1-flash (⚡ 50% Off Nights)'},
          {id: 'alibaba/deepseek-v4-pro-0813', name: 'alibaba/deepseek-v4-pro-0813 (⚡ Night 50% Off)'},
          {id: 'alibaba/deepseek-v4-flash-0731', name: 'alibaba/deepseek-v4-flash-0731 (⚡ Night 50% Off)'},
          {id: 'alibaba/deepseek-v4-pro', name: 'alibaba/deepseek-v4-pro'},
          {id: 'alibaba/auto', name: 'alibaba/auto (Qwen Auto Reasoning)'},
          {id: 'alibaba/qwen3.7-max', name: 'alibaba/qwen3.7-max'},
          {id: 'alibaba/qwen3.7-plus', name: 'alibaba/qwen3.7-plus'},
          {id: 'alibaba/qwen3.6-flash', name: 'alibaba/qwen3.6-flash'},
          {id: 'alibaba/glm-5.3', name: 'alibaba/glm-5.3'},
          {id: 'alibaba/glm-5.2', name: 'alibaba/glm-5.2'},
          {id: 'alibaba/qwen3-coder-plus', name: 'alibaba/qwen3-coder-plus'}
        ]
      }
    ];

    function populateComboSelects() {
      const selects = ['comboStep1Select', 'comboStep2Select', 'comboStep3Select', 'comboStep4Select'];
      selects.forEach((selId, idx) => {
        const el = document.getElementById(selId);
        if (!el || el.options.length > 0) return;
        if (idx >= 2) {
          const emptyOpt = document.createElement('option');
          emptyOpt.value = '';
          emptyOpt.textContent = '-- Không dùng / Bỏ qua bước này --';
          el.appendChild(emptyOpt);
        }
        comboSelectModelGroups.forEach(group => {
          const og = document.createElement('optgroup');
          og.label = group.label;
          group.models.forEach(m => {
            const opt = document.createElement('option');
            opt.value = m.id;
            opt.textContent = m.name;
            og.appendChild(opt);
          });
          el.appendChild(og);
        });
      });
      // Default selections
      const s1 = document.getElementById('comboStep1Select');
      const s2 = document.getElementById('comboStep2Select');
      const s3 = document.getElementById('comboStep3Select');
      if (s1 && !s1.value) s1.value = 'claude-code/claude-opus-5-5';
      if (s2 && !s2.value) s2.value = 'antigravity/gemini-4-carbon-high';
      if (s3 && !s3.value) s3.value = 'alibaba/qwen3.8-max';
    }

    async function loadCombosView() {
      populateComboSelects();
      const box = document.getElementById('combosGridBox');
      try {
        const r = await fetch('/v1/combos');
        if (r.status === 401) { handleSessionExpired(); return; }
        const d = await r.json();
        currentCombosList = d.combos || [];
        renderCombosGrid(currentCombosList);
      } catch (err) {
        if (box) box.innerHTML = '<div style="color:#f87171;">Lỗi tải danh sách Combo: ' + htmlEscape(err.message) + '</div>';
      }
    }

    function renderCombosGrid(combos) {
      const box = document.getElementById('combosGridBox');
      if (!box) return;
      if (!combos || combos.length === 0) {
        box.innerHTML = '<div style="color:var(--muted);">Chưa có Combo nào.</div>';
        return;
      }

      let html = '';
      combos.forEach((c, idx) => {
        const mode = c.mode || 'failover';
        let modeBadge = '<span class="badge" style="background:rgba(245,158,11,0.16);color:#fbbf24;border:1px solid rgba(245,158,11,0.35);">🛡️ Failover Tức Thì</span>';
        if (mode === 'pipeline') {
          modeBadge = '<span class="badge" style="background:rgba(168,85,247,0.18);color:#c084fc;border:1px solid rgba(168,85,247,0.4);">🔗 Pipeline Nối Tiếp (Kiến trúc ➔ Code)</span>';
        } else if (mode === 'consensus') {
          modeBadge = '<span class="badge" style="background:rgba(56,189,248,0.18);color:#38bdf8;border:1px solid rgba(56,189,248,0.4);">🧠 Hội Đồng Song Song (Mixture of Agents)</span>';
        } else if (mode === 'night-saver') {
          modeBadge = '<span class="badge" style="background:rgba(16,185,129,0.18);color:#34d399;border:1px solid rgba(16,185,129,0.4);">⚡ Night Discount 40-50% Off</span>';
        }

        const models = c.models || [];
        const roles = c.roles || [];
        let stepsHTML = '<div style="display:flex;flex-direction:column;gap:6px;margin-top:10px;background:#0a0e17;padding:10px;border-radius:8px;border:1px solid #1e293b;">';
        models.forEach((m, mIdx) => {
          const lowerM = m.toLowerCase();
          let provColor = '#fb923c';
          let provLabel = 'Antigravity';
          if (lowerM.startsWith('claude-code/')) {
            provColor = '#c084fc';
            provLabel = 'Claude Code';
          } else if (lowerM.startsWith('alibaba/')) {
            provColor = '#38bdf8';
            provLabel = 'Alibaba';
          }
          const roleLabel = roles[mIdx] || ('Bước ' + (mIdx + 1));
          const nightTag = (lowerM.includes('qwen3.8-max') || lowerM.includes('qwen3.8-flash'))
            ? ' <span style="color:#34d399;font-size:10.5px;font-weight:700;">⚡40% Off</span>'
            : ((lowerM.includes('deepseek-v4.1-flash') || lowerM.includes('0813') || lowerM.includes('0731'))
                ? ' <span style="color:#34d399;font-size:10.5px;font-weight:700;">⚡50% Off</span>' : '');
          stepsHTML += '<div style="display:flex;justify-content:space-between;align-items:center;font-size:11.5px;gap:6px;">' +
            '<span style="color:var(--muted);">' + htmlEscape(roleLabel) + ':</span>' +
            '<span style="font-family:monospace;font-weight:700;color:' + provColor + ';">' + htmlEscape(m) + nightTag + '</span>' +
          '</div>';
        });
        stepsHTML += '</div>';

        const handoffHTML = c.handoff_prompt
          ? '<div style="margin-top:8px;font-size:11px;color:#94a3b8;background:rgba(168,85,247,0.07);border:1px dashed rgba(168,85,247,0.28);padding:7px 9px;border-radius:6px;">💬 <strong>Chỉ dẫn phối hợp:</strong> ' + htmlEscape(c.handoff_prompt) + '</div>'
          : '';

        html += '<div style="background:#121722;border:1px solid #242c3d;border-radius:12px;padding:15px;display:flex;flex-direction:column;justify-content:space-between;">' +
          '<div>' +
            '<div style="display:flex;justify-content:space-between;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:8px;">' +
              modeBadge +
              '<code style="font-size:12.5px;font-weight:700;color:#fff;background:#1e293b;padding:3px 8px;border-radius:6px;">' + htmlEscape(c.id) + '</code>' +
            '</div>' +
            '<div style="font-weight:700;font-size:14px;color:#f1f5f9;margin-bottom:4px;">' + htmlEscape(c.name || c.id) + '</div>' +
            '<div style="font-size:12px;color:var(--muted);line-height:1.4;">' + htmlEscape(c.description || '') + '</div>' +
            stepsHTML +
            handoffHTML +
          '</div>' +
          '<div style="display:flex;justify-content:space-between;align-items:center;margin-top:12px;padding-top:10px;border-top:1px solid rgba(255,255,255,0.06);gap:6px;flex-wrap:wrap;">' +
            '<div style="display:flex;gap:6px;">' +
              '<button style="padding:5px 10px;font-size:11.5px;" onclick="testComboInPlayground(\'' + htmlEscape(c.id) + '\')">🧪 Chạy thử</button>' +
              '<button style="padding:5px 10px;font-size:11.5px;" onclick="editComboInBuilder(' + idx + ')">✏️ Tuỳ chỉnh</button>' +
            '</div>' +
            '<div style="display:flex;gap:6px;">' +
              '<button style="padding:5px 10px;font-size:11.5px;" onclick="navigator.clipboard.writeText(\'' + htmlEscape(c.id) + '\')">📋 Copy ID</button>' +
              (!c.built_in ? '<button style="padding:5px 9px;font-size:11.5px;color:#f87171;" onclick="deleteComboFromUI(\'' + htmlEscape(c.id) + '\')">🗑️</button>' : '') +
            '</div>' +
          '</div>' +
        '</div>';
      });
      box.innerHTML = html;
    }

    function editComboInBuilder(idx) {
      const c = currentCombosList[idx];
      if (!c) return;
      populateComboSelects();
      document.getElementById('comboIdInput').value = c.id || '';
      document.getElementById('comboModeSelect').value = c.mode || 'failover';
      document.getElementById('comboNameInput').value = c.name || '';
      document.getElementById('comboDescInput').value = c.description || '';
      document.getElementById('comboHandoffInput').value = c.handoff_prompt || '';

      const models = c.models || [];
      ['comboStep1Select', 'comboStep2Select', 'comboStep3Select', 'comboStep4Select'].forEach((selId, i) => {
        const el = document.getElementById(selId);
        if (el) el.value = models[i] || '';
      });
      document.getElementById('comboIdInput').scrollIntoView({behavior: 'smooth', block: 'center'});
    }

    function resetComboBuilderForm() {
      document.getElementById('comboIdInput').value = '';
      document.getElementById('comboModeSelect').value = 'pipeline';
      document.getElementById('comboNameInput').value = '';
      document.getElementById('comboDescInput').value = '';
      document.getElementById('comboHandoffInput').value = '';
      document.getElementById('comboSaveMsg').textContent = '';
    }

    async function saveComboFromUI() {
      const id = document.getElementById('comboIdInput').value.trim();
      const mode = document.getElementById('comboModeSelect').value;
      const name = document.getElementById('comboNameInput').value.trim();
      const desc = document.getElementById('comboDescInput').value.trim();
      const handoff = document.getElementById('comboHandoffInput').value.trim();
      const msg = document.getElementById('comboSaveMsg');

      if (!id) {
        if (msg) msg.innerHTML = '<span style="color:#f87171;">❌ Vui lòng nhập Mã ID Combo (ví dụ: auto/my-combo)</span>';
        return;
      }

      const models = [
        document.getElementById('comboStep1Select').value,
        document.getElementById('comboStep2Select').value,
        document.getElementById('comboStep3Select').value,
        document.getElementById('comboStep4Select').value
      ].filter(Boolean);

      if (models.length === 0) {
        if (msg) msg.innerHTML = '<span style="color:#f87171;">❌ Vui lòng chọn ít nhất 1 Model</span>';
        return;
      }

      try {
        const r = await fetch('/v1/combos', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            id: id,
            name: name || id,
            mode: mode,
            description: desc || ('Combo ' + mode + ' kết hợp ' + models.join(' ➔ ')),
            models: models,
            handoff_prompt: handoff
          })
        });
        const d = await r.json();
        if (r.ok) {
          if (msg) msg.innerHTML = '<span style="color:#34d399;">✔ Đã lưu Combo <strong>' + htmlEscape(id) + '</strong> thành công!</span>';
          currentCombosList = d.combos || [];
          renderCombosGrid(currentCombosList);
          loadModelsDiagnostics();
        } else {
          if (msg) msg.innerHTML = '<span style="color:#f87171;">❌ ' + htmlEscape(d.error ? d.error.message : 'Lỗi lưu Combo') + '</span>';
        }
      } catch (err) {
        if (msg) msg.innerHTML = '<span style="color:#f87171;">❌ Lỗi kết nối: ' + htmlEscape(err.message) + '</span>';
      }
    }

    async function deleteComboFromUI(id) {
      if (!confirm('Bạn có muốn xoá / đặt lại Combo ' + id + ' không?')) return;
      await fetch('/v1/combos?id=' + encodeURIComponent(id), {method: 'DELETE'});
      loadCombosView();
    }

    function testComboInPlayground(comboId) {
      switchNav('view-playground');
      const sel = document.getElementById('testModelSelect');
      if (sel) {
        let found = false;
        for (let i = 0; i < sel.options.length; i++) {
          if (sel.options[i].value === comboId) {
            sel.selectedIndex = i;
            found = true;
            break;
          }
        }
        if (!found) {
          const opt = document.createElement('option');
          opt.value = comboId;
          opt.textContent = comboId + ' (Custom Combo)';
          sel.appendChild(opt);
          sel.value = comboId;
        }
      }
    }

    // --- PARALLEL SUB-AGENTS SWARM & 50-ACCOUNT POOL FLOW ---
    let saPollingTimer = null;

    async function loadSubAgentsView() {
      try {
        const r = await fetch('/v1/subagents');
        if (r.status === 401) { handleSessionExpired(); return; }
        const d = await r.json();
        const agyEl = document.getElementById('saKpiAgyCount');
        const rdyEl = document.getElementById('saKpiReadyCount');
        const actEl = document.getElementById('saKpiActiveNow');
        const totEl = document.getElementById('saKpiTotalRun');
        if (agyEl) agyEl.textContent = (d.antigravity_accounts || 0) + ' / ' + (d.max_parallel_target || 50);
        if (rdyEl) rdyEl.textContent = d.ready_accounts || 0;
        if (actEl) actEl.textContent = d.active_sub_agents || 0;
        if (totEl) totEl.textContent = d.total_sub_agents_run || 0;

        renderSubAgentSlots(d.account_slots || []);
        renderAutomationsList(d.automations || []);
        renderVaultHistory(d.recent_swarms || []);

        if (d.recent_swarms && d.recent_swarms.length > 0 && !document.getElementById('saSwarmResultsBox').innerHTML.trim()) {
          renderSwarmSummary(d.recent_swarms[0]);
        }
      } catch (err) {
        console.error('Failed to load subagents status:', err);
      }
    }

    function renderAutomationsList(automations) {
      const box = document.getElementById('saAutomationsListBox');
      if (!box) return;
      if (!automations || automations.length === 0) {
        box.innerHTML = '<div style="color:var(--muted);padding:10px 0;">Chưa có lịch tự động hóa nào. Bấm <strong>"+ Tạo Lịch Tự Động Hóa Mới"</strong> để kích hoạt chạy đêm 40%-50% Off.</div>';
        return;
      }
      let html = '<div style="display:grid;grid-template-columns:repeat(auto-fill, minmax(320px, 1fr));gap:12px;">';
      automations.forEach(a => {
        const isEn = a.enabled;
        const statusBadge = isEn
          ? '<span class="badge badge-green">🟢 Đang Bật</span>'
          : '<span class="badge" style="background:#1e293b;color:#94a3b8;">⏸️ Tạm Dừng</span>';

        let winLabel = a.schedule_window;
        if (winLabel === 'night-22h') winLabel = '🌙 22:00 Mỗi Đêm (⚡ 40% Off)';
        else if (winLabel === 'nightly_21h') winLabel = '🌙 21:00 Mỗi Đêm (⚡ 40% Off)';
        else if (winLabel === 'interval_12h') winLabel = '⏰ Mỗi 12 Giờ (2 lần/ngày)';
        else if (winLabel === 'interval_6h') winLabel = '⚡ Mỗi 6 Giờ (4 lần/ngày)';

        html += '<div style="background:#111622;border:1px solid ' + (isEn ? 'rgba(56,189,248,0.28)' : '#242c3d') + ';border-radius:10px;padding:12px;display:flex;flex-direction:column;justify-content:space-between;">' +
          '<div>' +
            '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px;">' +
              '<strong style="font-size:13px;color:#fff;">' + htmlEscape(a.name) + '</strong>' +
              statusBadge +
            '</div>' +
            '<div style="font-size:11.5px;color:#38bdf8;font-weight:600;margin-bottom:6px;">' + htmlEscape(winLabel) + ' • ' + (a.agent_count || 8) + ' Sub-Agents</div>' +
            '<div style="font-size:12px;color:var(--muted);margin-bottom:8px;line-height:1.4;">' + htmlEscape(a.master_goal || '') + '</div>' +
            '<div style="font-size:11px;color:#94a3b8;background:#090d16;padding:6px 8px;border-radius:6px;margin-bottom:10px;">' +
              '<div>📊 Đã chạy: <strong>' + (a.total_runs || 0) + ' lần</strong>' + (a.last_run_at ? (' | Lần cuối: ' + htmlEscape(a.last_run_at.replace('T', ' ').substring(0, 19))) : '') + '</div>' +
              (a.last_status ? ('<div style="color:#34d399;margin-top:2px;">Trạng thái: ' + htmlEscape(a.last_status) + '</div>') : '') +
            '</div>' +
          '</div>' +
          '<div style="display:flex;justify-content:space-between;align-items:center;border-top:1px solid rgba(255,255,255,0.06);padding-top:8px;gap:6px;">' +
            '<button style="padding:4px 8px;font-size:11px;" onclick="toggleAutomationUI(\'' + htmlEscape(a.id) + '\')">' + (isEn ? '⏸️ Tắt' : '▶️ Bật') + '</button>' +
            '<div style="display:flex;gap:5px;">' +
              '<button class="btn-primary" style="padding:4px 9px;font-size:11px;" onclick="runAutomationUI(\'' + htmlEscape(a.id) + '\')">⚡ Chạy Ngay</button>' +
              '<button style="padding:4px 7px;font-size:11px;color:#f87171;" onclick="deleteAutomationUI(\'' + htmlEscape(a.id) + '\')">🗑️</button>' +
            '</div>' +
          '</div>' +
        '</div>';
      });
      html += '</div>';
      box.innerHTML = html;
    }

    function toggleNewAutomationForm() {
      const el = document.getElementById('newAutomationFormBox');
      if (el) el.style.display = (el.style.display === 'none' || !el.style.display) ? 'block' : 'none';
    }

    async function submitCreateAutomation() {
      const name = document.getElementById('autoSchedNameInput').value.trim();
      const goal = document.getElementById('autoSchedGoalInput').value.trim();
      const win = document.getElementById('autoSchedWindowSelect').value;
      const count = parseInt(document.getElementById('autoSchedCountInput').value || '8', 10);
      const model = document.getElementById('autoSchedModelSelect').value;

      if (!goal) {
        alert('Vui lòng nhập mục tiêu chiến dịch (Master Goal)!');
        return;
      }
      try {
        const r = await fetch('/v1/subagents', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            action: 'create_automation',
            name: name,
            master_goal: goal,
            schedule_type: win,
            agent_count: count,
            model: model
          })
        });
        if (r.ok) {
          toggleNewAutomationForm();
          document.getElementById('autoSchedGoalInput').value = '';
          document.getElementById('autoSchedNameInput').value = '';
          loadSubAgentsView();
        } else {
          const d = await r.json();
          alert('Lỗi tạo lịch: ' + (d.error ? d.error.message : d.message));
        }
      } catch (err) {
        alert('Lỗi kết nối: ' + err.message);
      }
    }

    async function toggleAutomationUI(id) {
      try {
        await fetch('/v1/subagents', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({action: 'toggle_automation', automation_id: id})
        });
        loadSubAgentsView();
      } catch (e) {
        alert('Lỗi bật/tắt lịch: ' + e.message);
      }
    }

    async function runAutomationUI(id) {
      if (!confirm('Khởi chạy chiến dịch tự động này ngay lập tức?')) return;
      alert('Đang kích hoạt chiến dịch...');
      try {
        const r = await fetch('/v1/subagents', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({action: 'run_automation', automation_id: id})
        });
        const d = await r.json();
        if (r.ok && d.summary) {
          alert('✔ Chiến dịch hoàn tất! ' + d.summary.success_count + '/' + d.summary.total_sub_agents + ' Sub-Agents thành công.');
          renderSwarmSummary(d.summary);
          loadSubAgentsView();
        } else {
          alert('Lỗi: ' + (d.error ? d.error.message : 'Thất bại'));
        }
      } catch (e) {
        alert('Lỗi chạy chiến dịch: ' + e.message);
      }
    }

    async function deleteAutomationUI(id) {
      if (!confirm('Bạn có chắc chắn muốn xóa lịch tự động này không?')) return;
      try {
        await fetch('/v1/subagents', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({action: 'delete_automation', automation_id: id})
        });
        loadSubAgentsView();
      } catch (e) {
        alert('Lỗi xóa lịch: ' + e.message);
      }
    }

    function renderVaultHistory(runs) {
      const box = document.getElementById('saVaultHistoryGrid');
      if (!box) return;
      if (!runs || runs.length === 0) {
        box.innerHTML = '<div style="color:var(--muted);padding:8px 0;">Chưa có chiến dịch nào được lưu trong kho. Khi bạn phóng Sub-Agents hoặc chạy lịch đêm, dữ liệu sẽ tự động lưu bền vững tại đây.</div>';
        return;
      }
      let html = '<div style="display:grid;grid-template-columns:repeat(auto-fill, minmax(360px, 1fr));gap:10px;">';
      runs.forEach(r => {
        const ok = r.success_count;
        const tot = r.total_sub_agents;
        const fail = tot - ok;
        const failoverNote = (r.watchdog_failovers > 0) ? (' • <span style="color:#fbbf24;">⚡ ' + r.watchdog_failovers + ' Watchdog Failover</span>') : '';
        html += '<div style="background:#0e131f;border:1px solid #202738;border-radius:9px;padding:12px;display:flex;flex-direction:column;justify-content:space-between;">' +
          '<div>' +
            '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:5px;">' +
              '<code style="color:#38bdf8;font-weight:700;font-size:12px;">' + htmlEscape(r.id) + '</code>' +
              '<span style="font-size:11px;color:var(--muted);">' + htmlEscape(r.timestamp || '') + '</span>' +
            '</div>' +
            '<div style="font-weight:600;font-size:12.5px;color:#f1f5f9;margin-bottom:6px;line-height:1.4;">' + htmlEscape(r.master_goal || 'Không tiêu đề') + '</div>' +
            '<div style="font-size:11.5px;color:var(--muted);margin-bottom:8px;">' +
              'Quản lý: <strong style="color:#fbbf24;">' + htmlEscape(r.manager_model || 'auto') + '</strong>' +
              ' | Thành công: <strong style="color:#34d399;">' + ok + '/' + tot + '</strong>' +
              (fail > 0 ? (' (<span style="color:#f87171;">' + fail + ' lỗi</span>)') : '') +
              failoverNote +
            '</div>' +
            '<div style="display:flex;justify-content:space-between;font-size:11px;color:#94a3b8;background:#080b11;padding:5px 8px;border-radius:6px;margin-bottom:10px;">' +
              '<span>Tokens: <strong style="color:#38bdf8;">' + Number(r.total_tokens || 0).toLocaleString() + '</strong></span>' +
              '<span>Giá trị: <strong style="color:#c084fc;">$' + (r.estimated_value_usd || 0).toFixed(4) + '</strong></span>' +
              '<span>Thời gian: <strong style="color:#34d399;">' + (r.total_wall_time_ms || 0) + ' ms</strong></span>' +
            '</div>' +
          '</div>' +
          '<div style="display:flex;justify-content:space-between;align-items:center;border-top:1px solid rgba(255,255,255,0.06);padding-top:8px;">' +
            '<button style="padding:4px 9px;font-size:11px;" onclick="previewHistoricalSwarm(\'' + htmlEscape(r.id) + '\')">👁️ Xem Kết Quả</button>' +
            '<div style="display:flex;gap:5px;">' +
              '<a href="/v1/subagents?export=' + encodeURIComponent(r.id) + '&amp;format=md" target="_blank" class="btn" style="text-decoration:none;padding:4px 8px;font-size:11px;color:#38bdf8;">📥 Tải .MD</a>' +
              '<a href="/v1/subagents?export=' + encodeURIComponent(r.id) + '&amp;format=json" target="_blank" class="btn" style="text-decoration:none;padding:4px 8px;font-size:11px;color:#34d399;">📥 Tải .JSON</a>' +
            '</div>' +
          '</div>' +
        '</div>';
      });
      html += '</div>';
      box.innerHTML = html;
    }

    function previewHistoricalSwarm(id) {
      fetch('/v1/subagents')
        .then(r => r.json())
        .then(d => {
          const run = (d.recent_swarms || []).find(x => x.id === id);
          if (run) {
            renderSwarmSummary(run);
            document.getElementById('saSwarmResultsBox').scrollIntoView({behavior: 'smooth', block: 'start'});
          }
        });
    }

    function renderSubAgentSlots(slots) {
      const box = document.getElementById('saAccountSlotsGrid');
      if (!box) return;
      if (!slots || slots.length === 0) {
        box.innerHTML = '<div style="color:var(--muted);">Chưa có tài khoản nào trong Pool.</div>';
        return;
      }

      let html = '';
      slots.forEach(s => {
        const isBusy = s.in_flight > 0;
        const borderCol = isBusy ? '#a855f7' : (s.in_cooldown ? '#f59e0b' : '#242c3d');
        const bgCol = isBusy ? 'rgba(168, 85, 247, 0.1)' : '#10141f';
        const statusBadge = !s.enabled
          ? '<span style="color:#64748b;font-size:11px;">⏸️ Đã tắt</span>'
          : (isBusy
              ? '<span style="color:#c084fc;font-weight:700;font-size:11px;">🔥 Đang chạy (' + s.in_flight + ' Sub-Agent)</span>'
              : (s.in_cooldown
                  ? '<span style="color:#fbbf24;font-size:11px;">⏳ Cooldown</span>'
                  : '<span style="color:#34d399;font-weight:600;font-size:11px;">🟢 Rảnh 100% (Idle)</span>'));

        let provColor = '#fb923c';
        if (s.provider === 'claude-code') provColor = '#c084fc';
        else if (s.provider === 'alibaba') provColor = '#38bdf8';

        html += '<div style="background:' + bgCol + ';border:1px solid ' + borderCol + ';border-radius:10px;padding:11px 12px;">' +
          '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:5px;">' +
            '<span style="font-size:11px;font-weight:700;color:' + provColor + ';">SLOT #' + s.slot_index + ' • ' + htmlEscape(s.provider.toUpperCase()) + '</span>' +
            statusBadge +
          '</div>' +
          '<div style="font-size:12.5px;font-weight:700;color:#f1f5f9;word-break:break-all;margin-bottom:4px;">' + htmlEscape(s.email) + '</div>' +
          (s.active_role ? '<div style="font-size:11px;color:#c084fc;margin-bottom:4px;">🤖 ' + htmlEscape(s.active_role) + '</div>' : '') +
          '<div style="display:flex;justify-content:space-between;font-size:11px;color:var(--muted);border-top:1px solid rgba(255,255,255,0.05);padding-top:5px;margin-top:4px;">' +
            '<span>Req: <strong style="color:#cbd5e1;">' + (s.success_count || 0) + '</strong></span>' +
            '<span>Tok: <strong style="color:#38bdf8;">' + Number(s.total_tokens || 0).toLocaleString() + '</strong></span>' +
            '<span>Lat: <strong style="color:#34d399;">' + (s.avg_latency_ms ? s.avg_latency_ms + 'ms' : '—') + '</strong></span>' +
          '</div>' +
        '</div>';
      });
      box.innerHTML = html;
    }

    async function launchSubAgentSwarmUI() {
      const goal = document.getElementById('saMasterGoalInput').value.trim();
      const count = parseInt(document.getElementById('saAgentCountSelect').value || '5', 10);
      const model = document.getElementById('saModelSelect').value;
      const roleModeEl = document.getElementById('saSwarmRoleModeSelect');
      const roleMode = roleModeEl ? roleModeEl.value : 'seo-and-code';
      const lineByLine = document.getElementById('saLineByLineCheckbox').checked;
      const statusBox = document.getElementById('saLaunchStatus');
      const btn = document.getElementById('saLaunchBtn');

      if (!goal) {
        if (statusBox) statusBox.innerHTML = '<span style="color:#f87171;">❌ Vui lòng nhập bài toán hoặc danh sách nhiệm vụ cho Đàn Sub-Agents!</span>';
        return;
      }

      let customTasks = [];
      let mode = roleMode;
      if (lineByLine) {
        mode = 'custom-tasks';
        const lines = goal.split('\n').map(l => l.trim()).filter(Boolean);
        let workerModel = 'auto-worker';
        if (roleMode === 'content-only') workerModel = 'antigravity/gemini-3.1-pro-high';
        else if (roleMode === 'coding-only') workerModel = 'antigravity/gemini-3.8-flash-high';

        customTasks = lines.map((line, i) => ({
          index: i + 1,
          role: 'Sub-Agent #' + (i + 1),
          prompt: line,
          model: workerModel
        }));
      }

      btn.disabled = true;
      btn.textContent = '⏳ Quản Lý SEO đang lập kế hoạch & chạy ' + (customTasks.length || count) + ' Sub-Agents...';
      statusBox.innerHTML = '<span style="color:#38bdf8;">👑 Bước 1: Quản Lý Dự Án SEO (Argon 4 / Opus 5.5 / Tối Qwen 3.8 Max) lập kế hoạch ➔ Bước 2: Phóng ' + (customTasks.length || count) + ' Sub-Agents (Gemini 3.1 Pro High viết bài & Gemini 3.8 Flash High viết code)...</span>';

      if (saPollingTimer) clearInterval(saPollingTimer);
      saPollingTimer = setInterval(loadSubAgentsView, 900);

      try {
        const r = await fetch('/v1/subagents/swarm', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            action: 'swarm',
            mode: mode,
            master_goal: goal,
            agent_count: count,
            model: model,
            tasks: customTasks
          })
        });
        const d = await r.json();
        if (saPollingTimer) { clearInterval(saPollingTimer); saPollingTimer = null; }
        loadSubAgentsView();

        if (r.ok && d.summary) {
          statusBox.innerHTML = '<span style="color:#34d399;">✔ Quản Lý SEO (<strong>' + htmlEscape(d.summary.manager_model || 'auto') + '</strong>) + ' + d.summary.success_count + '/' + d.summary.total_sub_agents + ' Sub-Agents hoàn tất trên ' + d.summary.distinct_accounts + ' tài khoản trong <strong>' + d.summary.total_wall_time_ms + ' ms</strong>!</span>';
          renderSwarmSummary(d.summary);
        } else {
          statusBox.innerHTML = '<span style="color:#f87171;">❌ Lỗi: ' + htmlEscape(d.error ? d.error.message : 'Thất bại') + '</span>';
        }
      } catch (err) {
        if (saPollingTimer) { clearInterval(saPollingTimer); saPollingTimer = null; }
        statusBox.innerHTML = '<span style="color:#f87171;">❌ Lỗi kết nối: ' + htmlEscape(err.message) + '</span>';
      } finally {
        btn.disabled = false;
        btn.textContent = '🚀 Phóng Đàn Sub-Agents Chạy Song Song Ngay';
      }
    }

    function renderSwarmSummary(summary) {
      const box = document.getElementById('saSwarmResultsBox');
      if (!box || !summary) return;

      let managerCard = '';
      if (summary.manager_plan || summary.manager_model) {
        const nightTag = summary.is_night_mode ? ' <span class="badge badge-green">🌙 Chế Độ Đêm ⚡ 40% Off</span>' : ' <span class="badge badge-blue">☀️ Chế Độ Ban Ngày</span>';
        managerCard = '<div style="background:rgba(245,158,11,0.08);border:1px solid rgba(245,158,11,0.35);border-radius:9px;padding:12px;margin-bottom:14px;">' +
          '<div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:6px;margin-bottom:6px;">' +
            '<div><strong style="color:#fbbf24;font-size:13px;">👑 Quản Lý Dự Án SEO (Lead Orchestrator): ' + htmlEscape(summary.manager_model || 'antigravity/gemini-4-argon-high') + '</strong>' + nightTag + '</div>' +
            '<span style="font-size:11.5px;color:#cbd5e1;font-family:monospace;">Tài khoản Quản lý: <strong>' + htmlEscape(summary.manager_account || 'auto') + '</strong> | Viết bài: <code>gemini-3.1-pro-high</code> | Code: <code>gemini-3.8-flash-high</code></span>' +
          '</div>' +
          (summary.manager_plan ? '<pre style="max-height:190px;overflow-y:auto;font-size:11.5px;margin:0;background:#080b11;color:#fde68a;">' + htmlEscape(summary.manager_plan) + '</pre>' : '') +
        '</div>';
      }

      let linkMatrixCard = '';
      if (summary.shared_link_matrix) {
        linkMatrixCard = '<div style="background:rgba(56,189,248,0.06);border:1px dashed rgba(56,189,248,0.35);border-radius:9px;padding:11px;margin-bottom:12px;">' +
          '<div style="font-weight:700;font-size:12px;color:#38bdf8;margin-bottom:5px;">🔗 Ma Trận Liên Kết Nội Bộ SEO (Inter-Agent Link Matrix đã inject vào tất cả Sub-Agents):</div>' +
          '<pre style="max-height:140px;overflow-y:auto;font-size:11px;margin:0;background:#080b11;color:#7dd3fc;">' + htmlEscape(summary.shared_link_matrix) + '</pre>' +
        '</div>';
      }

      const runId = summary.id || 'latest';
      const failoverTag = (summary.watchdog_failovers > 0)
        ? (' | <span style="color:#fbbf24;">⚡ ' + summary.watchdog_failovers + ' Watchdog Failover</span>')
        : '';

      let html = '<div style="background:#0b0f17;border:1px solid #242c3d;border-radius:10px;padding:14px;">' +
        '<div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;margin-bottom:12px;padding-bottom:10px;border-bottom:1px solid #1e293b;">' +
          '<div>' +
            '<span class="badge badge-green">🐝 Kết Quả Đàn Sub-Agents Song Song</span> ' +
            '<strong style="color:#fff;margin-left:6px;">' + summary.success_count + '/' + summary.total_sub_agents + ' Sub-Agents Thành Công</strong>' +
            failoverTag +
          '</div>' +
          '<div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;">' +
            '<div style="font-size:12px;color:var(--muted);">' +
              'Tài khoản: <strong style="color:#fb923c;">' + summary.distinct_accounts + '</strong> | ' +
              'Thời gian: <strong style="color:#34d399;">' + summary.total_wall_time_ms + ' ms</strong> | ' +
              'Tokens: <strong style="color:#38bdf8;">' + Number(summary.total_tokens || 0).toLocaleString() + '</strong> | ' +
              'Giá trị: <strong style="color:#c084fc;">$' + (summary.estimated_value_usd || 0).toFixed(4) + '</strong>' +
            '</div>' +
            '<div style="display:flex;gap:5px;">' +
              '<a href="/v1/subagents?export=' + encodeURIComponent(runId) + '&amp;format=md" target="_blank" class="btn" style="text-decoration:none;padding:4px 8px;font-size:11px;color:#38bdf8;">📥 Xuất .MD</a>' +
              '<a href="/v1/subagents?export=' + encodeURIComponent(runId) + '&amp;format=json" target="_blank" class="btn" style="text-decoration:none;padding:4px 8px;font-size:11px;color:#34d399;">📥 Xuất .JSON</a>' +
            '</div>' +
          '</div>' +
        '</div>' +
        managerCard +
        linkMatrixCard +
        '<div style="display:grid;grid-template-columns:repeat(auto-fill, minmax(380px, 1fr));gap:12px;">';

      (summary.results || []).forEach(res => {
        const ok = res.status === 'ok';
        const bCol = ok ? 'rgba(16,185,129,0.35)' : 'rgba(239,68,68,0.4)';
        const wdBadge = (res.watchdog_retried && res.failover_from)
          ? (' <span class="badge" style="background:rgba(245,158,11,0.2);color:#fbbf24;font-size:10px;">⚡ Failover từ ' + htmlEscape(res.failover_from) + '</span>')
          : '';

        html += '<div style="background:#121722;border:1px solid ' + bCol + ';border-radius:9px;padding:12px;">' +
          '<div style="display:flex;justify-content:space-between;align-items:center;gap:6px;margin-bottom:6px;">' +
            '<strong style="font-size:12.5px;color:#c084fc;">' + htmlEscape(res.role || ('Sub-Agent #' + res.index)) + '</strong>' +
            '<span style="font-size:11px;color:' + (ok ? '#34d399' : '#f87171') + ';font-weight:700;">' + (ok ? ('✔ ' + res.latency_ms + ' ms') : '❌ Lỗi') + '</span>' +
          '</div>' +
          '<div style="font-size:11.5px;color:#fb923c;margin-bottom:6px;font-family:monospace;">👤 Tài khoản: <strong>' + htmlEscape(res.account_email || 'auto-pool') + '</strong> (' + htmlEscape(res.resolved_model || '') + ')' + wdBadge + '</div>' +
          '<pre style="max-height:220px;overflow-y:auto;font-size:11.5px;margin:0;background:#080b11;color:#e2e8f0;">' + htmlEscape(ok ? res.output : res.error) + '</pre>' +
        '</div>';
      });

      html += '</div></div>';
      box.innerHTML = html;
    }

    async function submitBulkImportAccounts() {
      const raw = document.getElementById('saBulkImportInput').value.trim();
      const msg = document.getElementById('saBulkImportMsg');
      if (!raw) {
        if (msg) msg.innerHTML = '<span style="color:#f87171;">❌ Vui lòng dán danh sách URL Callback / Refresh Token / Access Token!</span>';
        return;
      }
      if (msg) msg.innerHTML = '<span style="color:#38bdf8;">⏳ Đang nạp và xác thực danh sách tài khoản vào Pool...</span>';
      try {
        const r = await fetch('/v1/subagents', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            action: 'bulk_import',
            bulk_input: raw
          })
        });
        const d = await r.json();
        if (r.ok) {
          let errNote = (d.errors && d.errors.length > 0) ? (' (Lỗi: ' + d.errors.join('; ') + ')') : '';
          msg.innerHTML = '<span style="color:#34d399;">✔ Đã nạp thành công <strong>' + (d.imported || 0) + '</strong> tài khoản mới vào Pool!' + htmlEscape(errNote) + '</span>';
          if (d.imported > 0) {
            document.getElementById('saBulkImportInput').value = '';
          }
          loadSubAgentsView();
          loadQuotaSummary(true);
        } else {
          msg.innerHTML = '<span style="color:#f87171;">❌ ' + htmlEscape(d.error ? d.error.message : 'Lỗi nạp tài khoản') + '</span>';
        }
      } catch (err) {
        msg.innerHTML = '<span style="color:#f87171;">❌ Lỗi: ' + htmlEscape(err.message) + '</span>';
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
