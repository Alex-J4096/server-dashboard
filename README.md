# Minecraft Bedrock Server Dashboard

面向 Ubuntu 的单实例 Minecraft Bedrock Dedicated Server 管理面板。后端负责进程生命周期、实时日志、控制台命令、安全配置编辑、SQLite 持久化、审计和指标采集；前端提供 Vue 3 管理界面。

## 已实现功能

- 启动、安全停止和重启 `bedrock_server`，进程状态与运行记录持久化。
- WebSocket 实时控制台、断线重连、历史日志和关键词搜索。
- `server.properties`、`allowlist.json`、`permissions.json` 白名单编辑。
- 保存前校验、旧内容快照、历史查询和回滚。
- 系统及进程 CPU、内存、磁盘、网络指标，以及 Prometheus `/metrics`。
- 命令、服务器控制及配置操作审计。
- 本地账户登录、HttpOnly 会话、登录限流和基于角色的权限控制。
- 管理员用户管理：创建账户、分配角色、禁用账户和重置密码。
- MCP/Agent 策略接口和前端占位页；危险能力未开放。

## 开发任务清单

已完成：

- [x] Bedrock 服务端真实启动、安全停止、重启和异常状态处理
- [x] 实时控制台、WebSocket 重连、日志检索和持久化
- [x] 配置白名单编辑、校验、快照和回滚
- [x] 系统/进程指标、Prometheus、Docker Compose 与 systemd 部署
- [x] 用户登录、服务端会话、登录限流和 CSRF 来源检查
- [x] `admin`、`operator`、`viewer` 三角色权限控制及用户管理
- [x] 关键认证、拒绝访问、命令和配置操作审计

待完成：

- [ ] MCP/Agent 真实协议接入、人工审批和操作审计
- [ ] Grafana 仪表盘和告警规则预配置
- [ ] HTTPS 反向代理部署模板与安全响应头加固
- [ ] 世界数据和 SQLite 自动备份、恢复及备份校验
- [ ] 前端按需加载、包体优化和端到端测试
- [ ] 多服务器实例管理

## 技术栈

Go 1.23+、标准库 HTTP、Gorilla WebSocket、SQLite、gopsutil、Prometheus client；Vue 3、TypeScript、Vite、Pinia、Vue Router、Element Plus。

## 本地启动

要求 Ubuntu 22.04+、Go 1.23+、Node.js 20+。将官方 Bedrock 服务端完整解压到仓库根目录 `bedrock-server/`，并确保二进制可执行：

```bash
chmod +x bedrock-server/bedrock_server
```

后端默认自动使用该目录；也可以创建 `backend/config/app.env`，参考 `app.env.example` 覆盖绝对路径：

```env
MC_ROOT_DIR=/opt/minecraft/bedrock
MC_EXECUTABLE_PATH=/opt/minecraft/bedrock/bedrock_server
MC_WORKING_DIR=/opt/minecraft/bedrock
AUTH_ENABLED=true
ADMIN_USERNAME=admin
ADMIN_PASSWORD=请设置至少12个字符的强密码
```

鉴权默认开启。数据库中没有用户时，后端使用上述环境变量创建首个管理员；`ADMIN_PASSWORD` 不会写入数据库明文，后续启动也不会覆盖已有管理员。登录会话默认有效 24 小时，可通过 `SESSION_TTL_HOURS` 调整。

启动后端：

```bash
cd backend
go run ./cmd/panel
```

启动前端开发服务器：

```bash
cd frontend
npm install
npm run dev
```

访问 `http://localhost:5173`。后端监听 `http://localhost:8080`，SQLite 默认保存到 `backend/data/panel.db`。

## Docker Compose

`bedrock-server/` 必须位于项目根目录。容器内后端以非 root 用户运行，因此宿主机目录必须允许 UID `10001` 读写世界和配置文件。

```bash
cd deploy
printf 'ADMIN_PASSWORD=请替换为至少12个字符的强密码\n' > .env
docker compose up --build -d
```

面板、Prometheus、Grafana 默认分别使用端口 80、9090、3000；后端仅在 Compose 内部网络暴露 8080。生产部署应移除不需要公开的端口，并使用 Caddy/Nginx 提供 TLS。

## Prometheus

后端的 `GET /metrics` 可直接抓取。Compose 已加载 `deploy/prometheus/prometheus.yml`，以 15 秒间隔抓取 `backend:8080`。

## 测试与构建

```bash
cd backend && go test ./...
cd frontend && npm run build
```

## 安全边界

- 面板不提供 Shell、任意文件读写或密钥读取能力。
- 配置路径限制在三个白名单文件，并拒绝绝对路径、路径穿越和软链接。
- 除健康检查、登录和 Prometheus `/metrics` 外，REST 与 WebSocket 接口均要求登录并执行角色权限检查。`/metrics` 是机器抓取接口，部署时不得直接暴露后端端口。
- 会话令牌只保存在服务端哈希和 `HttpOnly`、`SameSite=Strict` Cookie 中；HTTPS 下自动设置 `Secure`。
- `bedrock-server/`、世界数据、SQLite 和 `.env` 均被 Git 忽略；请单独执行可靠备份。

## MCP / Agent 规划

`backend/internal/mcp` 仅预留工具风险模型。未来只读诊断可以直接授权，配置变更提案属于低风险；停止、重启、发送命令、应用配置等高风险工具必须人工批准并写审计。不会提供 `shell.exec`、任意文件访问或秘密读取工具。
