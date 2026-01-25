# CLIProxyAPI 产品文档

## 目录

- [产品概述](#产品概述)
- [核心价值](#核心价值)
- [目标用户](#目标用户)
- [功能特性](#功能特性)
- [应用场景](#应用场景)
- [竞争优势](#竞争优势)
- [商业价值](#商业价值)
- [生态系统](#生态系统)
- [路线图](#路线图)

---

## 产品概述

**CLIProxyAPI** 是一个开源的 AI 代理服务器，它将各种 CLI AI 工具（如 Google Gemini CLI、Claude Code、OpenAI Codex）的 OAuth 订阅转换为标准化的 API 接口。

### 一句话描述

> 将你的 Google/ChatGPT/Claude 订阅转化为强大的 AI API，无需额外支付 API 费用。

### 核心理念

**"订阅优于按量付费"** - 大多数 AI 提供商提供两种付费模式：
- **订阅模式**: 固定月费，不限量使用（如 Google One AI Premium $20/月）
- **API 模式**: 按 Token 计费，用多少付多少

CLIProxyAPI 让用户用订阅模式的成本，获得 API 模式的灵活性。

---

## 核心价值

### 1. 成本节约

**对比场景**:

| 使用场景 | 传统 API 费用 | 使用 CLIProxyAPI | 月节省 |
|---------|--------------|-----------------|--------|
| AI 编程助手（日均 100k tokens） | ~$150/月 | $20/月（Google One） | **$130** |
| 内容创作工作室（3 个作者） | ~$300/月 | $60/月（3 个订阅） | **$240** |
| 企业 AI 研发团队（10 人） | ~$1500/月 | $200/月（10 个订阅） | **$1300** |

**关键洞察**: 当月使用量超过订阅阈值时，ROI 显著。

---

### 2. 统一接口

**问题**: AI 工具生态碎片化
- Cursor 只支持 OpenAI 格式
- Continue.dev 需要 Claude 格式
- Google AI Studio 要求 Gemini 格式

**解决方案**: 一个代理，兼容所有格式
```bash
# 同一个 Gemini 订阅账户
curl http://localhost:8317/v1/chat/completions    # OpenAI 格式
curl http://localhost:8317/v1/messages            # Claude 格式
curl http://localhost:8317/v1beta/generateContent # Gemini 格式
```

---

### 3. 多账户负载均衡

**场景**: 企业团队有 5 个 Google One AI Premium 账户

**传统做法**: 手动分配账户给不同团队成员（难以管理）

**CLIProxyAPI 做法**:
```yaml
# config.yaml
routing:
  strategy: "round-robin" # 自动轮询 5 个账户

api-keys:
  - "team-shared-key" # 团队共享一个密钥
```

**效果**:
- 自动分散负载到 5 个账户
- 单个账户配额用尽自动切换
- 无缝故障转移

---

### 4. 零停机运维

**场景**: 添加新账户或更新配置

**传统做法**: 重启服务器（影响所有用户）

**CLIProxyAPI**: 热重载
```bash
# 编辑配置文件
vim config.yaml

# 100ms 内自动生效，无需重启
# 正在进行的请求不受影响
```

---

## 目标用户

### 主要用户群体

#### 1. 个人开发者

**痛点**:
- 已购买 Claude Pro ($20/月) 用于日常对话
- 想在 Cursor/VS Code 中使用 Claude，但 API 额外收费
- 不想为同一服务支付两次费用

**价值**:
- 用现有订阅驱动所有 AI 开发工具
- 月节省 $50-100（避免 API 费用）

---

#### 2. AI 应用开发团队

**痛点**:
- 开发阶段 API 调用频繁，成本不可控
- 测试环境需要独立凭证管理
- 多个 AI 提供商需要不同的 SDK 集成

**价值**:
- 开发/测试环境使用订阅账户（成本固定）
- 统一 SDK 集成（嵌入 CLIProxyAPI 为库）
- 生产环境按需切换为正式 API

---

#### 3. 内容创作工作室

**痛点**:
- 多名作者使用 AI 辅助写作
- 需要统一管理账户和配额
- 需要使用统计和成本控制

**价值**:
- 集中管理所有作者的 AI 访问
- 实时查看每个作者的 Token 使用量
- 按需添加账户扩展容量

---

#### 4. 企业 IT 部门

**痛点**:
- 需要为内部部署 AI 服务网关
- 合规要求：数据不能发送到第三方 API 端点
- 需要审计和日志记录

**价值**:
- 私有部署（数据不出公司网络）
- 完整的使用日志和审计跟踪
- 支持 Kubernetes 水平扩展

---

## 功能特性

### 核心功能

#### 1. 多协议支持

| 协议 | 支持特性 |
|------|---------|
| **OpenAI** | 流式/非流式、Function Calling、Vision、Reasoning |
| **Claude** | Extended Thinking、Tool Use、PDF 支持 |
| **Gemini** | Thought Mode、Multimodal、Video 输入 |
| **Codex** | OpenAI 特有推理模型 |

**自动转换示例**:
```json
// 客户端发送 OpenAI 格式
{
  "model": "gemini-2.5-pro",
  "messages": [{"role": "user", "content": "Hello"}]
}

// 自动转换为 Gemini 格式并请求上游
{
  "model": "gemini-2.5-pro",
  "contents": [{"role": "user", "parts": [{"text": "Hello"}]}]
}
```

---

#### 2. OAuth 多账户管理

**支持的 OAuth 提供商**:
- ✅ Google (Gemini CLI, AI Studio, Vertex AI)
- ✅ OpenAI (Codex)
- ✅ Anthropic (Claude Code)
- ✅ Alibaba (Qwen Code)
- ✅ Deepseek (iFlow)
- ✅ Google Deepmind (Antigravity)

**一键登录**:
```bash
# 登录 Gemini 账户
./cliproxy-server -login

# 登录 Claude Code 账户
./cliproxy-server -claude-login

# 登录 OpenAI Codex 账户
./cliproxy-server -codex-login
```

**令牌管理**:
- 自动刷新过期的 OAuth Token
- 支持文件/Git/Postgres/S3 存储
- 多实例共享凭证（通过数据库或 Git）

---

#### 3. 智能路由

**模型前缀路由**:
```yaml
# 配置多个账户组
auths/
├── work-gemini-1.json   # prefix: "work"
├── personal-gemini.json # prefix: "personal"
└── test-claude.json     # prefix: "test"
```

```bash
# 请求定向到特定账户组
curl -X POST http://localhost:8317/v1/chat/completions \
  -d '{"model": "work/gemini-2.5-pro", ...}'  # 使用 work 组账户

curl -X POST http://localhost:8317/v1/chat/completions \
  -d '{"model": "personal/gemini-2.5-pro", ...}'  # 使用 personal 组账户
```

**负载均衡策略**:
- **Round Robin**: 轮询所有账户（默认）
- **Fill First**: 优先填满单个账户配额（适合成本优化）

---

#### 4. 配额智能处理

**自动故障转移**:
```yaml
quota-exceeded:
  switch-project: true   # 账户 A 配额用尽 → 切换到项目 B
  switch-preview-model: true  # 正式模型配额用尽 → 切换到预览版模型
```

**冷却机制**:
- 配额超限的账户进入指数退避冷却
- 冷却期间自动使用其他账户
- 配额恢复后自动重新加入轮询

---

#### 5. Payload 参数注入

**场景**: 强制所有 Gemini 请求启用思考模式

```yaml
payload:
  override: # 强制覆盖参数
    - models:
        - name: "gemini-2.5-pro"
      params:
        "generationConfig.thinkingConfig.thinkingBudget": 32768
```

**效果**: 客户端无需修改代码，服务端统一注入参数。

---

#### 6. Management API

**实时配置管理**:
```bash
# 查看所有账户状态
GET /v0/management/accounts
{
  "accounts": [
    {
      "id": "gemini-account1",
      "status": "active",
      "quota_remaining": "80%",
      "cooldown_until": null
    }
  ]
}

# 动态添加账户
POST /v0/management/accounts
{
  "provider": "gemini-cli",
  "credentials": { ... }
}

# 查询使用统计
GET /v0/management/usage?start=2025-01-01&end=2025-01-31
```

**控制面板**:
- 自动下载并托管 Web UI（来自 GitHub Release）
- 可视化账户管理和使用统计
- 支持一键导入/导出配置

---

### 高级功能

#### 1. SDK 嵌入式部署

**用例**: 桌面 AI 工具内置代理

```go
import "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"

// 在 Electron/Tauri 应用中嵌入代理
svc, _ := cliproxy.NewBuilder().
    WithConfig(cfg).
    Build()

go svc.Run(ctx) // 启动内嵌 HTTP 服务器
```

**生态案例**:
- **vibeproxy**: macOS 菜单栏应用（原生集成）
- **ProxyPal**: macOS GUI 管理工具
- **CodMate**: SwiftUI 会话管理器
- **ZeroLimit**: Tauri 桌面监控面板

---

#### 2. Amp CLI 集成

**Amp CLI** 是一个 AI 编程工具，CLIProxyAPI 提供官方支持：

**功能**:
- 代理 Amp 的 OAuth 认证流程
- 模型映射（将不可用模型路由到可用模型）
- 管理端点代理（账户信息、配额查询）

**配置示例**:
```yaml
ampcode:
  upstream-url: "https://ampcode.com"
  model-mappings:
    - from: "claude-opus-4-5"  # Amp 请求的模型
      to: "gemini-2.5-pro"     # 实际使用的模型
```

---

#### 3. 使用统计持久化

**MySQL 持久化**:
```yaml
usage-mysql:
  enable: true
  dsn: "user:pass@tcp(localhost:3306)/cliproxy"
  batch-size: 100
  flush-interval-seconds: 5
  enable-daily-stats: true  # 预聚合日报表
```

**SQLite 持久化**:
```yaml
usage-sqlite:
  enable: true
  db-path: "~/.cli-proxy-api/usage.db"
```

**价值**:
- 历史数据分析（成本核算、趋势预测）
- 审计合规（留存所有请求记录）
- 性能调优（识别高频模型/用户）

---

#### 4. Claude API 请求隐写 (Cloak)

**问题**: 某些 Claude API 端点检测并限制代理行为

**解决方案**: 隐写模式（Obfuscation）
```yaml
claude-api-key:
  - api-key: "sk-ant-..."
    cloak:
      mode: "auto"         # 仅在非 Claude Code 客户端时启用
      strict-mode: false   # 保留用户系统消息
      sensitive-words:     # 敏感词混淆（零宽字符）
        - "API"
        - "proxy"
```

**效果**: 请求看起来像是来自 Claude Code 官方客户端。

---

## 应用场景

### 场景 1: AI 编程团队

**背景**: 10 人研发团队，使用 Cursor IDE 进行 AI 辅助开发

**传统方案**:
- 每人购买 Cursor Pro ($20/月/人) + OpenAI API 额度 ($50/月/人)
- **总成本**: $700/月

**CLIProxyAPI 方案**:
```yaml
# 5 个 Google One AI Premium 账户 ($100/月)
# 配置 CLIProxyAPI 为 Cursor 后端
# 总成本: $100/月
```

**ROI**: 节省 $600/月 (85%)

---

### 场景 2: 多模型 A/B 测试

**背景**: AI 应用需要对比不同模型的输出质量

**挑战**: 集成多个 AI 提供商 SDK（OpenAI、Claude、Gemini）

**CLIProxyAPI 方案**:
```python
# 统一接口，切换模型只需改 model 参数
import openai
client = openai.OpenAI(base_url="http://localhost:8317/v1")

# 测试 Gemini
response = client.chat.completions.create(
    model="gemini-2.5-pro",
    messages=[...]
)

# 测试 Claude（无需改代码）
response = client.chat.completions.create(
    model="claude-sonnet-4-5",
    messages=[...]
)
```

**价值**:
- 无需集成多个 SDK
- 统一错误处理和重试逻辑
- 轻松记录和对比结果

---

### 场景 3: 私有部署合规要求

**背景**: 金融企业要求 AI 数据不能发送到外部 API

**问题**: 无法使用 OpenAI/Claude 的公有云 API

**CLIProxyAPI 方案**:
1. 部署 CLIProxyAPI 在内网
2. 使用企业的 Google Workspace 账户（私有 Vertex AI 端点）
3. 所有 AI 请求走内部网关

**合规优势**:
- 数据不出公司网络
- 完整审计日志（记录所有 prompt）
- 访问控制（API Key 管理）

---

### 场景 4: 桌面 AI 工具开发

**背景**: 开发一个 AI 写作助手桌面应用

**挑战**:
- 用户不想配置复杂的 API Key
- 想复用现有的 ChatGPT Plus 订阅

**CLIProxyAPI 方案**:
1. 桌面应用内嵌 CLIProxyAPI SDK
2. 引导用户通过 OAuth 登录（浏览器弹窗）
3. 令牌自动存储在本地
4. 应用调用本地 HTTP 接口

**用户体验**:
- 一键登录，无需手动复制 API Key
- 订阅自动续费（跟随 ChatGPT Plus）
- 应用自动更新模型列表

---

## 竞争优势

### 对比其他方案

| 维度 | CLIProxyAPI | OpenRouter | 直接使用 API | 手动脚本 |
|------|------------|------------|-------------|---------|
| **成本** | 订阅费（$20/月） | 按 Token 计费 + 加价 | 按 Token 计费 | 订阅费 |
| **多账户管理** | ✅ 自动负载均衡 | ❌ 单账户 | ❌ 手动轮询 | ⚠️ 需自己实现 |
| **协议转换** | ✅ 全自动 | ✅ 支持 OpenAI 格式 | ❌ 需多个 SDK | ❌ 需自己实现 |
| **热重载** | ✅ 零停机 | N/A | N/A | ❌ 需重启 |
| **私有部署** | ✅ 开源可部署 | ❌ SaaS 服务 | ✅ 官方 API | ✅ 自主控制 |
| **SDK 集成** | ✅ Go 库嵌入 | ❌ 仅 HTTP | ✅ 官方 SDK | ❌ 无 |
| **配额智能切换** | ✅ 自动 | ❌ 无 | ❌ 无 | ⚠️ 需自己实现 |

---

### 核心差异化

#### 1. 订阅优先设计

大多数代理专注于聚合 API Key，CLIProxyAPI 是唯一专注于**将订阅转换为 API** 的开源方案。

#### 2. 企业级特性

- Kubernetes 原生（支持 Readiness Probe）
- 多存储后端（Postgres、Git、S3）
- 使用统计持久化（MySQL/SQLite）
- Management API（运行时配置管理）

#### 3. 生态系统

已有 **10+ 开源项目** 基于 CLIProxyAPI 构建（见生态系统部分），形成良性循环。

---

## 商业价值

### 对个人用户

**投资回报**:
```
初始投入: $0 (开源免费)
月度成本: $20 (1 个 Google One AI Premium)
避免支出: $100-300 (API 费用)
月净节省: $80-280
```

**年度 ROI**: 960% - 3360%

---

### 对企业用户

**成本节省**:

假设 50 人研发团队：

| 项目 | 传统方案 | CLIProxyAPI 方案 | 节省 |
|------|---------|-----------------|------|
| **AI 订阅** | $1000/月（50 人 × $20） | $1000/月 | $0 |
| **API 费用** | $5000/月（高频使用） | $0 | **$5000** |
| **管理成本** | 2 人日/月（手动分配账户） | 0.5 人日/月（自动化） | **$600** |
| **总节省** | - | - | **$5600/月** |

**年度节省**: $67,200

---

### 隐藏价值

#### 1. 避免供应商锁定

**场景**: OpenAI 突然涨价 2 倍

**传统方案**: 被迫接受（切换成本高）

**CLIProxyAPI 方案**:
```yaml
# 一行配置切换到 Gemini
model-mappings:
  - from: "gpt-4"
    to: "gemini-2.5-pro"
```

**价值**: 供应商谈判筹码

---

#### 2. 开发/生产环境分离

**策略**:
- **开发/测试**: 使用订阅账户（通过 CLIProxyAPI）
- **生产环境**: 使用官方 API（按需付费，SLA 保障）

**价值**:
- 开发阶段成本可控
- 生产环境性能优化（无代理层开销）

---

#### 3. 知识产权保护

**场景**: 企业训练专有 AI 模型需要大量 Prompt 数据

**CLIProxyAPI 优势**:
- 所有 Prompt/Response 本地记录
- 构建私有数据集（用于微调模型）
- 审计和合规留痕

---

## 生态系统

### 基于 CLIProxyAPI 的项目

#### 桌面应用

1. **[vibeproxy](https://github.com/automazeio/vibeproxy)**
   - macOS 原生菜单栏应用
   - 一键启动/停止代理
   - 可视化账户切换

2. **[ProxyPal](https://github.com/heyhuynhgiabuu/proxypal)**
   - macOS GUI 管理工具
   - 可视化配置编辑器
   - 模型映射向导

3. **[Quotio](https://github.com/nguyenphutrong/quotio)**
   - macOS 菜单栏配额监控
   - 实时 Token 使用统计
   - 配额预警通知

4. **[ZeroLimit](https://github.com/0xtbug/zero-limit)**
   - Windows Tauri 桌面应用
   - 多账户配额仪表板
   - 系统托盘集成

5. **[ProxyPilot](https://github.com/Finesssee/ProxyPilot)**
   - Windows 原生 TUI + 系统托盘
   - OAuth 多提供商支持
   - 内置配置向导

---

#### 开发工具

6. **[CCS (Claude Code Switch)](https://github.com/kaitranntt/ccs)**
   - CLI 工具快速切换 Claude 账户
   - 支持 Gemini/Codex/Antigravity 备选
   - 会话持久化

7. **[CodMate](https://github.com/loocor/CodMate)**
   - macOS SwiftUI AI 会话管理器
   - Git Review 集成
   - 全局搜索和项目组织

8. **[Claude Proxy VSCode](https://github.com/uzhao/claude-proxy-vscode)**
   - VS Code 扩展
   - 一键切换 Claude 模型
   - 后台自动管理 CLIProxyAPI 进程

---

#### Web 工具

9. **[Subtitle Translator](https://github.com/VjayC/SRT-Subtitle-Translator-Validator)**
   - 浏览器字幕翻译工具
   - 复用 Gemini 订阅
   - 自动校验和纠错

---

#### 替代实现

10. **[9Router](https://github.com/decolua/9router)**
    - Next.js 重新实现（受 CLIProxyAPI 启发）
    - Web 仪表板
    - Combo 系统自动故障转移

---

### 生态价值

**网络效应**: 每个新项目都增加了 CLIProxyAPI 的价值
- 桌面应用降低了使用门槛（GUI）
- CLI 工具提高了高级用户效率
- Web 工具拓展了非技术用户市场

**社区驱动开发**:
- 10+ 外部贡献者
- 100+ GitHub Stars
- 活跃的 Issue 和 PR

---

## 路线图

### 已完成 ✅

- [x] 多协议支持（OpenAI/Claude/Gemini/Codex）
- [x] OAuth 多账户管理（Gemini/Claude/Codex/Qwen/iFlow/Antigravity）
- [x] 负载均衡（Round Robin/Fill First）
- [x] 配置热重载
- [x] Management API
- [x] SDK 化（可嵌入）
- [x] 多存储后端（File/Git/Postgres/S3）
- [x] 使用统计持久化（MySQL/SQLite）
- [x] Amp CLI 集成
- [x] Claude API 请求隐写

---

### 进行中 🚧

- [ ] **Web 控制面板增强**
  - 实时日志查看器
  - 可视化配额曲线
  - 一键导入/导出配置

- [ ] **性能优化**
  - gRPC 内部通信（降低序列化开销）
  - 响应缓存（相同 Prompt 复用结果）
  - 连接池自适应调整

- [ ] **安全增强**
  - OAuth Token 加密存储（AES-256）
  - 双因素认证（Management API）
  - Rate Limiting（防滥用）

---

### 计划中 📅

#### Q2 2025

- [ ] **更多 AI 提供商支持**
  - Mistral AI
  - Cohere
  - Perplexity API

- [ ] **高级路由**
  - 基于成本的路由（自动选择最便宜的模型）
  - 基于延迟的路由（自动选择最快的端点）
  - 自定义路由规则（Lua 脚本）

- [ ] **插件系统**
  - 请求前/后钩子（用于审计、修改）
  - 自定义协议转换器（动态加载）
  - 第三方扩展市场

#### Q3 2025

- [ ] **企业特性**
  - RBAC（基于角色的访问控制）
  - SSO 集成（SAML/OAuth）
  - 多租户支持（隔离配置和数据）

- [ ] **AI 功能**
  - 自动模型选择（根据 Prompt 特征推荐最佳模型）
  - Prompt 优化建议（降低 Token 消耗）
  - 异常检测（识别异常高频请求）

#### Q4 2025

- [ ] **SaaS 版本**
  - 托管服务（无需自部署）
  - 免费层（每月 100 万 Token）
  - 付费层（高级功能 + 技术支持）

---

## 快速开始

### 安装

**二进制下载**:
```bash
# 从 GitHub Release 下载最新版本
wget https://github.com/router-for-me/CLIProxyAPI/releases/latest/download/cliproxy-server-linux-amd64
chmod +x cliproxy-server-linux-amd64
mv cliproxy-server-linux-amd64 /usr/local/bin/cliproxy-server
```

**从源码构建**:
```bash
git clone https://github.com/router-for-me/CLIProxyAPI.git
cd CLIProxyAPI
go build -o cliproxy-server ./cmd/server
```

**Docker**:
```bash
docker pull ghcr.io/router-for-me/cliproxy-api:latest
docker run -p 8317:8317 -v ./config.yaml:/app/config.yaml cliproxy-api
```

---

### 配置

1. **复制示例配置**:
```bash
cp config.example.yaml config.yaml
```

2. **设置 API Key**:
```yaml
api-keys:
  - "your-secret-key-here"
```

3. **登录 OAuth 账户**:
```bash
# 登录 Gemini
./cliproxy-server -login

# 登录 Claude Code
./cliproxy-server -claude-login
```

4. **启动服务**:
```bash
./cliproxy-server
```

---

### 测试

```bash
# 测试 OpenAI 格式端点
curl -X POST http://localhost:8317/v1/chat/completions \
  -H "Authorization: Bearer your-secret-key-here" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-pro",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

---

## 支持

### 文档

- **快速开始指南**: https://help.router-for.me/
- **SDK 文档**: `docs/sdk-usage.md`
- **Management API**: `MANAGEMENT_API.md`
- **架构文档**: `ARCHITECTURE.md`

### 社区

- **GitHub Issues**: 报告 Bug 或功能请求
- **GitHub Discussions**: 提问和讨论
- **赞助商**: 感谢 Z.ai、PackyCode、Cubence 的支持

### 贡献

欢迎贡献代码、文档或提出建议！请参考 `README.md` 的贡献指南。

---

## 许可证

MIT License - 自由使用、修改和分发。

---

## 总结

**CLIProxyAPI 的核心价值主张**:

1. **成本节约**: 将订阅转化为 API，节省 80%+ 费用
2. **统一接口**: 一个代理兼容所有 AI 格式
3. **智能管理**: 多账户负载均衡和配额自动切换
4. **企业级**: 支持私有部署、高可用和合规要求
5. **生态丰富**: 10+ 衍生项目，活跃的开源社区

**适用场景**:
- 个人开发者（降低 AI 工具成本）
- AI 应用开发团队（统一多模型接口）
- 企业 IT 部门（私有部署和审计）
- 桌面工具开发者（嵌入式 SDK）

**立即开始**: https://help.router-for.me/
