# AGS CLI E2E 测试

AGS CLI 端到端测试脚本，测试 API Key、工具和实例的完整生命周期。

## 前置条件

- AGS CLI 已构建并在 PATH 中可用（或从仓库根目录运行）
- Cloud 后端已配置（`~/.ags/config.toml` 或环境变量）
- 已安装 `jq` 用于 JSON 解析

## 使用方法

```bash
# 使用默认设置运行（ap-guangzhou，外网）
./examples/e2e-test/e2e_test.sh

# 显示帮助
./examples/e2e-test/e2e_test.sh -h
```

## 参数选项

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-r, --region REGION` | 设置地域 | `ap-guangzhou` |
| `-i, --internal` | 使用内网域名 | 禁用 |
| `-d, --domain DOMAIN` | 设置 E2B 域名 | `tencentags.com` |
| `-h, --help` | 显示帮助信息 | - |

## 示例

```bash
# 使用内网
./examples/e2e-test/e2e_test.sh -i

# 使用上海地域
./examples/e2e-test/e2e_test.sh -r ap-shanghai

# 使用自定义 E2B 域名
./examples/e2e-test/e2e_test.sh -d custom-domain.com

# 组合使用
./examples/e2e-test/e2e_test.sh -r ap-shanghai -i
```

## 测试步骤

脚本执行以下 14 个步骤：

1. **列出 API Key** - 获取初始 API Key 数量
2. **创建 API Key** - 创建新的测试 API Key
3. **验证 API Key** - 确认新 Key 存在于列表中
4. **列出工具** - 获取初始工具数量
5. **创建工具** - 创建新的 code-interpreter 工具
6. **验证工具** - 确认新工具存在于列表中
7. **列出实例** - 获取初始实例数量（使用 E2B 后端）
8. **创建并运行实例** - 执行代码并测试流式输出
9. **验证实例** - 确认新实例存在于列表中
10. **测试 exec 命令** - 测试 Shell 执行（exec、exec with cwd/env、exec ps）
11. **测试 file 命令** - 测试文件操作（list、mkdir、upload、cat、stat、download、remove）
12. **删除实例** - 删除创建的实例
13. **删除工具** - 删除创建的工具
14. **删除 API Key** - 删除创建的 API Key

## 输出说明

脚本提供彩色输出：
- 🟢 绿色 (`✓`) - 成功
- 🔴 红色 (`✗`) - 错误
- 🟡 黄色 - 信息/警告
- 🔵 蓝色 - 步骤标题

## 退出码

- `0` - 所有测试通过
- `1` - 一个或多个测试失败
