# ags-mobile

移动沙箱 ADB 连接管理

## 概要

```
ags mobile <子命令> [选项]
ags m <子命令> [选项]
```

## 描述

通过加密 WebSocket 隧道提供对远程 Android 沙箱的安全 ADB 访问。支持多个并发连接，网络中断时自动重连。

## 前置条件

- 需要在本地安装 ADB（Android SDK Platform-Tools）
- 可通过 `ADB_PATH` 环境变量指定 ADB 路径，或确保 `adb` 在 `PATH` 中

## 子命令

| 子命令 | 描述 |
|--------|------|
| `connect` | 连接到移动沙箱（后台隧道 + adb connect） |
| `disconnect` | 断开移动沙箱连接 |
| `list` | 列出活跃的连接 |
| `adb` | 通过沙箱 ID 执行 ADB 命令 |
| `tunnel` | 前台运行 ADB 隧道（内部/调试用） |

## connect

连接到移动沙箱，自动建立后台 WebSocket 隧道并注册 ADB 设备。

```
ags mobile connect <sandbox_id>
```

此命令会：
1. 获取沙箱的 Access Token
2. 启动后台隧道进程
3. 自动执行 `adb connect` 连接到本地隧道端口
4. 将连接信息记录到 `~/.ags/tunnels.json`

### 示例

```bash
# 连接到移动沙箱
ags mobile connect 3239677239b46cca9bc50a969b19fcad80360375
```

## disconnect

断开移动沙箱连接，终止后台隧道并执行 `adb disconnect`。

```
ags mobile disconnect <sandbox_id>
ags mobile disconnect --all
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `--all` | bool | `false` | 断开所有活跃连接 |

### 示例

```bash
# 断开指定沙箱
ags mobile disconnect 3239677239b46cca9bc50a969b19fcad80360375

# 断开所有连接
ags mobile disconnect --all
```

## list

列出所有活跃的 ADB 隧道连接，显示沙箱 ID、本地端口和状态。

```
ags mobile list
```

### 输出示例

```
SANDBOX                  ADB ADDRESS            STATUS
3239677239b46cca9bc5...  127.0.0.1:60504        connected
```

## adb

通过沙箱 ID 执行 ADB 命令。自动查找本地隧道端口，并将命令传递给原生 `adb`。

```
ags mobile adb <sandbox_id> [adb_args...]
```

### 示例

```bash
# 查看 Android 版本
ags mobile adb <sandbox_id> shell getprop ro.build.version.release

# 查看 SDK 版本
ags mobile adb <sandbox_id> shell getprop ro.build.version.sdk

# 查看屏幕分辨率
ags mobile adb <sandbox_id> shell wm size

# 列出已安装的应用
ags mobile adb <sandbox_id> shell pm list packages

# 查看文件
ags mobile adb <sandbox_id> shell ls /sdcard

# 安装 APK
ags mobile adb <sandbox_id> install app.apk

# 查看日志
ags mobile adb <sandbox_id> logcat
```

## tunnel

在前台运行 ADB 隧道，主要供 `connect` 内部使用，也可用于调试。

```
ags mobile tunnel <sandbox_id> [选项]
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `--daemon` | bool | `false` | 以守护进程模式运行（由 connect 使用） |
| `--port` | int | `0` | 本地监听端口（0 = 自动分配） |

## 使用原生 ADB

通过 `ags mobile connect` 连接后，`ags mobile list` 会显示本地 ADB 地址。之后可以直接使用原生 `adb` 命令操作：

```bash
# 查看连接信息
ags mobile list
# 输出: 127.0.0.1:60504

# 使用原生 adb
adb -s 127.0.0.1:60504 shell
adb -s 127.0.0.1:60504 push local.apk /data/local/tmp/
adb -s 127.0.0.1:60504 install app.apk
adb -s 127.0.0.1:60504 logcat
```

## 完整工作流示例

```bash
# 1. 配置环境变量
export AGS_E2B_API_KEY="your-api-key"
export AGS_REGION=ap-chongqing
export AGS_DOMAIN=tencentags.com

# 2. 查看可用的移动沙箱工具
ags tool list

# 3. 创建移动沙箱实例
ags instance create -t <mobile_tool_name> --timeout 1800

# 4. 连接到沙箱（自动建立隧道 + ADB 连接）
ags mobile connect <sandbox_id>

# 5. 查看连接状态
ags mobile list

# 6. 执行 ADB 命令
ags mobile adb <sandbox_id> shell getprop ro.build.version.release

# 7. 或使用原生 adb（connect 后即可）
adb -s 127.0.0.1:<port> shell

# 8. 完成后断开连接
ags mobile disconnect <sandbox_id>
```

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-instance](ags-instance-zh.md) - 实例管理
