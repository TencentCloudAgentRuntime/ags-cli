# ags-proxy

将沙箱端口转发到本地

## 概要

```
ags proxy <sandbox_id> [local_port:]<remote_port> [选项]
```

## 描述

创建一个本地 HTTP/WebSocket 反向代理，将所有流量转发到远程沙箱的指定端口，类似于 `kubectl port-forward`。Access Token 会自动注入到每个代理请求中。

完整支持 HTTP 和 WebSocket 协议，适用于 Web 开发服务器（如 Vite、webpack-dev-server）、API 服务器以及沙箱中运行的任何 HTTP 服务。

> **注意**：使用此命令前，需要先在沙箱控制台中将远程端口配置为可访问状态。

## 端口格式

| 格式 | 描述 |
|------|------|
| `<remote_port>` | 将远程端口转发到本地相同端口 |
| `<local_port>:<remote_port>` | 将远程端口转发到指定的本地端口 |

## 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `--address` | string | `127.0.0.1` | 本地监听地址 |
| `--verbose` | bool | `false` | 启用详细请求日志 |

## 示例

```bash
# 将沙箱 8080 端口转发到本地 8080
ags proxy sandbox-xxx 8080

# 将沙箱 8080 端口转发到本地 3000
ags proxy sandbox-xxx 3000:8080

# 绑定到所有网卡（可被局域网内其他机器访问）
ags proxy sandbox-xxx 8080 --address 0.0.0.0

# 启用详细日志，查看每个代理请求
ags proxy sandbox-xxx 8080 --verbose
```

## 输出

代理启动后，会打印本地和远程地址：

```
Forwarding from 127.0.0.1:8080 -> 8080
  Local:  http://127.0.0.1:8080
  Remote: https://8080-sandbox-xxx.ap-guangzhou.tencentags.com

Press Ctrl+C to stop.
```

按 **Ctrl+C** 停止代理。进行中的请求最多有 5 秒时间完成后，进程才会退出。

## 完整工作流示例

```bash
# 1. 创建沙箱实例
ags instance create -t <tool-name>
# ✓ Instance created: sandbox-xxx

# 2. 在沙箱内启动 Web 服务（可通过 exec 或 run 执行）
ags exec sandbox-xxx "python3 -m http.server 8080 &"

# 3. 将沙箱端口转发到本地
ags proxy sandbox-xxx 8080
# Forwarding from 127.0.0.1:8080 -> 8080
#   Local:  http://127.0.0.1:8080
#   Remote: https://8080-sandbox-xxx.ap-guangzhou.tencentags.com

# 4. 在本地访问该服务
curl http://127.0.0.1:8080
# 或在浏览器中打开

# 5. 完成后按 Ctrl+C 停止代理
```

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-instance](ags-instance-zh.md) - 实例管理
- [ags-exec](ags-exec-zh.md) - Shell 命令执行
