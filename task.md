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
