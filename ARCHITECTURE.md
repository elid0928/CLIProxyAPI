# CLIProxyAPI 架构文档

## 目录

- [概述](#概述)
- [系统架构](#系统架构)
- [核心组件](#核心组件)
- [数据流](#数据流)
- [关键设计模式](#关键设计模式)
- [扩展性设计](#扩展性设计)
- [部署架构](#部署架构)

---

## 概述

CLIProxyAPI 是一个高度模块化的 Go 语言代理服务器，通过协议转换和认证管理，将各种 CLI AI 工具（Google Gemini CLI、Claude Code、OpenAI Codex 等）的 OAuth 订阅暴露为标准化的 API 接口（OpenAI/Gemini/Claude 兼容）。

### 核心价值主张

1. **协议无关性**: 自动转换不同 AI 提供商之间的协议格式
2. **认证抽象**: 统一管理多种 OAuth 流程和 API Key 凭证
3. **负载均衡**: 支持多账户轮询和填充优先策略
4. **零停机热重载**: 配置和凭证变更无需重启服务
5. **SDK 化设计**: 可作为独立服务或嵌入式库使用

---

## 系统架构

### 整体分层架构

```
┌─────────────────────────────────────────────────────────────┐
│                    CLI Entry Point                          │
│                   (cmd/server/main.go)                       │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ Config Loading │ Flag Parsing │ OAuth Login Flows   │   │
│  └──────────────────────────────────────────────────────┘   │
└────────────────────┬────────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────────┐
│                    Service Layer (SDK)                       │
│                    (sdk/cliproxy)                            │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Builder Pattern   │   Service Orchestrator          │   │
│  │  Hot-Reload Engine │   HTTP Server Lifecycle         │   │
│  └──────────────────────────────────────────────────────┘   │
└───┬──────────────────┬──────────────────┬──────────────────┘
    │                  │                  │
    │                  │                  │
┌───▼──────┐    ┌──────▼──────┐   ┌──────▼──────────────────┐
│   Auth   │    │   Watcher   │   │   API Handlers          │
│  Manager │    │   System    │   │  (internal/api)         │
│          │    │             │   │                         │
│ Token    │    │ fsnotify   │   │ OpenAI │ Claude │ Gemini│
│ Stores   │    │ Config Diff │   │ Codex  │ WebSocket     │
│          │    │ Synthesizer │   │ Management API         │
└───┬──────┘    └─────────────┘   └────────┬────────────────┘
    │                                       │
┌───▼───────────────────────────────────────▼────────────────┐
│              Runtime Executors Layer                        │
│              (internal/runtime/executor)                    │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ Gemini │ Claude │ Codex │ Antigravity │ Qwen │ etc. │  │
│  │ ─────────────────────────────────────────────────────│  │
│  │ • HTTP Client Management                             │  │
│  │ • Token Refresh & Cooldown                           │  │
│  │ • Quota Detection & Switching                        │  │
│  │ • Usage Statistics Aggregation                       │  │
│  └──────────────────────────────────────────────────────┘  │
└───────────────────────┬────────────────────────────────────┘
                        │
┌───────────────────────▼────────────────────────────────────┐
│              Translator Registry                            │
│              (internal/translator)                          │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ init() based auto-registration                       │  │
│  │ openai → claude  │  claude → gemini                  │  │
│  │ openai → gemini  │  gemini → openai                  │  │
│  │ ... (bidirectional protocol conversion)              │  │
│  └──────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────┘
```

---

## 核心组件

### 1. Service Layer (`sdk/cliproxy`)

**职责**: 服务生命周期管理和组件协调

#### Service 结构

```go
type Service struct {
    cfg            *config.Config
    authManager    *auth.Manager
    watcher        *watcher.Watcher
    server         *api.Server
    // ... 更多字段
}
```

**关键功能**:
- `Run(ctx)`: 启动 HTTP 服务器、配置监听器和令牌自动刷新
- `UpdateClients()`: 热重载配置和凭证
- `handleAuthUpdate()`: 异步处理文件系统事件并触发重载

#### Builder Pattern

```go
svc, err := cliproxy.NewBuilder().
    WithConfig(cfg).
    WithConfigPath("config.yaml").
    WithTokenClientProvider(customProvider).
    WithHooks(lifecycleHooks).
    Build()
```

**可注入组件**:
- `TokenClientProvider`: 自定义凭证加载逻辑
- `APIKeyClientProvider`: API Key 凭证管理
- `WatcherFactory`: 配置监听器工厂
- `ServerOptions`: HTTP 中间件和路由定制

---

### 2. Authentication System (`sdk/cliproxy/auth` + `sdk/auth`)

#### Auth Manager

**核心职责**:
- 凭证选择（基于模型前缀、负载均衡策略）
- 凭证生命周期管理（冷却、配额超限、过期刷新）
- 执行器调度

**凭证选择流程**:
```
1. 解析模型名称（检查前缀，如 "gemini/gemini-2.5-pro"）
2. 筛选符合条件的凭证（前缀匹配 + 未冷却 + 未配额超限）
3. 应用路由策略（round-robin 或 fill-first）
4. 返回可用凭证或触发重试逻辑
```

#### Token Stores

支持多种持久化后端：
- **File Store**: 本地文件系统（默认 `~/.cli-proxy-api/auths/`）
- **Git Store**: Git 仓库存储（支持多实例同步）
- **Postgres Store**: PostgreSQL 数据库（企业级部署）
- **Object Store**: S3 兼容对象存储（云原生部署）

**注册机制**:
```go
sdkAuth.RegisterTokenStore("git", gitStoreFactory)
sdkAuth.RegisterTokenStore("postgres", pgStoreFactory)
```

---

### 3. Runtime Executors (`internal/runtime/executor`)

**接口定义**:
```go
type Executor interface {
    Execute(ctx, auth, payload) (*Response, error)
    ExecuteStream(ctx, auth, payload, writer)
    Refresh(auth) error
    PrepareRequest(auth, payload) (*http.Request, error)
}
```

**每个执行器的职责**:
1. **协议翻译**: 调用 Translator 转换请求/响应格式
2. **认证注入**: 添加 OAuth Token 或 API Key 到请求头
3. **上游通信**: 与 AI 提供商 API 交互（HTTP/SSE）
4. **配额处理**: 检测 `429 Quota Exceeded` 错误并触发项目/模型切换
5. **令牌刷新**: 自动刷新过期的 OAuth Token
6. **使用统计**: 聚合输入/输出 Token 数量

**关键实现示例** (简化):
```go
// gemini_cli_executor.go
func (e *GeminiCLIExecutor) Execute(ctx, auth, payload) {
    // 1. 翻译请求
    geminiReq := translator.Translate("openai", "gemini", payload)

    // 2. 注入认证
    req.Header.Set("Authorization", "Bearer " + auth.AccessToken)

    // 3. 发送请求
    resp := e.httpClient.Do(req)

    // 4. 处理配额超限
    if resp.StatusCode == 429 {
        e.handleQuotaExceeded(auth)
    }

    // 5. 翻译响应
    return translator.Translate("gemini", "openai", resp)
}
```

---

### 4. Translator Registry (`internal/translator`)

**自动注册机制**:
每个翻译器子包通过 `init()` 函数自动注册：

```go
// internal/translator/openai/claude/translator.go
func init() {
    translator.Register("openai", "claude", translateOpenAIToClaude)
    translator.Register("claude", "openai", translateClaudeToOpenAI)
}
```

**目录结构示例**:
```
translator/
├── openai/claude/      # OpenAI ↔ Claude
├── openai/gemini/      # OpenAI ↔ Gemini
├── claude/gemini/      # Claude ↔ Gemini
├── codex/gemini/       # Codex ↔ Gemini
└── antigravity/        # Antigravity 协议转换
```

**核心转换逻辑**:
- **Messages**: 角色映射（system/user/assistant）
- **Tools/Function Calling**: JSON Schema 转换
- **Thinking Modes**: 推理模式转换（Claude Extended Thinking ↔ Gemini Thought）
- **Multimodal**: 图像/文件格式标准化

---

### 5. Configuration Watcher (`internal/watcher`)

**组件**:
- `Watcher`: 使用 `fsnotify` 监听文件系统事件
- `Synthesizer`: 根据 OAuth 模型别名生成虚拟配置
- `Diff`: 计算配置变更的最小差异集

**热重载流程**:
```
1. fsnotify 检测 config.yaml 或 auths/ 目录变更
2. Watcher 触发 debounced 事件（防抖 100ms）
3. Synthesizer 生成虚拟模型别名配置
4. Diff 计算新旧配置的差异
5. Service.handleAuthUpdate() 应用增量更新
6. Auth Manager 重新加载凭证和路由规则
```

**零停机保证**:
- 进行中的请求继续使用旧配置
- 新请求立即使用新配置
- 无需重启 HTTP 服务器

---

### 6. API Handlers (`internal/api`)

**路由结构**:
```
/v1/chat/completions       → OpenAI 兼容端点
/v1/messages               → Claude 兼容端点
/v1beta/generateContent    → Gemini 兼容端点
/v1/ws                     → WebSocket 实时流式
/api/provider/{provider}/v1 → Amp CLI 路由别名
/v0/management/*           → 管理 API
```

**中间件链**:
1. `AuthMiddleware`: API Key 验证
2. `CORSMiddleware`: 跨域处理
3. `RateLimitMiddleware`: 速率限制（商业模式）
4. `LoggingMiddleware`: 请求日志

**Management API** (`handlers/management/`):
- `/v0/management/config`: 运行时配置更新
- `/v0/management/accounts`: 账户状态查询
- `/v0/management/usage`: 使用统计查询
- `/v0/management/logs`: 日志下载

**安全设计**:
- `allow-remote: false`: 仅允许 localhost 访问（默认）
- `secret-key`: 强制密钥认证（SHA256 哈希）
- 密钥错误时返回 404（防止端点探测）

---

### 7. Usage Statistics (`internal/usage`)

**统计维度**:
- 每个凭证的输入/输出 Token 数
- 每个模型的调用次数
- 时间序列聚合（小时/天/月）

**持久化选项**:
- **内存模式**: 仅运行时统计（重启丢失）
- **MySQL**: 企业级持久化 + 预聚合日报表
- **SQLite**: 轻量级本地持久化

**批量写入机制**:
```yaml
usage-mysql:
  batch-size: 100          # 100 条记录批量插入
  flush-interval-seconds: 5 # 5 秒强制刷盘
```

---

## 数据流

### 典型请求处理流程

```
┌────────┐  POST /v1/chat/completions
│ Client │  { model: "gemini/gemini-2.5-pro", ... }
└───┬────┘
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ API Handler (handlers/openai_handler.go)                │
│  • 解析 JSON Payload                                    │
│  • 提取 model name: "gemini/gemini-2.5-pro"            │
│  • 调用 AuthMiddleware 验证 API Key                    │
└───┬─────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ Auth Manager (sdk/cliproxy/auth/manager.go)             │
│  • 解析模型前缀: prefix="gemini", model="gemini-2.5-pro"│
│  • 筛选可用凭证（前缀匹配 + 未冷却）                    │
│  • 应用 round-robin 策略选择凭证 #3                    │
└───┬─────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ Executor (internal/runtime/executor/gemini_cli.go)      │
│  • 检查协议: input=openai, output=gemini               │
│  • 调用 Translator("openai", "gemini")                  │
│  • 转换请求: messages → contents, tools → function_declarations │
│  • 注入 OAuth Token: Authorization: Bearer ya29...     │
│  • 应用 Payload Rules (default/override 参数注入)      │
└───┬─────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ Upstream API (https://generativelanguage.googleapis.com)│
│  POST /v1beta/models/gemini-2.5-pro:generateContent     │
└───┬─────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ Response Translation                                     │
│  • Gemini JSON → OpenAI JSON                            │
│  • candidates[0].content → choices[0].message           │
│  • usageMetadata → usage { prompt_tokens, ... }         │
│  • 流式响应: SSE 逐行转换                               │
└───┬─────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ Usage Statistics                                         │
│  • 记录: credential="gemini#3", model="gemini-2.5-pro"  │
│  • 累加: input_tokens=1024, output_tokens=512           │
│  • 批量写入 MySQL/SQLite（如启用）                      │
└───┬─────────────────────────────────────────────────────┘
    │
    ▼
┌────────┐
│ Client │ ← HTTP 200 + OpenAI-compatible JSON
└────────┘
```

---

## 关键设计模式

### 1. 接口驱动设计

**核心接口**:
- `Executor`: 执行器接口，所有提供商统一实现
- `TokenStore`: 令牌存储抽象，支持多种后端
- `TranslateFunc`: 协议转换函数签名

**优势**:
- 新增提供商只需实现 `Executor` 接口
- 切换存储后端无需修改业务逻辑
- 单元测试可轻松 Mock

### 2. init() 自动注册

**应用场景**:
- Translator 注册: `translator.Register("openai", "claude", fn)`
- Token Store 注册: `sdkAuth.RegisterTokenStore("git", factory)`

**优势**:
- 解耦核心框架和具体实现
- 新增组件无需修改注册中心代码
- 支持插件化扩展

### 3. Builder 模式

```go
svc := cliproxy.NewBuilder().
    WithConfig(cfg).
    WithServerOptions(customMiddleware).
    Build()
```

**优势**:
- 流式 API，可读性强
- 可选参数灵活组合
- 默认值自动填充

### 4. 策略模式

**凭证选择策略**:
- `RoundRobinSelector`: 轮询所有可用凭证
- `FillFirstSelector`: 优先填满单个凭证配额

**配置切换**:
```yaml
routing:
  strategy: "round-robin" # 或 "fill-first"
```

### 5. 观察者模式

**配置热重载**:
- `Watcher` 观察文件系统事件
- `Service` 订阅配置变更通知
- `Auth Manager` 响应凭证更新

---

## 扩展性设计

### 1. 新增 AI 提供商

**步骤**:
1. 实现 `Executor` 接口（`internal/runtime/executor/newprovider_executor.go`）
2. 实现协议转换器（`internal/translator/openai/newprovider/`）
3. 在 `init()` 中注册翻译器
4. 更新 `Auth.Channel` 类型定义

**无需修改**:
- Auth Manager 自动支持新凭证类型
- API Handlers 无需感知新提供商
- 配置系统自动解析新配置段

### 2. 自定义存储后端

```go
// 实现 TokenStore 接口
type CustomStore struct {}

func (s *CustomStore) List() ([]*Auth, error) { ... }
func (s *CustomStore) Save(auth *Auth) error { ... }
func (s *CustomStore) Delete(id string) error { ... }

// 注册到系统
sdkAuth.RegisterTokenStore("custom", func(cfg) TokenStore {
    return &CustomStore{}
})
```

### 3. SDK 嵌入式使用

```go
import "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"

// 在自己的应用中嵌入代理服务
svc, _ := cliproxy.NewBuilder().
    WithConfig(cfg).
    WithHooks(cliproxy.Hooks{
        OnBeforeStart: func() { log.Println("Starting...") },
    }).
    Build()

go svc.Run(ctx)
```

**用例**:
- 桌面应用内置代理（如 vibeproxy、ProxyPal）
- Serverless 函数内嵌
- 测试工具集成

---

## 部署架构

### 单机部署

```
┌─────────────────────────────────────┐
│  CLIProxyAPI Binary                 │
│  ├── config.yaml (本地文件)         │
│  └── ~/.cli-proxy-api/auths/        │
│      ├── gemini-account1.json       │
│      ├── claude-account2.json       │
│      └── codex-account3.json        │
└─────────────────────────────────────┘
          │
          ▼ HTTP :8317
    ┌───────────┐
    │  Clients  │
    └───────────┘
```

**适用场景**: 个人开发者、小团队

---

### 云原生部署 (Kubernetes)

```
┌─────────────────────────────────────────────────────────┐
│  Kubernetes Cluster                                     │
│  ┌─────────────────────────────────────────────────┐   │
│  │  ConfigMap: config.yaml                         │   │
│  └─────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────┐   │
│  │  Deployment: cliproxy (3 replicas)              │   │
│  │  ├── DEPLOY=cloud                               │   │
│  │  ├── PGSTORE_DSN=postgres://...                 │   │
│  │  └── Readiness: /v1/models                      │   │
│  └─────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────┐   │
│  │  Service: LoadBalancer                          │   │
│  └─────────────────────────────────────────────────┘   │
└──────────────────┬──────────────────────────────────────┘
                   │
                   ▼ External IP
             ┌───────────┐
             │  Internet │
             └───────────┘

┌─────────────────────────────────────────────────────────┐
│  External Postgres (RDS/CloudSQL)                       │
│  Table: oauth_tokens                                    │
│  └── Shared state for all replicas                     │
└─────────────────────────────────────────────────────────┘
```

**关键配置**:
```yaml
# config.yaml
auth-dir: "" # 不使用文件系统

# 环境变量
PGSTORE_DSN: "postgres://user:pass@db:5432/cliproxy"
DEPLOY: "cloud" # 等待配置文件出现后再启动
```

**优势**:
- 水平扩展（多实例共享凭证）
- 高可用（实例故障自动恢复）
- 零停机更新（滚动发布）

---

### 多实例同步 (Git 后端)

```
┌─────────────────────┐       ┌─────────────────────┐
│  Instance 1         │       │  Instance 2         │
│  └── GITSTORE_*     │       │  └── GITSTORE_*     │
└────────┬────────────┘       └────────┬────────────┘
         │                             │
         └─────────────┬───────────────┘
                       ▼
            ┌──────────────────────┐
            │  Git Repository      │
            │  (Private GitHub)    │
            │  auths/              │
            │  ├── gemini1.json    │
            │  └── claude1.json    │
            └──────────────────────┘
```

**配置示例**:
```bash
export GITSTORE_GIT_URL="https://github.com/user/cliproxy-tokens"
export GITSTORE_GIT_USERNAME="bot"
export GITSTORE_GIT_TOKEN="ghp_..."
```

**同步机制**:
- 每次凭证更新自动 Git Commit + Push
- 定期 Pull 检测远程变更
- 支持多地域部署同步

---

## 性能优化

### 1. 商业模式 (High Concurrency)

```yaml
commercial-mode: true
```

**优化措施**:
- 禁用高开销中间件（如详细日志）
- 减少请求上下文内存分配
- 连接池复用优化

### 2. 连接池管理

每个 Executor 维护独立的 HTTP 客户端：
```go
&http.Client{
    Timeout: 120 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     90 * time.Second,
    },
}
```

### 3. 流式响应优化

- SSE 流式传输避免大响应内存峰值
- 逐块翻译（不等待完整响应）
- Keep-Alive 防止空闲超时断连

### 4. 缓存策略

- 模型列表缓存（减少上游 `/models` 请求）
- OAuth Token 内存缓存（避免频繁文件 I/O）
- 配置 Diff 缓存（热重载时减少比较开销）

---

## 安全设计

### 1. 令牌存储加密

**建议实践**:
- 文件系统权限: `chmod 600 auths/*.json`
- Git 仓库私有化（Private Repository）
- Postgres 连接 TLS 加密
- 环境变量注入敏感配置（避免硬编码）

### 2. Management API 防护

- **默认拒绝远程访问**: `allow-remote: false`
- **强密钥认证**: SHA256 哈希验证
- **404 错误伪装**: 密钥错误返回 404（而非 401）

### 3. CORS 配置

```go
c.Header("Access-Control-Allow-Origin", "*") // 开发模式
// 生产环境应限制为特定域名
```

---

## 可观测性

### 1. 日志系统

**日志级别**:
- `debug: true`: 详细请求/响应日志
- `logging-to-file: true`: 日志轮转持久化

**日志内容**:
- HTTP 请求元数据（路径、方法、状态码）
- 凭证选择决策
- 协议转换细节
- 配额超限事件

### 2. 使用统计

**查询接口**:
```bash
GET /v0/management/usage?start=2025-01-01&end=2025-01-31
```

**响应示例**:
```json
{
  "credentials": {
    "gemini-account1": {
      "input_tokens": 1024000,
      "output_tokens": 512000,
      "requests": 3456
    }
  },
  "models": {
    "gemini-2.5-pro": { "requests": 2345 }
  }
}
```

### 3. 健康检查

```bash
GET /v1/models # 200 OK 表示服务正常
```

---

## 故障处理

### 1. 配额超限处理

**行为**:
```yaml
quota-exceeded:
  switch-project: true  # 自动切换到备用项目
  switch-preview-model: true # 切换到预览版模型
```

**流程**:
1. 检测 429 状态码
2. 标记当前凭证为配额超限
3. 尝试切换到同账户其他项目
4. 若失败，切换到下一个可用凭证
5. 触发指数退避冷却

### 2. 令牌过期自动刷新

```go
coreManager.StartAutoRefresh(ctx, 10*time.Minute) // 每 10 分钟检查一次
```

**刷新策略**:
- 检测 `expires_at < now + 5min`
- 调用 `Executor.Refresh(auth)`
- 更新 Token Store 持久化

### 3. 请求重试机制

```yaml
request-retry: 3 # 最多重试 3 次
max-retry-interval: 30 # 最多等待冷却凭证 30 秒
```

**重试条件**:
- HTTP 状态码: 403, 408, 500, 502, 503, 504
- 凭证冷却中（等待 `max-retry-interval` 秒）
- 配额超限（自动切换凭证）

---

## 总结

CLIProxyAPI 通过精心设计的分层架构和模块化组件，实现了：

1. **高内聚低耦合**: 每个组件职责单一，接口清晰
2. **高扩展性**: 新增提供商/存储后端无需修改核心代码
3. **高可用性**: 支持多实例部署、自动故障切换
4. **零停机运维**: 配置热重载、滚动更新
5. **生产级性能**: 连接池、流式处理、批量写入优化

核心设计理念：**协议转换与凭证调度完全解耦**，使系统既能作为独立服务运行，也能作为强大的 SDK 嵌入其他应用。
