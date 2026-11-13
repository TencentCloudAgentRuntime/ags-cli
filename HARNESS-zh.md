# 托管 Harness（preview）

[English](HARNESS.md)

这些命令需要 **preview 构建**。在已审查的源码中运行
`go build -tags preview -o agr ./cmd/agr`，再将下文 `agr` 换成 `./agr`。
凭证、地域和数据面域名配置见主 README。

ModelProvider 注册模型和凭证来源。Harness 包含可编辑草稿和不可变修订，发布后
对应平台管理的 Tool、Deployment。使用前准备目标地域可接受的角色、VPC 子网、
安全组和具有模型访问权限的 TokenHub API Key；将示例变量替换为实际资源。

## 创建与对话

~~~bash
agr harness engine list -o text
agr model-provider create --name demo-models \
  --tokenhub-api-key-id "$TOKENHUB_API_KEY_ID" --model hy3 -o text
~~~

将返回 ID 保存为 `MODEL_PROVIDER_ID`。引擎版本从实际列表中选择，以下以
`pi` / `1.3.0` 为例：

~~~bash
agr harness create --name demo-agent --engine pi --engine-version 1.3.0 \
  --model "$MODEL_PROVIDER_ID:hy3" --role-arn "$ROLE_ARN" \
  --subnet-id "$SUBNET_ID" --security-group-id "$SECURITY_GROUP_ID" --wait -o text
~~~

将返回 ID 保存为 `HARNESS_ID`。创建会保存草稿、创建修订 1 并开始发布。
失败后保留 ID，检查原因，修复后重试同一修订：

~~~bash
agr harness status "$HARNESS_ID" -o text
agr harness revision get "$HARNESS_ID" 1 -o text
agr harness revision publish "$HARNESS_ID" 1 --wait -o text
~~~

`PUBLISHED` 表示平台资源已激活。端点探测可启动实例。`prompt` 默认使用
`/workspace`；可通过 `--cwd` 指定实例内已存在的其他目录，续聊时保持相同目录。
`harness acp` 原样转发编辑器发送的 `cwd`，该路径也必须在远端实例内存在。

~~~bash
agr harness endpoint "$HARNESS_ID" --check -o text
agr harness prompt "$HARNESS_ID" 'Reply only HELLO' -o json
~~~

将 `Data.SessionId`、`Data.AffinityId` 保存为 `SESSION_ID`、`AFFINITY_ID`，
继续同一实例上的同一会话：

~~~bash
agr harness prompt "$HARNESS_ID" 'What did I ask you to reply with?' \
  --session-id "$SESSION_ID" --affinity-id "$AFFINITY_ID" -o json
agr harness prompt "$HARNESS_ID" - -o ndjson < task.md
~~~

`--timeout` 覆盖端点/token 准备、ACP 初始化、建会话及本轮对话。
Ctrl+C 或 `--timeout` 会请求取消，远端轮次仍可能完成；未完成请求不会自动重发。
JSON/NDJSON 失败结果保留已取得的会话和亲和标识。
`harness proxy --port 0 -o ndjson` 在第一条 `ready` 事件返回
本机 ACP 地址，并运行到被中断；不支持单信封 `-o json`。
编辑器可以使用 `harness acp` 的 stdio ACP 接口。

## 打开 DSH Web 控制台

对于具有 **PUBLISHED DSH 修订**、且 Deployment 为 **ACTIVE** 的 Harness：

~~~bash
agr harness web "$HARNESS_ID" -o text
~~~

CLI 使用当前地域和 `--domain` 配置，打开 Deployment 的 **3080** 服务端口对应
的 HTTPS Webview 根路径。现有 ACP 入口仍使用 8080 的 `/acp`。引擎判断以
已发布修订为准，草稿中的引擎变更不会影响这个判断。

浏览器在运行 CLI 的机器上启动。命令请求启动浏览器后即退出，`Opened` 不代表
页面已就绪。浏览器访问可能按 Deployment 现有策略启动实例，无需本地代理或
持续运行 CLI。

~~~bash
# 只返回安全地址：不打开浏览器，也不获取 token。
agr harness web "$HARNESS_ID" --no-open -o json
# SSH/无浏览器环境：显式获取登录链接，复制到浏览器打开。
agr harness web "$HARNESS_ID" --no-open --show-token -o json \
  --jq '.Data | {AccessURL, ExpiresAt}'
# 进入控制台时指定亲和标识。
agr harness web "$HARNESS_ID" --affinity-id "$AFFINITY_ID" -o text
~~~

`WebURL` 始终不包含 token。只有 `--show-token` 才会在文本/JSON 中输出带凭证的
`AccessURL` 和 `ExpiresAt`，请妥善保护链接。token 或浏览器认证过期后重新运行
命令即可。`--affinity-id` 设置首次请求的 `affinity_id` 查询参数，后续请求通过
网关 Cookie 保持亲和；同一域名的标签页共享 Cookie，不提供实例隔离。
命令不会发布修订或修改 Deployment 配置。

## 接入真实 MCP

先检查市场中可用的固定版本及其所需请求头、凭证：

~~~bash
agr harness tool list --type mcp --all -o text
agr harness tool get "$RECORD_ID/$VERSION_ID" -o text
agr harness update "$HARNESS_ID" --mcp "docs=MARKETPLACE/$RECORD_ID/$VERSION_ID" -o text
agr harness revision create "$HARNESS_ID" --publish --wait -o text
~~~

MCP 必须支持 Streamable HTTP。需要鉴权时，使用 `tool get` 提示的凭证、
请求头参数；不要把密钥写入日志或版本库。`--mcp-servers @file` 可读取已有受保护
JSON 配置，但不能与快捷参数混用。

发布后，在新实例上验证真实的只读调用。仅有模型回答不能证明 MCP 已执行，应检查
流中的 `tool_call`、`tool_call_update` 和完成后的工具结果。
`harness prompt` 会拒绝工具权限请求，需要用户授权的工具应使用交互式 ACP 客户端。

## 发布与回滚

~~~bash
agr harness update "$HARNESS_ID" --system-prompt @prompt.md -o text
agr harness revision diff "$HARNESS_ID" live draft -o text
agr harness revision create "$HARNESS_ID" --publish --wait -o text
agr harness revision publish "$HARNESS_ID" 1 --wait -o text
~~~

更新只修改草稿；创建修订冻结草稿并固定 ModelProvider 修订。重新发布历史修订
就是回滚；重复发布已处于 `PUBLISHED` 的修订会直接成功，不重复发起发布。

**发布影响后续创建的新实例，已有实例和会话保留启动配置。新会话或新的 ACP
连接仍可能复用旧实例。** 验证新版本时，应结合 Deployment 的扩缩容和亲和配置
确保分配新实例。可选 E2E 使用独占的不同实例，同时验证旧快照不变。

## 限制与安全

- 每个 MCP 服务支持一个托管凭据，该服务所有 `${credential}` 模板使用同一凭据；
  其他敏感请求头值以明文保存在 Harness 配置及其修订中。创建/更新时，对名称包含
  `Authorization`、`Key`、`Token` 或 `Secret`（不区分大小写）且不含 `${credential}`
  的非空明文请求头值给出告警。
  `revision diff` 和 `status` 隐藏所有 MCP 请求头值，包括凭据模板；请求和存储值
  保持原样。其他配置读取/预览输出可能包含明文。
- MCP 名称使用 1-32 个 ASCII 字母、数字、`_` 或 `-`。Pi 将 `-` 和 `_` 视为相同，
  例如 `my-tool` 与 `my_tool` 会冲突。
- 同一 Harness 的会话在 SessionSpace 中共享该 Harness ID 作为 `UserId`，
  Harness 内没有按终端用户隔离。同一会话应只有一个写入者，串行发送 turn。
- Harness 和 ModelProvider 配置保存 TokenHub 密钥的引用。发布时，解析出的密钥会
  作为字面值写入底层 Sandbox Tool 的 `NetworkPolicy` 请求头注入规则；具备该 Tool
  查询权限的身份可以读到密钥，应据此设置 Tool 的读取权限。

## 清理本次资源

删除 Harness 前先记录关联的 `TOOL_ID`：

~~~bash
agr harness get "$HARNESS_ID" -o json
agr harness delete "$HARNESS_ID" --yes --wait -o text
# 仅删除属于自己且已不再需要的资源。
agr tool delete "$TOOL_ID" --yes --wait -o text
agr model-provider delete "$MODEL_PROVIDER_ID" --yes -o text
~~~

删除 Harness 会删除 Deployment 和 Harness 修订，但保留 Tool、ModelProvider、
SessionSpace、Registry 资产和凭证。独立创建的测试实例应另外查询并停止；
不要为了恢复测试数量而删除既有资源。

真实运行时、MCP 验证及精确资源清理见
[E2E 使用说明](internal/patchscenarios/HARNESS-E2E.md)。
