# Minecraft Bedrock Server Dashboard

## 0. 项目目标

重构一个运行在 Linux 下的 Minecraft Bedrock Dedicated Server Web 运维面板。

技术栈：

- 后端：Go
- 前端：Vue 3
- 数据库：SQLite 优先
- 部署环境：Ubuntu
- 可观测性：Prometheus + Grafana 可选接入
- 后续扩展：MCP 服务 + Agent 运维能力

项目核心目标：

1. 管理 Minecraft Bedrock Server 进程。
2. 在 Web 端实时查看服务器日志。
3. 在 Web 端输入 Minecraft 控制台命令。
4. 支持配置文件的 Web 端查看、编辑、校验、保存、快照和回滚。
5. 查看服务器运行状态，包括进程状态、CPU、内存、磁盘、网络等。
6. 保存运行日志到数据库，支持历史查询。
7. 暴露 Prometheus `/metrics` 接口。
8. 为后续 Agent 运维和 MCP 工具调用预留安全接口。
9. 所有高风险操作必须可审计。

第一阶段不需要实现复杂多实例、多用户权限、完整 Agent 自动运维。先实现单服务器实例版本。

------

## 1. 总体架构

采用单体后端 + 模块化内部结构。

```text
Vue Frontend
    |
HTTP + WebSocket
    |
Go Backend
    |
    |-- Server Manager
    |-- Log Manager
    |-- Config Manager
    |-- Metrics Manager
    |-- Prometheus Exporter
    |-- Audit Manager
    |-- MCP / Agent Gateway, later phase
    |
Linux Runtime
    |
bedrock_server process
    |
SQLite
```

后端职责：

- 管理 bedrock_server 子进程。
- 读取 stdout/stderr。
- 将日志推送到 WebSocket。
- 将日志写入 SQLite。
- 接收 Web 端命令并写入进程 stdin。
- 提供配置文件安全编辑能力。
- 采集系统和进程指标。
- 暴露 REST API、WebSocket API 和 Prometheus metrics。

前端职责：

- Dashboard 状态展示。
- Console 实时日志和命令输入。
- Config Editor 配置文件编辑。
- Logs 历史日志查询。
- Metrics 基础指标展示。
- Agent Ops 页面预留。

------

## 2. 技术选型

### 后端

建议：

```text
Go 1.23+
Chi
Gorilla WebSocket 或 nhooyr/websocket
SQLite
sqlc 或 GORM
Prometheus client_golang
gopsutil
fsnotify
```

本项目优先使用简单、稳定、便于维护的方案。

数据库访问可以优先使用 `database/sql` + `sqlx`，也可以使用 GORM。若使用 GORM，需要保证 migration 清晰。

### 前端

建议：

```text
Vue 3
Vite
TypeScript
Pinia
Vue Router
Element Plus
Monaco Editor 或 CodeMirror
ECharts
```

### 部署

建议：

```text
Docker Compose
Caddy 或 Nginx 反代
Prometheus
Grafana
systemd service 可选
```

------

## 3. 推荐目录结构

```text
mc-panel/
  backend/
    cmd/
      panel/
        main.go

    internal/
      app/
        app.go
        router.go
        config.go

      api/
        server_handler.go
        logs_handler.go
        config_handler.go
        metrics_handler.go
        audit_handler.go

      server/
        manager.go
        process.go
        state.go
        command.go
        types.go

      logs/
        hub.go
        writer.go
        repository.go
        parser.go
        types.go

      configmgr/
        manager.go
        safe_path.go
        validator.go
        snapshot.go
        types.go

      metrics/
        collector.go
        prometheus.go
        system.go
        process.go
        types.go

      storage/
        db.go
        migration.go
        models.go

      audit/
        audit.go
        repository.go
        types.go

      mcp/
        server.go
        tools.go
        policy.go
        audit.go

    migrations/
      001_init.sql

    go.mod
    go.sum

  frontend/
    src/
      main.ts
      App.vue

      api/
        server.ts
        logs.ts
        config.ts
        metrics.ts

      stores/
        server.ts
        logs.ts
        config.ts
        metrics.ts

      router/
        index.ts

      views/
        Dashboard.vue
        Console.vue
        ConfigEditor.vue
        Logs.vue
        Metrics.vue
        AgentOps.vue

      components/
        ServerStatusCard.vue
        LogConsole.vue
        CommandInput.vue
        ConfigFileTree.vue
        ConfigEditorPanel.vue
        MetricCard.vue
        MetricChart.vue

  deploy/
    docker-compose.yml
    prometheus/
      prometheus.yml
    grafana/
      provisioning/
    systemd/
      mc-panel.service

  README.md
```

------

## 4. 后端配置文件设计

创建后端配置文件，例如：

```text
backend/config/app.env
```

配置项建议：

```env
ADDR=:8080

DB_PATH=./data/panel.db

MC_SERVER_ID=default
MC_SERVER_NAME=Default Bedrock Server
MC_ROOT_DIR=/opt/minecraft/bedrock
MC_EXECUTABLE_PATH=/opt/minecraft/bedrock/bedrock_server
MC_WORKING_DIR=/opt/minecraft/bedrock

LOG_RETENTION_DAYS=7
LOG_MAX_ROWS=1000000

ENABLE_PROMETHEUS=true
ENABLE_MCP=false

AUTH_ENABLED=false
SESSION_SECRET=change-me
```

要求：

1. 后端启动时加载配置。
2. 所有路径必须支持绝对路径。
3. 后端启动时检查 `MC_WORKING_DIR` 和 `MC_EXECUTABLE_PATH` 是否存在。
4. 缺失关键配置时应返回明确错误。

------

## 5. 数据库设计

第一阶段使用 SQLite。

创建 migration 文件：

```sql
CREATE TABLE IF NOT EXISTS servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_dir TEXT NOT NULL,
    executable_path TEXT NOT NULL,
    working_dir TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS server_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,
    pid INTEGER,
    state TEXT NOT NULL,
    started_at DATETIME,
    stopped_at DATETIME,
    exit_code INTEGER,
    error_message TEXT,
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS log_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,
    run_id INTEGER,
    level TEXT NOT NULL,
    source TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_log_entries_server_created
ON log_entries(server_id, created_at);

CREATE INDEX IF NOT EXISTS idx_log_entries_message
ON log_entries(message);

CREATE TABLE IF NOT EXISTS command_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,
    run_id INTEGER,
    command TEXT NOT NULL,
    source TEXT NOT NULL,
    user_id TEXT,
    success BOOLEAN NOT NULL,
    error_message TEXT,
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS config_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_by TEXT,
    reason TEXT,
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_config_snapshots_file
ON config_snapshots(server_id, file_path, created_at);

CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_type TEXT NOT NULL,
    actor_id TEXT,
    action TEXT NOT NULL,
    target TEXT,
    detail TEXT,
    created_at DATETIME NOT NULL
);
```

------

## 6. Server Manager 设计

### 6.1 状态定义

```go
type ServerState string

const (
    StateStopped  ServerState = "stopped"
    StateStarting ServerState = "starting"
    StateRunning  ServerState = "running"
    StateStopping ServerState = "stopping"
    StateCrashed  ServerState = "crashed"
)
```

### 6.2 核心能力

实现 `server.Manager`：

```go
type Manager interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Restart(ctx context.Context) error
    Kill(ctx context.Context) error
    SendCommand(ctx context.Context, cmd string, source string, userID string) error
    Status(ctx context.Context) (*Status, error)
}
```

### 6.3 进程启动要求

启动方式：

- 使用 `exec.CommandContext`。
- 工作目录设置为 `MC_WORKING_DIR`。
- 命令为 `MC_EXECUTABLE_PATH`。
- 捕获 stdout、stderr、stdin。
- stdout/stderr 分别进入 Log Manager。
- stdin 用于 Web 控制台命令输入。

注意：

1. Start 必须加锁，避免重复启动。
2. Stop 必须加锁，避免重复停止。
3. 进程退出后必须更新状态。
4. 非正常退出记录为 `crashed`。
5. 每次启动创建一条 `server_runs` 记录。
6. 每次停止更新 `server_runs.stopped_at` 和 `exit_code`。

### 6.4 Stop 策略

优先发送 Minecraft 命令：

```text
stop
```

等待若干秒。

若超时，再尝试终止进程。

不要一开始就 kill。

------

## 7. Log Manager 设计

日志流转路径：

```text
bedrock_server stdout/stderr
    ↓
Log Manager
    ↓
Ring Buffer
    ↓
WebSocket Broadcast
    ↓
SQLite Writer
```

### 7.1 日志结构

```go
type LogEntry struct {
    ID        int64     `json:"id"`
    ServerID  string    `json:"server_id"`
    RunID     *int64    `json:"run_id,omitempty"`
    Level     string    `json:"level"`
    Source    string    `json:"source"`
    Message   string    `json:"message"`
    CreatedAt time.Time `json:"created_at"`
}
```

`Level` 可选：

```text
info
warn
error
system
```

`Source` 可选：

```text
stdout
stderr
panel
agent
system
```

### 7.2 WebSocket 协议

服务端推送日志：

```json
{
  "type": "log",
  "data": {
    "server_id": "default",
    "level": "info",
    "source": "stdout",
    "message": "Server started.",
    "created_at": "2026-06-20T12:00:00Z"
  }
}
```

客户端发送命令：

```json
{
  "type": "command",
  "data": {
    "command": "list"
  }
}
```

服务端推送状态：

```json
{
  "type": "status",
  "data": {
    "state": "running",
    "pid": 1234,
    "uptime_seconds": 3600
  }
}
```

错误消息：

```json
{
  "type": "error",
  "message": "server is not running"
}
```

### 7.3 日志查询 API

```text
GET /api/logs?limit=500
GET /api/logs/search?q=error&limit=200
GET /api/logs/stream
```

`/api/logs/stream` 使用 WebSocket。

### 7.4 日志保留策略

实现简单清理任务：

```text
删除超过 LOG_RETENTION_DAYS 的日志
或当日志行数超过 LOG_MAX_ROWS 时删除最旧日志
```

清理任务可以在后端启动时执行一次，之后每小时执行一次。

------

## 8. Config Manager 设计

目标：提供安全的配置文件查看、编辑、保存、快照和回滚。

### 8.1 允许编辑的文件

第一版只允许白名单文件：

```text
server.properties
allowlist.json
permissions.json
```

后续可扩展：

```text
worlds/<world>/behavior_packs.json
worlds/<world>/resource_packs.json
```

### 8.2 安全路径规则

必须实现路径沙箱。

要求：

1. 禁止绝对路径。
2. 禁止 `../`。
3. 禁止软链接逃逸。
4. 只允许白名单文件。
5. 所有文件操作都必须限制在 `MC_ROOT_DIR` 内。
6. 不允许 Web 端编辑面板自身配置。
7. 不允许读取 `.env`、密钥文件、证书、系统文件。

### 8.3 API

```text
GET  /api/config/files
GET  /api/config/file?path=server.properties
PUT  /api/config/file
POST /api/config/validate
GET  /api/config/history?path=server.properties
POST /api/config/restore
```

保存请求：

```json
{
  "path": "server.properties",
  "content": "server-name=My Server\nmax-players=10\n",
  "reason": "update max players"
}
```

回滚请求：

```json
{
  "snapshot_id": 1
}
```

### 8.4 保存流程

保存配置文件时必须按以下顺序执行：

1. 校验路径。
2. 校验文件是否在白名单。
3. 读取当前内容。
4. 将当前内容写入 `config_snapshots`。
5. 校验新内容格式。
6. 写入临时文件。
7. 原子替换目标文件。
8. 写入 audit event。
9. 返回成功。

### 8.5 校验规则

`server.properties`：

- 按 key-value 文本处理。
- 允许注释行。
- 允许空行。
- 基础校验：每个非空非注释行必须包含 `=`。
- 可选增强：对 `server-port`、`max-players` 等字段做类型校验。

JSON 文件：

- 必须是合法 JSON。
- 保存前格式化可选，但不要强制改变用户内容。
- 出错时返回错误行列信息。

------

## 9. Metrics Manager 设计

目标：提供系统状态和进程状态。

### 9.1 状态 Summary API

```text
GET /api/metrics/summary
```

返回示例：

```json
{
  "server": {
    "state": "running",
    "pid": 1234,
    "uptime_seconds": 3600
  },
  "process": {
    "cpu_percent": 12.5,
    "memory_bytes": 734003200
  },
  "system": {
    "cpu_percent": 35.1,
    "memory_total": 4096000000,
    "memory_used": 2500000000,
    "disk_total": 50000000000,
    "disk_used": 23000000000
  },
  "network": {
    "rx_bytes_total": 123456789,
    "tx_bytes_total": 987654321
  }
}
```

### 9.2 Prometheus 指标

暴露：

```text
GET /metrics
```

建议指标：

```text
mc_server_running
mc_server_uptime_seconds
mc_server_restart_total
mc_server_crash_total
mc_server_log_lines_total
mc_server_command_total
mc_process_cpu_percent
mc_process_memory_bytes
mc_system_cpu_percent
mc_system_memory_used_bytes
mc_system_memory_total_bytes
mc_network_rx_bytes_total
mc_network_tx_bytes_total
mc_panel_ws_clients
mc_panel_agent_actions_total
```

所有指标需要带 `server_id` label。

示例：

```text
mc_server_running{server_id="default"} 1
mc_server_uptime_seconds{server_id="default"} 3600
mc_process_memory_bytes{server_id="default"} 734003200
```

------

## 10. Audit Manager 设计

所有重要操作必须写审计。

需要记录的操作：

```text
server.start
server.stop
server.restart
server.kill
server.command
config.read
config.update
config.restore
agent.tool_call
agent.approve
agent.reject
```

审计结构：

```go
type AuditEvent struct {
    ID        int64
    ActorType string
    ActorID   string
    Action    string
    Target    string
    Detail    string
    CreatedAt time.Time
}
```

ActorType：

```text
user
agent
system
```

第一版没有用户系统时，可以使用：

```text
actor_type = "user"
actor_id = "local"
```

------

## 11. REST API 汇总

### Server

```text
GET  /api/server/status
POST /api/server/start
POST /api/server/stop
POST /api/server/restart
POST /api/server/command
```

`POST /api/server/command` 请求：

```json
{
  "command": "list"
}
```

### Logs

```text
GET /api/logs?limit=500
GET /api/logs/search?q=error&limit=200
GET /api/logs/stream
```

### Config

```text
GET  /api/config/files
GET  /api/config/file?path=server.properties
PUT  /api/config/file
POST /api/config/validate
GET  /api/config/history?path=server.properties
POST /api/config/restore
```

### Metrics

```text
GET /api/metrics/summary
GET /metrics
```

### Audit

```text
GET /api/audit?limit=200
```

------

## 12. 前端页面施工要求

### 12.1 Dashboard.vue

展示：

- 服务器状态。
- PID。
- 运行时长。
- CPU 使用率。
- 内存使用量。
- 启动按钮。
- 停止按钮。
- 重启按钮。
- 最近错误日志。

交互要求：

- 状态每 3 到 5 秒刷新一次。
- Start/Stop/Restart 操作需要 loading 状态。
- Restart 操作需要二次确认。
- 错误要用 Element Plus Message 展示。

### 12.2 Console.vue

展示：

- 实时日志窗口。
- 命令输入框。
- 发送按钮。
- 自动滚动开关。
- 暂停滚动开关。
- 日志级别过滤。
- 清空当前视图按钮。

交互要求：

- 页面打开时连接 WebSocket。
- WebSocket 断开时显示连接状态。
- 支持重连。
- 输入命令回车发送。
- 命令发送后清空输入框。
- 不允许发送空命令。
- 日志只清空前端视图，不删除数据库。

### 12.3 ConfigEditor.vue

展示：

- 左侧配置文件列表。
- 中间代码编辑器。
- 右侧校验结果和历史版本。
- 保存按钮。
- 校验按钮。
- 回滚按钮。

交互要求：

- 打开文件前先从 `/api/config/files` 获取白名单文件。
- 保存前调用 validate。
- 保存成功后提示“部分配置可能需要重启服务器生效”。
- 回滚操作需要二次确认。

### 12.4 Logs.vue

展示：

- 历史日志列表。
- 搜索框。
- 时间范围过滤。
- source 过滤。
- level 过滤。

第一版可以只实现：

- 最近 500 条日志。
- 关键词搜索。

### 12.5 Metrics.vue

展示：

- CPU。
- 内存。
- 磁盘。
- 网络收发。
- bedrock_server 进程 CPU。
- bedrock_server 进程内存。

第一版可以使用普通卡片 + 简单折线图。

### 12.6 AgentOps.vue

第一版只做占位页面。

展示：

```text
Agent 运维功能尚未启用。
未来将支持 MCP tools、诊断、配置 patch、人工审批和审计。
```

------

## 13. MCP / Agent 预留设计

第一版可以不实现完整 MCP，但后端结构中需要预留 `internal/mcp`。

未来工具设计：

### 只读工具

```text
server.status
logs.tail
logs.search
config.read
metrics.summary
```

### 低风险工具

```text
config.diff
config.propose_patch
backup.create
diagnostics.run
```

### 高风险工具

```text
server.stop
server.restart
server.send_command
config.apply_patch
```

高风险工具必须经过人工审批，不允许 Agent 直接执行。

禁止实现：

```text
shell.exec
file.read_any
file.write_any
secret.read
world.delete
panel_config.modify
```

Agent 所有工具调用都必须写入 `audit_events`。

------

## 14. 安全要求

必须遵守：

1. Web 端不能执行任意 shell 命令。
2. Agent 不能执行任意 shell 命令。
3. 配置编辑必须限制在白名单文件。
4. 所有文件路径必须做 sandbox 校验。
5. 禁止路径穿越。
6. 禁止读取密钥、证书、`.env`、系统文件。
7. Stop/Restart/Kill 等操作必须记录审计。
8. 后续启用公网部署时必须启用登录认证。
9. WebSocket 也必须复用认证机制。
10. 不要把敏感信息写入前端日志。

------

## 15. 部署文件

### 15.1 docker-compose.yml

第一版至少包含：

```text
backend
frontend
prometheus
grafana
```

也可以先只做：

```text
backend
frontend
```

后续再加入 Prometheus 和 Grafana。

### 15.2 Prometheus 配置

创建：

```text
deploy/prometheus/prometheus.yml
```

内容示例：

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: "mc-panel"
    static_configs:
      - targets: ["backend:8080"]
```

### 15.3 Grafana

预留目录：

```text
deploy/grafana/provisioning/
```

第一版可以不写完整 dashboard，但需要在 README 中说明 `/metrics` 已经可以被 Prometheus 抓取。

------

## 16. 开发阶段划分

请按阶段施工，每阶段完成后保证项目可运行。

### Phase 1：后端基础和进程管理

完成：

- Go 项目初始化。
- 配置加载。
- SQLite 初始化。
- migration 执行。
- Server Manager。
- Start/Stop/Restart/Status API。
- 基础审计。

验收：

```text
go run ./cmd/panel 可以启动后端
GET /api/server/status 可用
POST /api/server/start 可启动 bedrock_server
POST /api/server/stop 可停止 bedrock_server
server_runs 表有记录
audit_events 表有记录
```

### Phase 2：实时日志和命令输入

完成：

- stdout/stderr 捕获。
- Log Manager。
- WebSocket Hub。
- 日志实时推送。
- Web 端命令写入 stdin。
- 日志入库。
- 命令入库。

验收：

```text
Console 页面能看到实时日志
Console 页面能发送 list、say、stop 等 Minecraft 命令
log_entries 表有日志
command_entries 表有命令记录
刷新页面后可以查询历史日志
```

### Phase 3：前端基础页面

完成：

- Vue 3 项目初始化。
- Router。
- Pinia。
- Element Plus。
- Dashboard。
- Console。
- Logs。

验收：

```text
前端可以查看服务器状态
前端可以启动/停止/重启服务器
前端可以实时查看日志
前端可以发送命令
前端可以查询历史日志
```

### Phase 4：配置管理

完成：

- Config Manager。
- 安全路径校验。
- 白名单文件列表。
- 配置读取。
- 配置校验。
- 配置保存。
- 配置快照。
- 配置回滚。
- ConfigEditor 页面。

验收：

```text
可以查看 server.properties
可以编辑 server.properties
保存前会校验
保存前会自动创建快照
可以查看历史快照
可以回滚到旧版本
非法路径会被拒绝
非白名单文件会被拒绝
```

### Phase 5：系统指标和 Prometheus

完成：

- gopsutil 接入。
- 服务器进程 CPU/内存。
- 系统 CPU/内存/磁盘/网络。
- `/api/metrics/summary`。
- `/metrics`。
- Metrics 页面。

验收：

```text
Dashboard 显示 CPU/内存
Metrics 页面显示系统状态
Prometheus 可以 scrape /metrics
```

### Phase 6：MCP / Agent 预留

完成：

- `internal/mcp` 目录。
- Tool interface。
- Policy interface。
- Audit hook。
- AgentOps 占位页面。
- README 说明未来工具设计。

验收：

```text
代码结构中已经预留 MCP Gateway
AgentOps 页面存在
README 中说明 MCP 安全边界
```

------

## 17. 错误处理要求

后端统一返回 JSON：

成功：

```json
{
  "ok": true,
  "data": {}
}
```

失败：

```json
{
  "ok": false,
  "error": {
    "code": "SERVER_NOT_RUNNING",
    "message": "server is not running"
  }
}
```

常见错误码：

```text
SERVER_ALREADY_RUNNING
SERVER_NOT_RUNNING
SERVER_START_FAILED
SERVER_STOP_FAILED
COMMAND_WRITE_FAILED
CONFIG_FILE_NOT_ALLOWED
CONFIG_PATH_INVALID
CONFIG_VALIDATE_FAILED
CONFIG_SAVE_FAILED
DB_ERROR
INTERNAL_ERROR
```

前端必须显示清晰错误，不要只输出 `Error`。

------

## 18. 日志规范

后端自身日志使用结构化日志，建议使用 `slog`。

记录内容：

```text
server start
server stop
server crash
command received
config updated
config restored
websocket connected
websocket disconnected
metrics collection error
```

不要打印敏感配置。

------

## 19. 测试要求

至少实现以下测试：

### 后端单元测试

```text
configmgr.SafePath
configmgr.ValidateServerProperties
configmgr.ValidateJSON
server state transition
logs parser
```

### 后端集成测试

可以模拟一个 fake server process。

fake server 行为：

```text
启动后每秒输出一行日志
stdin 收到 list 后输出 player list
stdin 收到 stop 后退出
```

验收：

```text
Start 能启动 fake server
SendCommand 能写入 stdin
LogManager 能收到 stdout
Stop 能停止进程
```

------

## 20. README 要求

README 至少包含：

1. 项目简介。
2. 功能列表。
3. 技术栈。
4. Linux 部署要求。
5. 如何配置 `MC_ROOT_DIR` 和 `MC_EXECUTABLE_PATH`。
6. 如何启动后端。
7. 如何启动前端。
8. 如何使用 Docker Compose。
9. 如何接入 Prometheus。
10. 安全注意事项。
11. MCP / Agent 运维规划。

------

## 21. 代码质量要求

1. 不要把所有逻辑写在 handler 里。
2. Handler 只负责参数解析、调用 service、返回响应。
3. 进程管理逻辑放在 `internal/server`。
4. 日志逻辑放在 `internal/logs`。
5. 配置逻辑放在 `internal/configmgr`。
6. 指标逻辑放在 `internal/metrics`。
7. 数据库逻辑放在 `internal/storage` 或各模块 repository。
8. 所有 goroutine 必须有退出机制。
9. WebSocket 连接关闭后必须清理。
10. 数据库写入失败不能阻塞实时日志推送。
11. 高频日志写入建议使用 channel + batch insert。
12. 所有 public API 都需要清晰的 request/response struct。

------

## 22. 第一版不做的事情

明确不在第一版实现：

```text
复杂多用户权限
OAuth 登录
完整告警系统
自动备份世界存档
任意 Shell Web Terminal
Agent 自动执行高风险操作
完整 MCP 客户端 UI
完整 Grafana Dashboard
```

这些只做结构预留。

------

## 23. 最终验收标准

项目完成后应满足：

1. 在 Linux 下可以启动 Go 后端。
2. 可以通过 Web 面板启动、停止、重启 Minecraft Bedrock Server。
3. 可以实时查看服务器日志。
4. 可以在 Web 端输入 Minecraft 控制台命令。
5. 日志会保存到 SQLite。
6. 可以查询历史日志。
7. 可以查看和编辑 `server.properties`。
8. 保存配置前会自动创建快照。
9. 可以回滚配置。
10. 可以查看 CPU、内存、磁盘、网络、进程资源。
11. `/metrics` 可以被 Prometheus 抓取。
12. 重要操作会写入审计日志。
13. 所有配置文件操作都有路径安全限制。
14. Agent/MCP 相关代码结构已经预留，但不开放危险能力。
15. README 能让新开发者在本地跑起来。

------

## 24. Codex 执行要求

请按 Phase 顺序实现，不要跳阶段。

每完成一个 Phase：

1. 确认项目可以编译。
2. 确认主要功能可运行。
3. 更新 README。
4. 简要列出已完成内容。
5. 简要列出未完成内容。
6. 不要引入与本文档无关的大型依赖。
7. 不要实现任意 shell 执行功能。
8. 不要把 Agent 设计成可直接控制 Linux Shell。
9. 遇到需求不明确处，优先选择安全、简单、可维护的实现。

第一优先级是可运行和可维护，第二优先级才是功能完整度。