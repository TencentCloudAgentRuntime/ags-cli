# 故障排查

本文档涵盖使用 AGS CLI 时可能遇到的常见错误及解决方法。

## 认证失败

### E2B 后端：API Key 无效或缺失

**现象：**

```
Error: E2B API key is required (set AGS_E2B_API_KEY or e2b.api_key in config)
```

**原因：** CLI 无法找到 E2B API Key。

**解决方法：**

1. 设置环境变量：
   ```bash
   export AGS_E2B_API_KEY="your-api-key"
   ```
2. 或在 `~/.ags/config.toml` 中添加：
   ```toml
   backend = "e2b"

   [e2b]
   api_key = "your-e2b-api-key"
   ```
3. 验证密钥是否已加载：
   ```bash
   ags config show  # 检查 api_key 是否已设置
   ```

### Cloud 后端：AKSK 凭证无效或缺失

**现象：**

```
Error: cloud API credentials are required (set AGS_CLOUD_SECRET_ID/AGS_CLOUD_SECRET_KEY or cloud.secret_id/cloud.secret_key in config)
```

**原因：** 腾讯云 SecretID 或 SecretKey 未配置。

**解决方法：**

1. 从[腾讯云控制台 - API 密钥](https://console.cloud.tencent.com/cam/capi)获取凭证。
2. 设置环境变量：
   ```bash
   export AGS_CLOUD_SECRET_ID="your-secret-id"
   export AGS_CLOUD_SECRET_KEY="your-secret-key"
   ```
3. 或在 `~/.ags/config.toml` 中添加：
   ```toml
   backend = "cloud"

   [cloud]
   secret_id = "your-secret-id"
   secret_key = "your-secret-key"
   region = "ap-guangzhou"
   ```

### 认证成功但请求被拒绝

**现象：** HTTP 403 Forbidden 或 "authorization failed" 错误。

**原因：** 凭证有效但权限不足。

**解决方法：**

1. 确保腾讯云子账户已关联 `QcloudAGSFullAccess` 策略（或等效权限）。
2. 如使用临时凭证（STS Token），请确认未过期。

---

## 后端能力不匹配

### 工具或 API Key 操作返回 "Not Supported"

**现象：**

```
Error: operation not supported by e2b backend
```

**原因：** E2B 后端仅支持实例操作、代码执行和文件操作。工具管理 (`ags tool`) 和 API Key 管理 (`ags apikey`) 需要使用 Cloud 后端。

**解决方法：**

切换到 Cloud 后端：

```bash
# 通过环境变量
export AGS_CLOUD_SECRET_ID="your-secret-id"
export AGS_CLOUD_SECRET_KEY="your-secret-key"

# 使用 --backend 参数
ags tool list --backend cloud
```

或在 `~/.ags/config.toml` 中设置默认后端：

```toml
backend = "cloud"
```

**参考：**

| 功能 | E2B 后端 | Cloud 后端 |
|------|---------|-----------|
| 工具管理 | ✗ | ✓ |
| 实例操作 | ✓ | ✓ |
| 代码执行 | ✓ | ✓ |
| 文件操作 | ✓ | ✓ |
| API Key 管理 | ✗ | ✓ |

### 无效的 Backend 值

**现象：**

```
Error: invalid backend: xyz (must be 'e2b' or 'cloud')
```

**解决方法：** 在配置文件或 `--backend` 参数中使用 `e2b` 或 `cloud`。

---

## 网络和地域问题

### 连接超时或被拒绝

**现象：**

```
Error: failed to create sandbox: Post "https://api.ap-guangzhou.tencentags.com/...": dial tcp: i/o timeout
```

**原因：** CLI 无法访问 AGS API 端点，可能原因：
- 企业防火墙或代理阻止出站 HTTPS
- 地域或域名配置错误
- DNS 解析失败

**解决方法：**

1. **检查连通性：**
   ```bash
   curl -I https://api.ap-guangzhou.tencentags.com
   ```
2. **如在代理后面**，配置 HTTP 代理：
   ```bash
   export HTTP_PROXY="http://your-proxy:port"
   export HTTPS_PROXY="http://your-proxy:port"
   ```
3. **如在腾讯云内网**，使用内部端点：
   ```bash
   ags --internal <command>
   ```
4. **确认地域正确**，可用地域包括：
   - `ap-guangzhou`（默认）
   - 查看 [AGS 文档](https://cloud.tencent.com/document/product/1732) 获取最新地域列表。

### 地域错误

**现象：** 在某个地域创建的沙箱工具或实例在另一个地域查询时不可见。

**解决方法：** 确保配置中的 `region` 与资源创建的地域一致：

```toml
[e2b]
region = "ap-guangzhou"

[cloud]
region = "ap-guangzhou"
```

或通过命令行传入 `--region`：

```bash
ags tool list --region ap-guangzhou
```

---

## 端口转发问题

### 端口转发返回 403 或连接被拒绝

**现象：** `ags proxy` 已连接但所有请求返回 HTTP 403 或被立即关闭。

**原因：** 目标端口未在 AGS 控制台中开放。

**解决方法：**

1. 登录 [AGS 沙箱控制台](https://console.cloud.tencent.com/ags)。
2. 进入沙箱实例 → **网络** → **开放端口**。
3. 将远程端口号添加到白名单。
4. 重试：
   ```bash
   ags proxy <sandbox-id> 8080
   ```

---

## 配置问题

### 弃用警告

**现象：**

```
Warning: config field "e2b.region" is deprecated, please use top-level "region" instead.
```

**原因：** 配置文件使用了旧版字段布局。

**解决方法：** 将弃用字段移至 `~/.ags/config.toml` 的顶层：

```toml
# 旧版（已弃用）
[e2b]
region = "ap-guangzhou"
domain = "tencentags.com"

# 新版（推荐）
region = "ap-guangzhou"
domain = "tencentags.com"

[e2b]
api_key = "your-api-key"
```

### 找不到配置文件

**现象：** CLI 使用默认值，忽略你的设置。

**原因：** 配置文件不在预期路径。

**解决方法：** 默认配置路径为 `~/.ags/config.toml`，可指定自定义路径：

```bash
ags --config /path/to/config.toml <command>
```

---

## 实例和沙箱错误

### 不能同时指定 --instance 和 --tool-name

**现象：**

```
Error: cannot specify both --instance and --tool-name/--tool
```

**解决方法：** 只使用其中一个选项：
- `--instance <id>` 定位已有实例
- `--tool-name <name>` 或 `--tool <name>` 从工具创建新实例

### 沙箱创建失败

**现象：**

```
Error: failed to create sandbox: ...
```

**可能原因和解决方法：**

1. **工具不存在：** 验证工具名称：
   ```bash
   ags tool list
   ```
2. **配额超限：** 在[腾讯云控制台](https://console.cloud.tencent.com/ags)检查 AGS 资源配额。
3. **凭证无效：** 参考上方[认证失败](#认证失败)部分。

---

## 移动沙箱问题

### 找不到 ADB

**现象：**

```
Error: adb not found in PATH
```

**解决方法：** 安装 [Android SDK Platform Tools](https://developer.android.com/tools/releases/platform-tools) 并确保 `adb` 在 `$PATH` 中：

```bash
# macOS（通过 Homebrew）
brew install android-platform-tools

# 验证
adb version
```

### Mobile Connect 失败

**现象：** `ags mobile connect` 无法建立隧道。

**解决方法：**

1. 确认实例是**移动**类型沙箱（不是代码沙箱）。
2. 确保实例正在运行：
   ```bash
   ags instance list
   ```
3. 查看隧道日志获取详情：
   ```bash
   cat ~/.ags/tunnel-<sandbox-id>.log
   ```

---

## 常见错误速查表

| 错误信息 | 可能原因 | 快速修复 |
|---------|---------|---------|
| `E2B API key is required` | 缺少 API Key | 设置 `AGS_E2B_API_KEY` |
| `cloud API credentials are required` | 缺少 AKSK | 设置 `AGS_CLOUD_SECRET_ID` 和 `AGS_CLOUD_SECRET_KEY` |
| `invalid backend` | Backend 名称拼写错误 | 使用 `e2b` 或 `cloud` |
| `operation not supported` | 当前后端不支持此操作 | 切换到 `cloud` 后端 |
| `dial tcp: i/o timeout` | 网络问题 | 检查防火墙、代理、地域 |
| `cannot specify both --instance and --tool-name` | 参数冲突 | 只使用一个 |
| `adb not found` | ADB 未安装 | 安装 Android Platform Tools |
| `failed to create sandbox` | 认证、配额或工具问题 | 检查凭证和工具名称 |

---

## 仍然无法解决？

如果以上步骤无法解决你的问题：

1. 使用 `--output json` 运行失败的命令以获取结构化错误详情。
2. 在 [GitHub](https://github.com/TencentCloudAgentRuntime/ags-cli/issues) 上提交 issue，包含：
   - 完整的错误信息
   - CLI 版本（`ags --version`）
   - 操作系统和 Go 版本
   - 运行的命令（隐藏凭证）
