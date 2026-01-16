# ags-browser

管理浏览器沙箱

## 概要

```
ags browser <子命令> [选项]
ags b <子命令> [选项]
```

## 描述

浏览器沙箱提供可通过 VNC 访问的远程浏览器环境。您可以通过基于 Web 的 VNC 客户端查看和交互浏览器，也可以通过 Chrome DevTools Protocol (CDP) 进行程序化控制。

## 子命令

| 子命令 | 描述 |
|--------|------|
| `vnc` | 显示浏览器沙箱的 VNC URL |

## vnc

显示用于访问浏览器沙箱的 VNC URL。可以连接到现有实例或创建新实例。

```
ags browser vnc [选项]
ags b vnc [选项]
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `--instance` | string | - | 要连接的实例 ID |
| `-t, --tool` | string | - | 用于创建新实例的工具名称 |
| `--tool-id` | string | - | 工具 ID（仅云端后端） |
| `--timeout` | int | `300` | 实例超时时间（秒） |
| `-p, --port` | int | `9000` | VNC 服务端口 |
| `--time` | bool | `false` | 打印耗时 |

注意：必须指定 `--instance` 或 `--tool`/`--tool-id` 之一，但不能同时指定。

### 输出

命令输出：

| 字段 | 描述 |
|------|------|
| `instance_id` | 沙箱实例 ID |
| `tool` | 使用的工具名称 |
| `status` | 实例状态 |
| `vnc_url` | 通过 noVNC Web 客户端访问浏览器的 URL |
| `cdp_url` | 用于程序化访问的 Chrome DevTools Protocol URL |

### 示例

```bash
# 显示现有实例的 VNC URL
ags browser vnc --instance sbi-xxxxxxxx

# 创建新的浏览器沙箱并显示 VNC URL
ags browser vnc --tool browser-v1

# 使用工具 ID 创建
ags browser vnc --tool-id sdt-xxxxxxxx

# 创建时设置自定义超时（1小时）
ags browser vnc --tool browser-v1 --timeout 3600

# 使用自定义端口
ags browser vnc --tool browser-v1 --port 5900

# JSON 输出
ags browser vnc --tool browser-v1 -o json
```

### VNC URL 格式

VNC URL 遵循以下格式：
```
https://{port}-{instance_id}.{region}.{domain}/novnc/vnc_lite.html?&path=websockify?access_token={token}
```

### CDP URL 格式

用于程序化浏览器控制的 CDP URL：
```
https://{port}-{instance_id}.{region}.{domain}/cdp?access_token={token}
```

## 全局选项

参见 [ags(1)](ags-zh.md) 了解全局选项。

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-instance](ags-instance-zh.md) - 实例管理
- [ags-tool](ags-tool-zh.md) - 工具管理
