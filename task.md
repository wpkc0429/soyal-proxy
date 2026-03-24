# SOYAL Proxy 專案任務清單

- [x] 初始化 Go 專案 (`go mod init soyal-proxy`)
- [x] 實作 Configuration 解析 (設備列表、Serial Port、Redis 參數)
- [x] 實作 Serial Worker (RS-485 COM Port 讀寫通訊)
- [x] 實作 Logic Parser (HEX 轉換成卡號與時間)
- [x] 實作 Redis Publisher (將事件推播給後端)
- [x] 實作 Main 主程式與 Goroutines 整合

---
### 階段二：卡片白名單 CLI 同步
- [x] 實作 `parser.go` 常數定義與 `UserParameter` 資料結構
- [x] 新增 `cli/sync.go` 提供 `SyncDown` 與 `SyncUp` 功能 (獨立於 Proxy 常駐迴圈)
- [x] 修改 `main.go` 解析 `-sync-down` 與 `-sync-up` 參數並切換模式

---
### 階段三：後端即時遙控機制 (Redis Pub/Sub)
- [x] 擴充 `publisher.go` 加入 Redis Subscriber 訂閱功能
- [x] 實作 `serialworker` 緊急寫入機制 (Priority Queue)，允許插隊發送指令
- [x] 建立 `21H` 控制指令模組 (`{"node_id": 1, "action": "open"}`)
- [x] 在主程式建立 Channel 並把 Subscriber 接收的命令送給 Serial Worker

---
### 階段四：自動化測試與驗證 (Automated Testing)
- [x] 實作 `parser_test.go` 單元測試
- [x] 驗證各種異常 HEX 封包的邊界條件

---
### 階段五：Web 介面 UI/UX 排版最佳化 (Management Dashboard)
- [x] 導入 Sidebar (側邊導航欄) 與主要的 Main Content 區域，建立完整的管理後台版型
- [x] 頂部 Header 重啟，放置全域操作按鈕 (同步、對時、儲存)
- [x] 將「遙控設備」、「即時刷卡」、「白名單管理」切分為視覺獨立的卡片或是分頁呈現
- [x] 強化 Tailwind 暗黑模式 (Dark Mode) 與毛玻璃 (Glassmorphism) 特效，套用漸層背景與過渡動畫
