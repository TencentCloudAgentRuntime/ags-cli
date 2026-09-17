# Session OpenAPI 与后端实际行为差异记录

初次记录：2026-09-16；最近更新：2026-09-17。本文记录本轮 Session preview 接入中遇到的差异、处理决定和验证结果，不代表对整个 OpenAPI 的完整审计。

当前结论：CLI 已按已验证的后端行为收敛；调整后的完整 Session 生命周期真实 E2E 通过。OpenAPI 上游修订、API 负责人确认及线上部署版本核对尚未完成。

## 最新结论：上海正式环境复核与当前范围

2026-09-17 上海正式环境（`ap-shanghai`，API 版本 `2025-09-20`）验证：现有 `session.lifecycle` 的 58 次调用、20 个断言组通过；但随后独立计数复核发现后端 bug，因此不能笼统表述“全部 E2E 通过”。

| 项目 | 上海实际结果 | 当前决定 |
| --- | --- | --- |
| 空间四种筛选、Session 标题筛选及 Metadata 组合 | 正反例与分页通过 | 保留当前 CLI |
| InlineData | append/list 回读一致 | 保留 |
| Timestamp | 客户端传入时间未保留，服务端生成时间 | 仅保留响应字段 |
| DescribeSession 的 NumRecentEvents / AfterTimestamp | 请求成功但不返回 Events | 按已确认范围继续移除；事件查询使用 session event list |
| ModifySessionTitle | ActionOffline，已被 ModifySession 替代 | 不提供独立标题命令 |
| UpdateEvent | InvalidAction | 暂不纳入本次 CLI |
| DescribeEvents.SkipTotal | 显式 true/false 都返回 UnknownParameter | 暂不纳入本次 CLI |
| EventCount | get 为 0，list 与实际事件数一致 | 已知后端 bug；正确契约要求一致，CLI 不额外查询或覆写结果 |

计数复核使用同一新建会话，核对 SpaceId/UserId/SessionId 后，观测如下：

| 阶段 | DescribeSession.EventCount | DescribeSessions.EventCount | DescribeEvents.TotalCount |
| --- | ---: | ---: | ---: |
| 空会话 | 0 | 0 | 0 |
| 追加 1 条事件 | 0 | 1 | 1 |
| 追加 2 条事件 | 0 | 2 | 2 |
| 等待 10 秒后 | 0 | 2 | 2 |

省略 NumRecentEvents 或分别传 0、1、10，单条查询结果均相同。该结果与单条摘要未填充 EventCount、列表单独统计的源码路径吻合。以上用于界定后端问题，不改变 CLI 的目标契约。

### 测试与报告口径

新增独立 `session.event-count` 场景，断言 `event-count.consistent`：从空会话到追加 1/2 条事件，get/list/EventCount 必须等于已写入事件数量，并与事件列表的 TotalCount 和实际条数一致。EventCount 的覆盖项绑定此专项场景，不再借用普通 session.get 的成功断言。

本地故障用例分别模拟“只有 get 错报 0”和“get/list 同时错报 0”，两者都必须使专项场景失败，并验证失败后资源清理。不会把已知后端 bug 标为 skip、预期通过或成功豁免。

报告应分别展示：

- 生命周期场景：本次上海实测通过。
- 计数契约：独立真实复核失败，归因为已知后端 bug。
- 完整验收：不能据生命周期通过宣称所有契约通过；覆盖计划包含计数专项后，实际失败应使总报告失败。

新增注册场景的本地测试结果与此前原始云 API 复核是两类证据；此前通过报告不包含新增专项，不能复用为新覆盖计划的通过证明。后端修复后重新运行专项及完整 E2E。

本轮原始 API 探测与计数复核创建的测试资源均已删除，并以 get 返回 not_found 确认。完整原始证据保存在本地，尚未作为公开 PR 附件发布。

以下重庆与初次发现章节为历史记录；当前功能范围和报告结论以上述上海复核及明确决定为准。

## 最新状态：2026-09-17 重庆正式环境验证

以更新后的后端契约恢复空间列表 `Filters`，并补充会话 `title`、`title-like` 与 Metadata 的组合查询验证。当前结论：

| 项目 | 最新状态 | CLI 决定 |
| --- | --- | --- |
| S-01 空间 Filters | 新版源码已实现；重庆正式环境四种筛选正反例及过滤后分页通过，不再返回 UnknownParameter | 已恢复 API patch、mapping、help/schema 和生成命令 |
| S-02 Timestamp | 仍由服务端生成 | 保持响应字段，不恢复请求输入 |
| S-03 近期事件参数 | YunAPI 摘要仍无 Events | 保持移除，使用事件列表查询 |
| S-04 ModifySessionTitle | 正式路由仍使用 ModifySession | 保持 session update --title |
| S-05 InlineData | 新版源码补齐响应投影，与云端回读一致 | 保留，完整场景回读再次通过 |
| Session 标题筛选 | title/title-like 正反例及与 Metadata 的 AND 组合真实验证通过 | 沿用 session list --filters，新增回归断言 |

本轮于 **2026-09-17 12:03:35–12:04:08（北京时间）**在 `ap-chongqing / ags.tencentcloudapi.com` 运行完整 `session.lifecycle`：**58 次 CLI 调用、20 个断言组全部通过，cleanup=pass**。创建的 2 个空间、2 个会话均已删除，随后查询确认不存在。没有测试所有长度/数量限制和非法组合，不外推其他地域或权限。

- Patch digest：`c6a9616e9c4d48d191cd3d5cb1f79cd6c667c69a3d42b6dec89f3a6b4a78f0e5`。
- Plan digest：`31954812aea596b4b6f36be44f3ad2d2176e7167bc7f3b9254d77378c056730e`。
- 本轮机器报告、源码快照和二进制身份已在本地保存。仍为未提交快照预验证；正式 PR 的最终提交需重跑严格报告。

以下章节保留 **2026-09-16 初次发现和处理历史**。其中空间筛选尚不可用、InlineData 源码与线上不同的旧结论，已由上表更新；旧报告 hash 仅对应当日快照。

## 1. 证据范围

| 来源 | 本轮使用的版本或环境 | 适用边界 |
| --- | --- | --- |
| OpenAPI 附件 | `session-0916.json` | 附件描述，不直接等于线上已经发布的契约 |
| 后端源码 | 本轮检查的 Session 服务源码快照（版本记录保存在本地） | 本地源码快照；未证明它就是线上部署版本 |
| 真实云 API | `ags.tencentcloudapi.com`，`ap-chongqing`，2026-09-16 | 结论限于本次环境、账号权限及实际执行的请求 |
| CLI | `codex/session-preview` 的未提交工作区快照 | 预验证结果，不是绑定最终 PR commit 的严格报告 |

附件 SHA-256：`b9d0eebdc6f9e232f576967881dfd6f672f38f25a437f1b615f6b291925ab5c5`。

本文保留接口名称、可观察行为和处理决定；源码定位、原始响应和完整本地报告单独保存。正式 PR 的验收报告仍需归档并由独立 reviewer 复现。

## 2. 差异总览

| 编号 | 接口或字段 | 差异 | 当前 CLI 处理 | 证据状态 |
| --- | --- | --- | --- | --- |
| S-01 | `DescribeSessionSpaces.Filters` | 附件支持空间筛选；当前请求结构无此字段，线上明确拒绝 | 移除空间列表筛选输入，保留分页 | 源码与云端错误均确认 |
| S-02 | `AppendEvent.Event.Timestamp` | 附件请求/响应共用带时间字段的模型；后端生成写入时间 | 拆分事件输入与输出模型，时间仅在响应中保留 | 源码与云端回读均确认 |
| S-03 | `DescribeSession.NumRecentEvents / AfterTimestamp` | 附件和后端接受查询参数，但 YunAPI 响应摘要不返回事件 | 首版移除这两个输入；事件查询使用 `session event list` | 源码确认；未单独在线证明这两个参数的效果 |
| S-04 | `ModifySessionTitle` | 附件有独立 Action；当前正式 YunAPI 路由未注册 | 仅提供 `session update --title`，映射 `ModifySession` | 路由源码确认；替代命令已通过真实 E2E |
| S-05 | `Event.Content.Parts[].InlineData` | 本地响应模型缺失，但附件声明且线上可以回读 | 保留输入与输出能力、保留回读断言 | 线上确认；源码与部署版本差异待查 |

## 3. 逐项说明

### S-01：空间列表 Filters 尚不可用

**附件**：`paths./DescribeSessionSpaces.post.requestBody` 定义 `Filters`，描述支持 `space-id`、`name`、`name-like`、`description-like`，并给出筛选示例。

**源码**：`DescribeSessionSpacesRequest` 的业务参数只有 `Offset`、`Limit`；`describeSessionSpaces` 没有处理上述业务筛选条件。函数内的授权资源过滤不等于用户传入的 `Filters`。

**线上**：本轮对 `space-id` 和 `name` 分别发送筛选请求，均收到：

```text
Code: UnknownParameter
Message: 未定义参数 `Filters` 。
```

**处理**：从 preview API patch、mapping、help 和生成命令中移除 `Filters`；本地 raw request 也拒绝该字段。空间分页按实际 `TotalCount` 遍历，确认本轮资源出现，不再假设账号只有本轮创建的两个空间。

**后续**：若要支持筛选，需要核对 API 注册与实际部署的服务实现；补齐并真实验证后再恢复 CLI 能力。本轮没有修改后端，也没有修改原始附件。

### S-02：事件时间由服务端生成

**附件**：`AppendEvent.Event` 与响应 `Event` 都引用 `components.schemas.EventInfo`，其中 `Timestamp` 仅描述为“事件时间”，未明确是只读字段，容易被生成器当作可设置的请求字段。

**源码**：`EventReq` 没有 `Timestamp`；`SessionService.AppendEvent` 无条件执行：

```go
event.Timestamp = time.Now().Truncate(time.Millisecond)
```

**线上**：请求中提供 `2026-09-01T00:00:00Z`，回读得到 2026-09-16 的服务端写入时间。时间没有按客户端输入保留。用固定的 2026-09-03 做排除条件会返回事件，这不能证明 `AfterTimestamp` 失效。

**处理**：新增不含 `Timestamp` 的 `EventInput`，供 `AppendEventRequest.Event` 引用；保留响应 `EventInfo.Timestamp` 并明确服务端生成语义。E2E 不再发送时间字段，验证 append 返回的时间有效、list 回读与其一致，并以服务端返回时间构造包含/排除边界。

**后续**：建议上游 OpenAPI 拆分输入/输出模型，或提供生成器能正确处理的只读字段标记；保持现有后端时间语义。

### S-03：查询近期事件的参数没有对应的 YunAPI 输出

**附件**：`DescribeSession` 接收 `NumRecentEvents`、`AfterTimestamp`，但响应只有 `SessionInfo`，该模型没有 `Events`。

**源码**：`YunAPIDescribeSessionRequest` 接收参数，handler 也传给服务查询；然而 `yunAPISessionSummary` 最终只构造会话摘要，不输出查询到的事件。

**性质**：不是简单的“后端拒绝参数”，而是请求功能与对客响应不闭合。不能仅因请求成功就声明近期事件查询已经验证。

**处理**：按本轮确认，首版 `session get` 不暴露这两个参数，help/schema 和 raw request 同步收敛。使用 `session event list --after-timestamp ...` 查询事件，该独立查询链路已通过真实验证。

**后续**：API 负责人决定删除无对客效果的参数，或在正式响应契约和实现中增加事件输出；闭环后再恢复 CLI 输入。

### S-04：独立修改标题 Action 与路由不一致

**附件**：同时包含 `ModifySessionTitle` 和 `ModifySession`。

**源码**：内部 `Dispatch` 有 `ModifySessionTitle`，但正式 `DispatchYunAPI` 只注册 `ModifySession`。不能用内部路由存在来证明对客云 API 可调用。

**处理**：仅提供 `session update --title`，不提供 `session update-title`。通过 `ModifySession` 修改标题、随后 get 回读已通过真实 E2E。

**边界与后续**：本轮未直接调用线上的 `ModifySessionTitle`，不宣称线上已实测该 Action 不存在。建议负责人核对注册与路由，明确独立 Action 是应补齐还是从文档移除。

### S-05：InlineData 的本地源码与线上响应不同

**附件**：`EventPartInfo` 包含 `InlineData`。

**源码**：输入 `PartReq` 和 `ToModelEvent` 支持该字段，但 `EventPartSummary` 没有它，当前本地 `eventSummary` 也不赋值。

**线上**：追加包含 `MimeType`、`Data` 的事件后，事件列表能够回读一致的 `InlineData`；调整后的完整 E2E 再次通过该断言。

**处理**：保留 CLI 能力。撤回此前仅根据本地源码作出的“线上可能丢失 InlineData”判断；不能为对齐旧源码删除已实测支持的字段。

**后续**：核对线上部署 commit、接口转换层及本地分支。当前只能确认行为有差异，不能确定是哪一层造成，也不把本地 commit 标为线上版本。

## 4. 测试假设修正，不单独视为接口缺陷

- `Thought: false` 可被响应序列化省略；JSON 字符串字段可被重新序列化。比较 `FunctionCall`、`FunctionResponse`、`Metadata`、`Extensions`、`StateDelta` 时使用 JSON 语义比较，仍拒绝内容改变或必要字段丢失。
- 空间列表包含账号已有资源，`TotalCount` 不等于本次新建资源数；测试必须允许已有资源，并只清理自己创建的资源。
- 覆盖清单绑定到某个场景不等于行为已经验证。分页和筛选使用多个对象及不匹配条件，并用忽略参数的本地故障用例证明错误实现会失败。

## 5. 最新真实 E2E 结果

2026-09-16 21:02:03–21:02:34（北京时间），调整后的注册场景 `session.lifecycle` 完整运行通过，没有跳过空间列表步骤：

- CLI 调用：54 次。
- 断言组：18 个，全部通过。
- 清理：本轮创建 2 个空间、2 个会话；均删除并通过再次 get 返回 `not_found` 确认不存在，`cleanup=pass`。
- 覆盖本次场景中的空间、会话生命周期，列表/筛选/分页，事件写入与回读、时间筛选、状态增量、非法输入校验。
- 这不等于附件全部字段、所有筛选操作符、地域和账号权限都已验证；例如未单独验证附件描述的 Session 标题筛选。

| 身份 | 值 |
| --- | --- |
| CLI 工作区来源 SHA-256 | `fb61b4d6e3d0fedc39a476869e7d7b20e9359af6d662a378aacaa45b262d6dd5` |
| CLI 二进制 SHA-256 | `4101404c3d94b222a28f90cd01eb1a7c8705ce1d7d19b538dd121d22ef8182e3` |
| Patch digest | `70cc09e90c525801a7def99c0c7f871424200eb59c25dbd7d8828361436952b9` |
| Plan digest | `174cedbfd4153d0c6b240f83e62b2ec5218ef4d036c9954c6e515bcb1667ef9d` |

本文件在该次 E2E 之后新建，因此不包含在上述工作区来源 hash 中；业务代码未因编写本文而改动。当前尚未提交，结果属于工作区快照真实预验证；提交后仍需对最终 commit 运行正式严格报告。

## 6. 证据归档状态

本轮已保存以下本地证据，尚未作为公开 PR 附件发布：

- 首次失败场景报告与空间 Filters 错误定位。
- 独立补充链路及时间边界验证。
- 调整后完整场景的机器报告、摘要及 CLI 来源身份。

正式 PR 应补充可访问的报告链接，并对最终提交重跑严格验证。本文没有复制凭据、租户/账号标识、真实资源 ID 或内部源码路径。
