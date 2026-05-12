# AGS CLI DX / AX 综合改造文档

日期：2026-05-12

来源：

- 本地 DX 审计：[2026-05-12-ags-cli-dx-issues.md](./2026-05-12-ags-cli-dx-issues.md)
- 产品原则与改造方案：`/tmp/ags-cli-ax-principles-and-plan.md`

## 一句话结论

`ags-cli` 现在已经具备较完整的功能面，但它还不是一个稳定的“可执行协议”。人类可以靠阅读、重试和上下文理解绕过问题；Agent、CI 和脚本则会被错误的退出码、混杂的输出、隐式副作用、不一致的后端路径和不可预测的错误文本带偏。

下一阶段的重点不是继续增加命令，而是先把 CLI 契约做实：非交互、可解析、可组合、可恢复、输出有界、错误可行动、资源行为一致。完成后，CLI 才能作为 MCP、Agent skill、SDK 和文档的权威语义源。

## 背景

`ags-cli` 的调用方正在变化。除了人类开发者，Claude Code、Codex、Gemini CLI、CI Agent 等系统也会通过 shell 调用它。这些调用方不会像人一样读长 help、理解彩色表格、处理 prompt 或手工修复半结构化输出；它们把 CLI 当作协议使用。

因此 AGS CLI 需要同时服务两类体验：

| 维度 | Human DX | Agent DX / AX |
| --- | --- | --- |
| 主要目标 | 好读、好发现、好输入、好理解 | 可预测、可解析、可组合、可恢复、可限制、可验证 |
| 输出偏好 | 表格、颜色、提示语、别名 | 稳定 JSON、字段一致、stdout 纯数据 |
| 失败恢复 | 人读错误后改命令 | 依赖 exit code、error code、hint 和 retryable |
| 交互方式 | prompt、REPL、浏览器、editor 可接受 | 默认非交互，无 TTY 可运行 |
| 文档依赖 | README/help 可辅助 | CLI 自描述应是事实来源 |

两类体验不冲突，但必须分层：人类模式可以保留便利和展示，Agent 模式必须提供稳定契约。

## 已验证状态

本地审计覆盖了构建、静态检查、测试、帮助、文档、输出格式、错误路径、权限和配置行为。

| 项目 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `make test` | 通过 |
| `go vet ./...` / `make lint` | 通过 |
| `golangci-lint run ./...` | 通过，0 issues |
| `gitleaks detect --no-git --source . --redact` | 通过，无泄漏 |
| `go build -o /tmp/ags-dx-audit .` | 通过 |
| `ags docs markdown` | 通过，生成 44 个文件 |
| `ags docs man` | 通过，生成 44 个 man pages |
| shell completion | bash / zsh / fish / powershell 均可生成 |

这说明代码质量底座不差，主要风险集中在 CLI 契约、运行时一致性和文档真实性。

## 当前问题图谱

下面把本地审计的 15 个问题合并到产品原则里的问题维度。

| 问题维度 | 本地证据 | 影响 |
| --- | --- | --- |
| 非 TTY 崩溃 | DX-001：`ags </dev/null` 进入 REPL 后 panic | CI/Agent 无法安全探测 CLI |
| 后端与资源行为不一致 | DX-002：E2B key 校验通过，但 `run/exec/file` 临时实例走云端凭证 | 默认 quick start 和真实行为断裂 |
| 错误和 usage 混杂 | DX-003：运行时错误后刷完整 usage，JSON 模式也输出纯文本 | Agent 难解析，人类难定位 |
| 配置校验不统一 | DX-004：`--backend bad` 被当作 E2B，`--output yaml` 不被及时拒绝 | 用户调错方向 |
| 帮助依赖外部工具 | DX-005：`mobile adb --help` 在缺少 adb 时失败 | 缺依赖时看不到修复说明 |
| 本地参数晚校验 | DX-006：无效 repeat/language/env/timeout 先触发网络或创建 | 反馈慢且误导 |
| readiness 日志不可信 | DX-007：负端口仍先打印 tunnel listening | Agent 误判服务可用 |
| destructive / broad 操作不安全 | DX-008、DX-015：`--all` 和 ID 可混用；删除无 dry-run/确认说明 | 误删风险和 blast radius 不透明 |
| 配置文件错误处理弱 | DX-009：坏 TOML 只 warning，继续用默认值 | 根因被后续凭证错误掩盖 |
| 凭证权限防护不足 | DX-010：含 key 的 config 0644 无警告 | 凭证可能暴露 |
| 文档与默认路径冲突 | DX-011、DX-014：默认 E2B 下推荐 cloud-only 命令；`--instance` required 文案不准 | 新用户第一屏失败 |
| 文档生成策略不一致 | DX-012：completion 文档生成，hidden docs 命令不生成 | public/dev 文档边界不清 |
| provider 错误直出 | DX-013：Cloud SDK / E2B REST 原文透传 | Agent 被内部细节带偏 |

产品原则文档中还提到但本轮本地审计未完全复测的风险，也应进入改造基线：

- 远端执行失败时本地 exit code 可能仍为 0。
- text 模式和 JSON 模式 exit code 可能不一致。
- JSON 顶层结构、字段大小写和同概念字段类型不统一。
- `--limit` / pagination 可能不真正限制输出。
- E2E 脚本可能存在假阳性。

这些项需要在 P0 阶段补充可复现测试。

## 产品定位

### CLI 是高保真核心

AGS 能力应该先在 CLI 中定义清楚，再派生 MCP tools、Agent skill、`AGENTS.md` 和 SDK 绑定。原因：

- CLI 只在调用时消耗上下文，适合 Agent 按需发现能力。
- CLI 比 MCP 更能表达复杂嵌套参数和 raw payload。
- Agent 对 Unix 命令、管道、stdout/stderr、`jq`、`xargs` 有强训练先验。
- CLI 是人类和 Agent 都能共同调试的最小公共接口。

### 三层目标架构

| 层级 | 目标 | 主要能力 |
| --- | --- | --- |
| 人类 CLI | 保留当前便利体验 | 表格、颜色、短别名、REPL、prompt、浏览器打开、便利 flags |
| Agent CLI | 提供稳定机器契约 | `--agent` / `--machine`、非交互、统一 JSON、结构化错误、稳定 exit code、dry-run、schema/status |
| 协议表面 | 从 CLI schema 派生 | MCP tools、Agent skill、`AGENTS.md`、SDK / Code Mode 绑定 |

核心原则：只维护一套语义。命令 schema 是权威数据源，文档、help、MCP 和 skill 都从它派生或被它校验。

## 设计原则

### 1. 默认非交互

Agent-facing 路径必须在无 TTY 下运行。非 TTY 时不进入 REPL，不打开 editor，不自动打开浏览器，不等待确认。

建议：

- 新增 `--non-interactive`
- 支持 `AGS_NON_INTERACTIVE=1`
- 非 TTY 无子命令时显示 help 或返回结构化 usage error，不 panic
- `--agent` 模式隐含 non-interactive

### 2. stdout 是数据，stderr 是诊断

stdout 只放下游消费的数据；stderr 放 progress、warning、debug、timing、error、usage。JSON 模式下 stdout 必须始终是合法 JSON，包括失败路径。

这会影响：

- `run` 的远端 stdout/stderr 映射
- `exec` 的命令输出映射
- `file upload/download/remove` 的成功提示
- timing、warning、provider request id 的输出位置

### 3. JSON 使用统一 envelope

所有命令在 Agent / JSON 模式下都返回统一 envelope：

```json
{
  "schema_version": "ags.v1",
  "command": "ags instance list",
  "status": "succeeded",
  "data": {},
  "error": null,
  "warnings": [],
  "meta": {
    "backend": "e2b",
    "request_id": null,
    "duration_ms": 123
  }
}
```

list 类命令统一：

```json
{
  "items": [],
  "pagination": {
    "limit": 20,
    "offset": 0,
    "next_cursor": null
  }
}
```

要求：

- 空列表是 success，不是特殊文本。
- 字段名统一小写 snake_case 或 lowerCamelCase，不能混用 `id` 和 `ID`。
- 同概念字段类型统一，例如 `stdout` 不应有时是数组、有时是字符串。
- `-o json` 是全局契约；不支持时必须报结构化错误，不能静默忽略。

### 4. 错误指向下一步

错误要同时服务程序和人：

```json
{
  "code": "AUTH_MISSING",
  "kind": "configuration",
  "message": "missing E2B API key",
  "hint": "Set AGS_E2B_API_KEY or pass --e2b-api-key.",
  "retryable": false,
  "details": {}
}
```

错误分类至少覆盖：

- usage
- configuration
- auth
- permission
- not_found
- conflict
- network
- timeout
- rate_limit
- backend_unsupported
- remote_execution_failed
- partial_success

底层 SDK、内部域名、签名细节可以放在 debug/details 中受控暴露，但默认 message 必须是 AGS CLI 语义。

### 5. exit code 是控制流

exit code 必须和 JSON error kind 一致，且 text / JSON 模式一致。

| 退出码 | 含义 |
| --- | --- |
| `0` | success |
| `2` | usage error |
| `3` | not_found |
| `4` | auth / permission |
| `5` | conflict / already_exists |
| `6` | rate_limit |
| `7` | timeout |
| `8` | network |
| `9` | backend_unsupported |
| `10` | partial_success |
| `11` | remote_execution_failed |

远端代码抛异常、远端命令非零退出、批量删除部分失败，都不能返回 success。

### 6. CLI 能描述自己

新增运行时自描述入口：

```bash
ags capabilities -o json
ags schema
ags schema instance.create
ags status -o json
ags doctor
```

schema 应包含：

- 参数名、类型、必填、默认值、enum
- backend 支持情况
- 是否 mutation
- 是否幂等
- 是否可能创建资源
- 输出 schema
- 可能错误码
- examples，并带 CI 验证标记

### 7. 环境可内省

Agent 遇到认证问题时，需要知道当前 CLI 识别到了什么。

`ags status -o json` 应返回：

- 当前 backend
- 当前 region/domain/internal
- 已识别的凭证类型，不返回 secret 原文
- 配置来源：flag/env/config/default
- 各命令组在当前后端是否可用
- config 文件路径和权限状态

`ags doctor` 应检测：

- config 是否可解析
- 凭证是否缺失或权限过宽
- deprecated config 是否存在
- backend 与命令能力是否匹配
- 外部依赖，例如 adb
- 网络/鉴权基本探测

### 8. 描述与实现同步

README、help、man、markdown docs、schema、examples 必须同步。

要求：

- 第一屏示例必须在默认配置下成立，或明确标注需要 cloud backend。
- high-frequency examples 进入 CI smoke test。
- `file --help` 这类参数 required 文案必须反映真实行为。
- hidden/dev 命令的文档生成策略明确化。

### 9. 输出有界

list / get 类命令默认有界，limit 必须真实生效。

建议：

```bash
ags instance list --fields id,status,tool --limit 20 -o json
ags instance list --page-all --output ndjson
```

大输出应支持：

- `--fields`
- cursor / offset
- `--page-all`
- NDJSON
- 摘要 + artifact ID

### 10. Agent 输入不可信

对 Agent 生成的参数按外部输入处理：

- path traversal
- 控制字符
- ID 中的 `?`、`#`、`%`
- double encoding
- domain / region 传完整 URL
- tool name 和 tool ID 混用
- raw payload schema validation

返回给 Agent 的外部数据也要考虑 prompt injection 和控制字符消毒。

### 11. 资源生命周期一致

需要实例的命令统一走同一套实例获取/创建逻辑：

- `instance create`
- `run`
- `exec`
- `file`
- `browser`
- `mobile`
- `proxy`

原则：

- 同一 backend 下行为一致。
- 隐式创建资源需要显式授权，例如 `--allow-create`。
- 支持 `--instance` 复用已有实例。
- 支持幂等语义：already_absent / already_exists / already_running 可被正常表达。
- 支持 `--idempotency-key` 或 ensure/apply 风格命令。

### 12. mutation 可预演

所有 mutation 支持：

- `--dry-run`
- `--idempotency-key`
- 返回 `effects`

示例 effects：

```json
{
  "effects": [
    {
      "action": "create",
      "resource_type": "sandbox_instance",
      "resource_id": null,
      "estimated": true
    }
  ]
}
```

### 13. 复杂输入支持 raw payload

保留人类 flags，同时为 Agent 提供 JSON payload：

```bash
ags instance create --tool-name code-interpreter-v1 --timeout 300
ags instance create --params '{"tool_name":"code-interpreter-v1","timeout":300}'
```

raw payload 应做 schema validation，并纳入 dry-run 和 error model。

### 14. 支持管道和精简输出

建议：

- `-` 表示 stdin/stdout
- `--id-only` / `--ids`
- `--quiet`
- `--porcelain`
- `--` 分隔命令参数

目标是支持这样的可组合路径：

```bash
id=$(ags instance create -t code-interpreter-v1 --id-only)
ags exec --instance "$id" -- python --version
ags instance delete "$id"
```

### 15. 用真实 Agent 场景测试

测试不只验证命令返回，还要验证 Agent 是否能完成任务。

指标：

- 任务成功率
- 交互轮次
- 是否误解析 stdout
- 是否遗漏资源清理
- 是否产生无效重试
- 是否因长输出撑爆上下文
- 是否把底层 provider 错误当事实

## 分阶段路线图

### P0：修复契约破口

目标：先让 CLI 不误导 Agent 和脚本。

| 改动 | 对应问题 | 验收 |
| --- | --- | --- |
| 非 TTY 下无子命令不进入 REPL | DX-001 | `ags </dev/null` 不 panic，exit code 非 2 panic，输出 help 或结构化 error |
| 运行时错误不刷完整 usage | DX-003 | auth/network/provider 错误只输出 error + hint；usage 错误才输出 usage |
| JSON 模式失败路径也是 JSON | DX-003 | `ags -o json instance list` 缺凭证时 stdout 可被 `jq` 解析 |
| 全局 config 统一校验 | DX-004 | `--backend bad`、`--output yaml` 在所有命令前失败 |
| 本地参数前置校验 | DX-006、DX-007、DX-008 | invalid repeat/language/env/timeout/port/flag combo 不触发网络 |
| config 解析失败变 fatal | DX-009 | 坏 TOML 不继续 fallback 到默认配置 |
| exit code 和远端失败对齐 | 产品原则 | 远端代码异常、远端命令非零退出均返回非零 |
| list limit/depth 真实生效 | 产品原则 | `--limit 3` 实际最多返回 3 条 |

建议优先 PR 拆分：

1. root/non-TTY + SilenceUsage/Error 基础设施
2. config validation + malformed config fatal
3. local flag validation sweep
4. JSON error formatter + exit code mapper
5. list boundary tests

### P1：统一资源行为和后端模型

目标：让需要实例的命令共享一套心智模型。

| 改动 | 对应问题 | 验收 |
| --- | --- | --- |
| `run/exec/file` 复用统一实例 provider | DX-002 | E2B backend 下不再误走云端 credential path |
| 明确临时实例创建策略 | DX-002、隐式副作用 | 默认不隐式创建或需要 `--allow-create`；行为写入 schema |
| cloud-only 命令前置判断 | DX-011 | E2B 下 `tool/apikey` 返回 backend_unsupported + hint |
| provider 错误包装 | DX-013 | message 给出 backend/credential/action hint，request id 保留在 meta/details |
| help/README quick start 修正 | DX-011、DX-014 | 默认路径复制可跑；cloud 示例显式带 cloud backend |
| 删除类命令说明 destructive 语义 | DX-015 | help 写明是否立即执行、是否支持 dry-run/force、部分失败行为 |

### P2：建立 Agent 模式

目标：提供明确的机器入口，而不是让 Agent 模仿人类 CLI。

范围：

- 全局 `--agent` 或 `--machine`
- `AGS_AGENT=1`
- `--non-interactive` / `AGS_NON_INTERACTIVE=1`
- Agent 模式默认 JSON envelope
- 禁用 ANSI、spinner、prompt、editor、browser auto-open
- 所有诊断走 stderr
- 所有错误含 code/kind/hint/retryable

验收：

```bash
env AGS_AGENT=1 ags status | jq .
env AGS_AGENT=1 ags run -c 'raise Exception("boom")'
```

第二条应返回稳定 JSON error，exit code 为 `11`。

### P3：统一 JSON、错误和 exit code

目标：一次接入，所有命令通用。

范围：

- `schema_version`
- `status`
- `data`
- `error`
- `warnings`
- `meta`
- 统一字段命名和类型
- 空列表 success
- partial success model
- exit code 表落地

验收：

- 所有支持 `-o json` 的命令失败路径都能被 `jq` 解析。
- 同一个错误在 text / JSON 模式 exit code 一致。
- 批量操作返回单个 envelope，而不是多个独立 JSON 对象。

### P4：schema、capabilities、status、doctor

目标：CLI 自身成为 Agent 的文档源和配置排查工具。

新增：

```bash
ags capabilities -o json
ags schema
ags schema instance.create
ags status -o json
ags doctor
```

验收：

- Agent 不读 README，只通过 `schema/status` 能判断当前环境可执行哪些命令。
- schema 能驱动 docs 和 MCP/skill 生成。
- `doctor` 能发现坏 TOML、缺凭证、config 权限过宽、缺 adb。

### P5：上下文和组合优化

目标：减少 token 消耗和命令拼接错误。

范围：

- `--fields`
- cursor pagination
- `--page-all --output ndjson`
- `--id-only` / `--ids` / `--quiet` / `--porcelain`
- 文件命令支持 `-`
- shell 命令支持 `--`
- `--params` raw JSON payload

验收：

- `ags instance list --fields id,status --limit 5 -o json` 输出小而稳定。
- `ags file upload - /tmp/input.txt` 支持 stdin。
- Agent 可以用 `--id-only` 完成 create -> exec -> delete 工作流。

### P6：安全和副作用控制

目标：让 Agent 能安全执行 mutation。

范围：

- `--dry-run`
- `--idempotency-key`
- `effects`
- 输入硬化
- 输出消毒
- config credential 权限检查

验收：

- 所有 create/delete/update/apikey/proxy/mobile connect 类 mutation 都支持 dry-run 或明确说明不可 dry-run。
- 含凭证 config 权限过宽会被 `doctor` 检出，必要时命令直接 warning/fail。
- Agent 输入中的路径穿越、控制字符、非法 ID 被本地拒绝。

### P7：Agent 适配包和场景测试

目标：持续验证 Agent 能真实完成任务。

产物：

- 修复现有 e2e 脚本，跳过关键步骤不能报绿
- `docs/agent-contract.md`
- `AGENTS.md`
- `skills/ags/SKILL.md`
- Agent scenario tests
- agentprobe 风格评估

首批场景：

1. 发现环境状态，判断当前 backend 能做什么。
2. 创建实例、运行代码、验证输出、清理实例。
3. 连接已有实例执行命令并上传/下载文件。
4. 遇到缺凭证时根据 hint 修复或停止。
5. 批量 list/get/delete 时遵守 limit、dry-run 和 partial success。

## 问题到阶段映射

| Issue | 阶段 | 说明 |
| --- | --- | --- |
| DX-001 非 TTY panic | P0 | 契约底线 |
| DX-002 E2B/backend 不一致 | P1 | 资源行为统一 |
| DX-003 错误刷 usage / JSON 错误非 JSON | P0 / P3 | 先止血，再统一 envelope |
| DX-004 全局配置绕过 | P0 | 所有命令前置校验 |
| DX-005 `mobile adb --help` 依赖 adb | P0 | help 必须无依赖 |
| DX-006 本地参数晚校验 | P0 | 避免无效网络调用 |
| DX-007 tunnel 负端口仍监听 | P0 | 参数先验 + readiness 语义 |
| DX-008 `--all` 忽略 ID | P0 / P6 | flag 互斥 + destructive safety |
| DX-009 坏 config 只 warning | P0 | fatal config error |
| DX-010 config 权限无检查 | P6 | doctor + 安全门禁 |
| DX-011 quick start 默认失败 | P1 | docs 与实现同步 |
| DX-012 docs 生成策略不一致 | P4 / P7 | schema 驱动 docs |
| DX-013 provider 错误直出 | P1 / P3 | AGS error model |
| DX-014 `--instance` required 文案不准 | P1 | help 修正 |
| DX-015 destructive 无 dry-run/确认说明 | P6 | mutation 可预演 |

## 近期可执行任务清单

### 第一周：契约止血

- 给 root command 增加非 TTY 保护。
- 全局设置/调整 `SilenceUsage` 策略，区分 usage error 和 runtime error。
- 增加统一错误类型：`kind/code/hint/retryable/exit_code`。
- 所有命令入口前置 `config.Validate()` 或更细粒度 config validation。
- 坏 config parse 变 fatal。
- 修复 `mobile adb --help`。
- 前置校验 repeat/max-parallel/language/env/timeout/port/flag mutual exclusion。

### 第二周：后端和资源一致性

- 抽象 `InstanceProvider`：existing instance、create temporary、token cache、cleanup 一处实现。
- `run/exec/file/browser/mobile/proxy` 统一实例获取路径。
- 修复 E2B backend 下 `run/exec/file` 误走 cloud credentials。
- 为 cloud-only 命令增加 backend_unsupported 结构化错误。
- 修正 README、root help、file help、quick start。

### 第三周：Agent 模式最小可用

- 增加 `--agent` / `AGS_AGENT=1`。
- Agent 模式默认 JSON envelope。
- stdout/stderr 分离第一轮落地。
- `ags status -o json` 最小版本。
- `ags doctor` 最小版本。

### 第四周：测试和文档闭环

- 为每个 P0/P1 bug 加 CLI-level regression test。
- 修复现有 e2e 假阳性。
- 建立 smoke examples：README/help 中高频命令必须可验证。
- 初版 `docs/agent-contract.md` 和 `AGENTS.md`。

## 验收标准

### Agent 契约验收

- 无 TTY 下所有 Agent-facing 命令不 panic。
- 所有失败路径有稳定 exit code。
- JSON 模式 stdout 永远是合法 JSON。
- 运行时错误不输出整页 usage。
- 缺配置/缺权限/后端不支持都有 actionable hint。
- 同一操作 text / JSON 模式 exit code 一致。
- `--limit`、`--fields` 等边界控制真实生效。
- mutation 能 dry-run 或明确声明不可 dry-run。

### 文档验收

- README quick start 在默认配置下可执行，或明确说明 backend/credential 前提。
- root help 第一屏示例不会把默认用户带到失败路径。
- 每个命令 help 的 required/default/alias/backend support 与 schema 一致。
- docs/man/markdown 由同一 command schema 或同一验证机制约束。

### 安全验收

- config 中含 credential 且权限过宽时 warning 或 fail。
- token/cache/tunnel/config 文件权限策略一致。
- Agent 输入中的危险路径、非法 ID、控制字符在本地被拒绝。
- provider 错误不默认泄漏内部地址和底层签名细节。

## 产品承诺

改造完成后，AGS CLI 应能对 Agent 调用方承诺：

1. Agent 模式下所有命令非交互，无 TTY 可运行。
2. stdout 永远是结果，stderr 永远是诊断。
3. JSON 输出有版本号、有 schema、有稳定 envelope，包括失败路径。
4. 失败一定非零退出，附带结构化错误、可读消息和修复建议。
5. 同类命令行为一致，资源生命周期统一。
6. list / get 输出默认有界，支持 fields 和分页，且参数真正生效。
7. mutation 支持 dry-run 和幂等，副作用显式返回。
8. CLI 能在运行时描述自己的能力和当前环境状态。
9. 命令可通过管道组合，无需正则解析人类文本。
10. CLI、MCP、skill、docs 派生自同一套 command schema。
11. 可用性基于真实 Agent 场景持续验证。

