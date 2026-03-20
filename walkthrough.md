# SOYAL Proxy 專案完成說明

目前已經順利將 SOYAL 門禁系統的取代專案開發並編譯完成。所有的 Go 原始碼以及編譯出來的 Windows 執行檔 (`soyal_proxy.exe`) 皆存放於您的專案目錄：
`/var/www/docker/html/go-lang/soyal-proxy`

## 1. 程式特色與實作範圍
1. **Serial Worker**: 
   - 透過設定檔中的 COM port 連接 RS-485。
   - **開機自動偵測與型號辨識 (Auto-Discovery)**：程式啟動時會主動發送站號 1~16 的 `12H 00H` (讀取設備參數) 詢問指令。不僅能動態將有回應的站號加入輪詢清單，還能精準解析出該設備的具體型號（例如識別出它是 `AR-881E`、`AR-829Ev5` 或是 `AR-721Ev2` 等），即使不在設定檔中也能賦予它正確的設備名稱。
   - 使用 **Polling 模式**：每秒會定時輪詢所有啟用與自動註冊的設備站號 (Node ID)，發送 `25H` 讀取最新一筆事件，並以 `37H` 刪除該筆已紀錄的事件。
2. **Logic Parser**:
   - 包含對 SOYAL HEX 指令的 `7E` 封包頭、`Length` 驗證，以及 `XOR` 和 `SUM` 的嚴格檢查。
   - 針對 Command `27H` (刷卡事件的 Echo)，能精準切出刷卡時間 (YY/MM/DD HH:MM:SS) 與卡片內碼 (Site Code + Card Code)。
3. **Redis Publisher**:
   - 即時將取得的刷卡事件打包成 JSON：
     ```json
     {
         "device_name": "大門前門",
         "card_id": "01234:56789",
         "time": "2023-10-25T14:30:15Z",
         "event_code": 11,
         "event_desc": "Normal Access by tag"
     }
     ```
   - 透過 `PUBLISH` 指令將 JSON 推進指定的 Redis Topic 供後端接手處理。

## 2. 參數設定 (config.json)
在執行檔的同一層目錄中，有一個自動產生的 `config.json` 樣板：
```json
{
  "serial_port": "COM1",
  "baud_rate": 9600,
  "redis_host": "127.0.0.1:6379",
  "redis_pass": "",
  "redis_topic": "soyal_events",
  "devices": {
    "1": "大門前門",
    "2": "大門後門",
    "3": "電梯",
    "4": "車庫etag",
    "5": "車庫刷卡"
  }
}
```
> [!IMPORTANT]
> 請在執行 `soyal_proxy.exe` 之前，將 `serial_port` 改為您 Windows 上對應的 RS-485 轉接線 COM Port (例如 `"COM3"`)。並確保您的 Redis 伺服器正在運行。

## 3. 回來驗證與執行
由於軟體依賴實體的 RS-485 設備，請您：
1. 將 RS-485 USB 轉接線接上您的電腦，並確認裝置管理員的 COM Port 號碼。
2. 修改 `config.json` 後，請在終端機 (Powershell/CMD) 啟動程式：
   ```cmd
   cd //wsl.localhost/Ubuntu-20.04/var/www/docker/html/go-lang/soyal-proxy/
   soyal_proxy.exe
   ```
3. 用卡片在感應大門刷卡，觀察終端機是否印出解析成功的 Event，以及您後端 Redis 是否收到訊息。

## 4. 全域白名單同步管理 (CLI)
為了方便您統一管理所有設備內的卡片白名單，程式提供了不需要改變背景運行邏輯的獨立同步工具：

1. **下載所有設備白名單 (Global Sync Down)**：
   在終端機輸入：
   ```cmd
   soyal_proxy.exe -sync-down-all
   ```
   > 程式會自動向 `config.json` 裡面設定的所有設備讀取會員資料，自動整理合併擁有相同卡號 (`XXXXX:XXXXX`) 的使用者，並在資料夾產生一份綜合的 `global_users.json`。
   
2. **本機全域編輯 (Global Edit)**：
   用純文字編輯器開啟 `global_users.json`，您可以很直觀的看到「一張卡號對應了哪些設備上的哪些位址 (User Address)」：
   ```json
   [
     {
       "card_id": "01234:56789",
       "permissions": {
         "1": { 
           "user_addr": 15,
           "pin": "1234",
           "expiry": "2024-12-31" 
         },
         "3": { 
           "floors": [1, 2, 3, 5] 
         }
       }
     }
   ]
   ```
   > **進階設定說明：** 
   > 您可以在 permissions 的各台機器設定中，額外宣告：
   > 1. `"pin": "1234"` (設定專屬四位數密碼)
   > 2. `"expiry": "2025-10-15"` (設定卡片到期日，若無填寫預設為 2099 年)
   > 3. `"floors": [1, 2, 10]` (針對電梯控制器，直接填寫允許抵達的樓層**陣列**，取代難懂的十六進位)。
   > 4. `"group1"` / `"group2"` / `"zone"` / `"mode"` 等進階選項也能設定，未填時程式將全數以 SOYAL 標準設定（開全區、全大門、純刷卡）自動補齊。
   您可以自由地去增加 Node Permission，只要記得 `user_addr` 不要與其他卡片重複（或直接修改想要的卡號供其他新進員工使用），進階屬性若有需要再參閱 `global_users_format_guide.md` 設定即可。
   
3. **上傳更新所有設備 (Global Sync Up)**：
   變更完成後，在終端機輸入：
   ```cmd
   soyal_proxy.exe -sync-up-all
   ```
   > 程式會自動讀取 `global_users.json` 內的設定，分門別類向各自對應的 Node 發送對應的資料與卡號，一鍵將全公司的權限同步回設備內存中！

## 5. 遠端手動遙控裝置 (Backend Remote Control)
由於 Go 程式在背景會以每秒最快速度不停發送輪詢封包 (`Polling`) 給設備，其他程式無法再直接佔用 COM Port。不過您可以用您的**後端系統**（透過 Redis），對正在執行的 `soyal_proxy.exe` 下達遙控開關命令：

只要您的後端往 Redis 頻道 `soyal_commands` 去 **PUBLISH** 以下格式的 JSON 字串：
```json
{
  "node_id": 3,
  "action": "open_door"
}
```

代理程式收到後，會「緊急暫停」一瞬間的背景輪詢，並對 `Node 3` 發送原廠 `21H` 的實體繼電器開門控制碼，達成**最即時且不會發生撞包的非同步開門功能**！

**現有支援的 Action 指令：**
- `"open_door"` 或 `"open"`：一般遙控開門 (觸發設備的 Output 2 Relay)。
- `"close_door"`：強制鎖門 (Output 2 OFF)。
- `"pulse_door"` 或 `"garage_toggle"`：點放 Output 2 繼電器 (Pulse)。適用於單按鍵循環式車庫鐵捲門 (按一下開、再按暫停、再按關)。
- `"alarm_on"`：觸發警報 (Output 1 ON)。
- `"alarm_off"`：解除警報 (Output 1 OFF)。
- `"pulse_alarm"` 或 `"garage_stop"`：點放 Output 1 繼電器 (Pulse)。如果您的鐵捲門有獨立的「暫停鍵」，且接線牽在卡機的警報端上，可用此指令觸發暫停。
