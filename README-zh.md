# AGS CLI

[English](README.md)

AGS CLI 是一个用于管理腾讯云智能体沙箱（AGS）的命令行工具。它提供了一种便捷的方式来管理沙箱工具、实例，并在隔离环境中执行代码。

## 功能特性

- **工具管理**：创建、列出和删除沙箱工具（模板）
- **实例管理**：启动/停止沙箱实例，支持灵活的生命周期控制
- **代码执行**：支持多种语言（Python、JavaScript、TypeScript、R、Java、Bash）
- **Shell 命令执行**：在沙箱中运行 Shell 命令，支持流式输出
- **文件操作**：在沙箱中上传、下载和管理文件
- **双后端支持**：同时支持 E2B API 和腾讯云 API
- **交互式 REPL**：内置交互模式，支持自动补全
- **流式输出**：长时间运行代码的实时输出流

## 安装

### 使用 go install

```bash
go install github.com/tencentags/ags-cli@latest
```

### 从源码编译

```bash
git clone https://github.com/tencentags/ags-cli.git
cd ags-cli
make build
```

### 跨平台编译

```bash
make build-all  # 编译 Linux、macOS、Windows 版本
```

## 配置

创建 `~/.ags/config.toml`：

```toml
backend = "e2b"
output = "text"

[e2b]
api_key = "your-e2b-api-key"
domain = "tencentags.com"
region = "ap-guangzhou"

[cloud]
secret_id = "your-secret-id"
secret_key = "your-secret-key"
region = "ap-guangzhou"
```

或使用环境变量：

```bash
export AGS_E2B_API_KEY="your-api-key"
export AGS_CLOUD_SECRET_ID="your-secret-id"
export AGS_CLOUD_SECRET_KEY="your-secret-key"
```

### 后端差异

AGS CLI 支持两种后端，功能有所不同：

| 功能 | E2B 后端 | 云端后端 |
|------|----------|----------|
| 认证方式 | 仅 API Key | SecretID + SecretKey |
| 工具管理 | ✗ | ✓ |
| 实例操作 | ✓ | ✓ |
| 代码执行 | ✓ | ✓ |
| 文件操作 | ✓ | ✓ |
| API 密钥管理 | ✗ | ✓ |

**E2B 配置**提供了对 E2B API 的兼容。使用 E2B 后端时，只需要 API Key 即可进行沙箱实例相关操作（创建、列出、删除实例，执行代码，文件操作），但无法管理沙箱工具。

如需管理沙箱工具（列出/获取/创建/更新/删除）和 API 密钥，必须使用**云端后端**，配置腾讯云的 SecretID 和 SecretKey。您可以在此获取 AKSK：https://console.cloud.tencent.com/cam/capi

## 快速开始

```bash
# 进入 REPL 模式
ags

# 列出可用工具
ags tool list

# 创建实例
ags instance create -t code-interpreter-v1

# 执行 Python 代码
ags run -c "print('Hello, World!')"

# 流式输出执行
ags run -s -c "import time; [print(i) or time.sleep(1) for i in range(5)]"

# 执行 Shell 命令
ags exec "ls -la"

# 上传/下载文件
ags file upload local.txt /home/user/remote.txt
ags file download /home/user/file.txt ./local.txt
```

## 命令参考

各命令的详细文档：

| 命令 | 别名 | 描述 | 文档 |
|------|------|------|------|
| `tool` | `t` | 工具管理 | [ags-tool](docs/ags-tool-zh.md) |
| `instance` | `i` | 实例管理 | [ags-instance](docs/ags-instance-zh.md) |
| `run` | `r` | 代码执行 | [ags-run](docs/ags-run-zh.md) |
| `exec` | `x` | Shell 命令执行 | [ags-exec](docs/ags-exec-zh.md) |
| `file` | `f`, `fs` | 文件操作 | [ags-file](docs/ags-file-zh.md) |
| `apikey` | `ak`, `key` | API 密钥管理 | [ags-apikey](docs/ags-apikey-zh.md) |

参见 [ags](docs/ags-zh.md) 了解全局选项和配置详情。

### Man Pages

生成并安装 man pages 以便离线查阅文档：

```bash
# 生成 man pages
make man

# 安装到系统（需要 sudo）
make install-man

# 查看文档
man ags
man ags-tool
man ags-instance
```

## Shell 补全

```bash
# Bash
ags completion bash > /etc/bash_completion.d/ags

# Zsh
ags completion zsh > "${fpath[1]}/_ags"

# Fish
ags completion fish > ~/.config/fish/completions/ags.fish
```

## 许可证

[许可证详情]
