# CLIProxyAPI System Architecture Diagram Design

## Purpose

Create a detailed 16:9 architecture diagram for a technical presentation. The diagram should explain the end-to-end request path and the supporting control plane without descending to individual source files.

## Audience

The primary audience is technical stakeholders who need to understand how CLIProxyAPI accepts multiple client protocols, authenticates and schedules accounts, translates requests, applies provider-specific reasoning settings, executes upstream calls, and operates through hot-reload, storage, plugins, and observability components.

## Information Architecture

The diagram uses a left-to-right layered data flow:

1. Client tools and SDKs: OpenAI SDK, Claude Code, Gemini CLI, Codex, and other compatible clients.
2. Access layer: Gin HTTP server, OpenAI/Responses, Claude and Gemini compatible routes, WebSocket endpoints, Management API, and TUI.
3. Core processing: access authentication, middleware, model registry and resolution, account-pool selection, round-robin scheduling, retries, cooldowns, and fallback behavior.
4. Translation and execution: protocol Translator, canonical Thinking pipeline with `ApplyThinking()`, provider executors, and streaming, non-streaming, or WebSocket transport.
5. Upstream providers: OpenAI/Codex, Anthropic/Claude, Google Gemini/Vertex, xAI/Grok, OpenAI-compatible providers, and plugin providers.
6. Supporting control plane: configuration and credential hot reload, file/Postgres/Git/object storage, caches, usage accounting, logging, model updater, and plugin runtime.

Solid arrows show the primary request and response path. Dashed arrows show configuration, credential, plugin, registry, and observability control flows.

## Visual Design

- Canvas: landscape 16:9, suitable for a presentation slide.
- Background: dark navy-to-black technical backdrop with restrained grid accents.
- Color coding: cyan/blue for access, violet for core processing, orange for execution, green for upstream providers, and slate for control-plane services.
- Components: clean rounded cards with short Chinese labels and selected English implementation names.
- Title: `CLIProxyAPI 系统架构`.
- Subtitle: `多协议兼容 · OAuth 多账户调度 · Provider 转换与执行`.
- Typography: highly legible sans-serif, optimized for projection.
- Density: detailed but scan-friendly; no file-level lists, decorative mascots, logos, watermarks, or irrelevant infrastructure.

## Accuracy Constraints

- Preserve the canonical Thinking architecture: suffix/body parsing and normalization feed a canonical `ThinkingConfig`, followed by provider-specific application.
- Show protocol translation before provider execution and response translation on the return path.
- Keep executors responsible for upstream execution; do not place general helper services inside the executor layer.
- Represent account selection and round-robin scheduling as part of authentication/runtime management.
- Distinguish runtime request flow from hot-reload, storage, registry, plugins, and usage/logging control flows.
- Show streaming, non-streaming, and supported WebSocket behavior.

## Deliverable

Generate one polished PNG in the repository under `docs/assets/`, using a non-destructive filename. Inspect the result for content hierarchy, text accuracy, and presentation readability before delivery.
