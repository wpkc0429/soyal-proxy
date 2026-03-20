# Goal Description
製作一個 Go 程式取代現有的 701server 門禁系統，透過 Serial Port (RS-485) 與多個 SOYAL 設備互相通訊，處理包含大門前門、後門、電梯、車庫門(eTag及刷卡)等五種情境。程式將負責解析原始的 HEX 封包為卡號與時間，並即時透過 Redis Pub/Sub 傳送給後端。

## User Review Required
> [!IMPORTANT]
> 1. 請確認設備使用的 SOYAL 協議具體細節（主動回報還是 Polling 模式？如果是 Polling，需要發送的 Polling 指令為何？封包長度與格式是否有參考手冊？）。
> 2. 請確認給後端的 Redis Pub/Sub 的 Topic 名稱與 JSON 格式需求（例如是否需要包含卡號、時間、設備別名）。
> 3. 請確認要連線的 COM Port 名稱與通訊參數 (Baud Rate 通常為 9600)。

## Proposed Changes

### 1. Configuration (`config/config.go`)
- 建立設定檔載入機制 (JSON 或 YAML)，設定檔內容包含:
  - `SerialPort`: COM port 名稱與 BaudRate
  - `Redis`: 主機位址、Port、密碼、Topic 名稱
  - `Devices`: Node ID 對應的設備名稱（例如 Node 1 = 刷卡感應大門前門）

### 2. Serial Worker (`serial/worker.go`)
#### [NEW] `serial/worker.go`
- 引入開源的 Serial 庫 (`go.bug.st/serial` 或類似套件)。
- 建立 `ReadLoop` 與 `Write` 方法，負責與 RS-485 硬體持續通訊。
- 實作 **Polling Mode**：定時輪詢發送 `18H` (Polling Status) 與 `25H` (Get Event Log) 到設備的 Node ID。
- 若 `25H` 取回事件後，需自動發送 `37H` 刪除該筆已取回的紀錄。讀取到的 HEX Byte Stream 會拋到 Channel 供 Parser 使用。

### 3. Logic Parser (`parser/parser.go`)
#### [NEW] `parser/parser.go`
- 從 Channel 接收 Serial Worker 傳來的 Byte Stream。
- 負責找到封包頭 (`0x7E` 或 `FF 00 5A A5`) 並驗證 XOR/SUM Checksum。
- 根據 SOYAL 協議，解析 `09H` (Polling Echo) 或 `27H` (Event Log Echo)，取出時間與卡號資訊（Site Code + Card Code），轉換為 `AccessEvent` 結構:
  ```go
  type AccessEvent struct {
      DeviceName string
      CardID     string
      Time       time.Time
  }
  ```

### 4. Redis Publisher & Subscriber (`publisher/redis.go`)
#### [NEW] `publisher/redis.go` (取代原先單純的 publisher)
- **Publisher**：使用 `go-redis/redis/v8` 建立連線池。提供非同步推播介面，將 `AccessEvent` 序列化為 JSON 後以 `PUBLISH` 指令推送到 `soyal_events` Topic 中。
- **Subscriber**：啟動一個背景協程訂閱 `soyal_commands` Topic。
  - 接收後端系統發送的 JSON 控制指令，例如 `{"node_id": 1, "action": "open", "door_id": 1}`。
  - 接到指令後，將該指令轉換為 SOYAL 底層控制碼 (`21H` 或自訂十六進位陣列)。
  - 透過 Go Channel 將封包投遞給 `Serial Worker` 的寫入佇列 (優先於一般的 Polling 封包發送，避免被阻塞)，達成即時遙控開門/觸發繼電器等需求。

### 5. CLI Tool 功能 (`cli/sync.go`)
#### [NEW] `cli/sync.go`
- 透過指令列參數 (`-sync-down <node>` 或 `-sync-up <node>`) 來觸發白名單同步，不啟動常駐背景輪詢。
  - ** `-sync-down`（下載白名單）**：對指定設備發送 `87H` 指令（分頁讀取），遍歷會員位址。自動過濾掉空號後，將所有有效使用者的相關參數（User Address, UID, 密碼, Mode, 群組等）完整導出為本機的 JSON 檔案（例如 `users_node3.json`）。
  - **本地編輯**：使用者用純文字編輯器開啟該 JSON 並進行增刪或權限微調。
  - ** `-sync-up`（上傳白名單）**：讀取修改後的 JSON 檔案，依序向設備發送 `83H/84H` (寫入使用者參數) 來更新設備記憶體。

### 6. Main Module (`main.go`)
#### [MODIFY] `main.go`
- 啟動時先解析 Command Line 參數，若有下達 `-sync` 相關參數，則進入白名單同步流程並在結束後離開。
- 若無帶入特殊參數，則如常進入 Proxy 常駐模式啟動 Serial Worker 與 Parser。

## Verification Plan

### Automated Tests
- **Parser 單元測試**: 在 `parser` 模組寫 Unit Tests，自訂幾組 SOYAL HEX 假封包，驗證是否能正確解出卡號與時間。指令：`go test ./parser/...`

### Manual Verification
- 程式編譯後，將執行檔放入 Windows 或 Linux (接有 RS-485 轉 USB) 的環境中執行。
- 實際用卡片刷卡，觀察終端機是否有印出解析資訊，並使用 `redis-cli monitor` 觀察 Redis 或是後端系統是否正確收到 Pub/Sub 事件。
