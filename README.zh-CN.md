<p align="center">
  <img src="docs/icon.png" width="112" alt="Kumo 图标">
</p>

<h1 align="center">Kumo</h1>

<p align="center">快速、原生的 mihomo（Clash）桌面客户端：一个开关即可连接，完整仪表盘尽在眼前。</p>

<p align="center">
  <a href="https://github.com/kumo-app/kumo/releases/latest"><img src="https://img.shields.io/badge/release-v0.4.4-3b82f6" alt="版本 v0.4.4"></a>
  <img src="https://img.shields.io/badge/platforms-macOS%20%7C%20Windows%20%7C%20Linux-6b7280" alt="平台：macOS、Windows、Linux">
  <img src="https://img.shields.io/badge/core-mihomo%20v1.19.32-10b981" alt="内置内核：mihomo v1.19.32">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-0ea5e9" alt="许可证：MIT"></a>
</p>

<p align="center"><a href="README.md">English</a> | <b>简体中文</b></p>

> 应用界面目前提供繁体中文（默认）和英文。下文引用的界面文字以繁体显示，与应用中一致。

## 亮点

- **开箱即用。** 每个安装包都内置 [mihomo](https://github.com/MetaCubeX/mihomo) 内核。
  Kumo 会用独立的数据目录、空闲的控制端口和随机密钥运行它，退出时自动停止。
- **一个开关即可连接。** 在首页或托盘打开系统代理（macOS、Windows、Linux GNOME）。
  原有代理设置会先保存，断开或退出时恢复；万一 Kumo 崩溃，下次启动时会自动修复。
- **真正的仪表盘。** 实时上传/下载曲线、内核内存、当前连接、本次流量、正在使用的节点及其延迟，
  以及当前订阅的用量与到期时间。
- **节点一目了然。** 每个代理组一张卡片，延迟按颜色区分，支持全部测速、按延迟排序、一键切换到最快节点。
- **订阅管理。** 通过链接添加（可每 6/12/24 小时自动更新）或导入本地 YAML；切换时内核直接重新加载，无需重启。
- **实时活动。** 规则、连接（显示链路、规则与流量，可单独关闭）和日志，均可筛选。
- **原生且轻量。** 纯 Go 编写，基于 [MyGo](https://github.com/egoist/mygo)，没有 Electron，也没有 WebView。
  支持浅色与深色主题、六种主题色、繁体中文与英文。

## 截图

| | |
|---|---|
| ![首页](docs/screenshots/home.png)<br>**首页** — 连接开关、实时流量、模式、节点与延迟 | ![节点](docs/screenshots/nodes.png)<br>**节点** — 代理组、延迟、一键选最快 |
| ![订阅](docs/screenshots/subscriptions.png)<br>**订阅** — 链接添加、用量、到期、更新 | ![连接](docs/screenshots/connections.png)<br>**连接** — 实时链路、规则与流量 |
| ![规则](docs/screenshots/rules.png)<br>**规则** — 内核规则列表，可筛选 | ![设置](docs/screenshots/settings.png)<br>**设置** — 启动行为、进阶模式、外观 |
| ![内核](docs/screenshots/core.png)<br>**进阶模式 › 内核** — 端口、局域网、TUN、IPv6、日志级别、内核来源 | ![深色](docs/screenshots/home-dark.png)<br>**深色主题** |
| ![配置](docs/screenshots/config.png)<br>**进阶模式 › 配置** — 运行中的配置，可复制 | ![首次启动](docs/screenshots/first-run.png)<br>**首次启动** — 添加订阅前只显示一条简短提示 |

## 下载与安装

前往 [Releases](https://github.com/kumo-app/kumo/releases/latest) 下载最新版本，`SHA256SUMS` 列出了每个文件的校验值。

| 平台 | 文件 | 说明 |
|---|---|---|
| macOS（Apple 芯片与 Intel） | `Kumo-<版本>.dmg` 或 `Kumo-<版本>-macos-universal.app.zip` | 将 Kumo 拖入“应用程序”。 |
| Windows 10/11（x64） | `Kumo-Setup-<版本>-amd64.exe` 或 `Kumo-<版本>-windows-amd64-portable.zip` | 安装程序，或解压后运行 `Kumo.exe`。 |
| Linux（x64 / arm64） | `kumo_<版本>_<架构>.deb` 或 `kumo-<版本>-linux-<架构>.tar.gz` | `sudo apt install ./kumo_*.deb`；系统代理开关需要 GNOME。 |

安装包**未经代码签名**：

- **macOS：** 首次打开时右键点击 Kumo › **打开** › **打开**，或执行
  `xattr -dr com.apple.quarantine /Applications/Kumo.app`。
- **Windows：** 如出现 SmartScreen 提示，点击 **更多信息** › **仍要运行**。

## 快速上手

1. **添加订阅。** 打开「訂閱」，粘贴服务商提供的 Clash 订阅链接并点击「新增」
   （本地 YAML 请用「匯入檔案…」），新订阅会立即启用。
2. **连接。** 在「首頁」打开「系統代理」，浏览器和应用即通过 Kumo 联网；关闭即断开。
   除非开启「設定 › 啟動時自動開啟系統代理」，否则启动时系统代理保持关闭。
3. **选择模式。**
   - **規則**（推荐）— 由订阅规则决定哪些流量走代理。
   - **全域** — 所有流量经由所选节点。
   - **直連** — 不使用代理。
4. **选择节点。** 在首页直接切换（⚡ 一键切到最快），或到「節點」页全部测速后选择。

托盘（macOS 为菜单栏）可查看状态、切换系统代理和模式。关闭窗口后 Kumo 仍留在托盘；
选择「結束 Kumo」或按 ⌘Q 会停止内核并恢复代理设置。

## 进阶模式

在「設定 › 進階模式」开启后，侧边栏会新增两个页面：

- **核心** — 混合/HTTP/SOCKS 端口、允许局域网连接、TUN、IPv6、日志级别；内核来源
  （内置，或填写地址、端口与密钥的外部控制器）；状态、版本、执行文件、日志与重启；延迟测试网址与超时。
- **設定檔** — 内核正在运行的 YAML（订阅内容加上 Kumo 的覆盖项），只读，可复制或打开文件位置。

快捷键：**⌘R / Ctrl+R** 刷新，**⌘, / Ctrl+,** 打开设置。

**增强模式**（TUN）让所有应用都经过 Kumo，包括不理会系统代理的应用。可在首页开关、托盘或 Dock 菜单、内核页开启。首次开启时 Kumo 会请求一次管理员授权：

- **macOS** — 输入一次密码后，由 launchd 辅助程序（`app.kumo.tun.<uid>`）仅在 Kumo 请求时以 root 运行内核，并使用独立的 root 所有目录（`/Library/Application Support/Kumo`）。Kumo 将配置交给辅助程序；含有监听器（listeners 等）的配置会被拒绝，控制器固定在 127.0.0.1。
- **Linux** — 通过 `pkexec` 将内核复制到 `/usr/local/lib/kumo/<uid>/`，仅限你本人执行（私有群组，否则以 ACL 限制，需要 `setfacl`），并授予 `cap_net_admin` 与 `cap_net_bind_service`（需要 polkit 与 libcap）。polkit 对话框显示的程序为 `/bin/sh`，Kumo 会事先说明将安装的内容。
- **Windows** — 以管理员身份运行 Kumo；mihomo 自带 wintun。若使用另一个管理员账户提权，Kumo 会以该账户运行，系统代理与设置都会属于该账户而非你本人；建议以自己的账户提权运行。

只有 Kumo 自带的内核会获得权限：其 SHA-256 固定在 Kumo 中（`corepin`，来自 `scripts/fetch-core/core.lock.json`），并由 root 在 root 所有的副本上校验；更新 Kumo 或内核后会再次请求授权，`PATH` 上的内核永远不会被提权。macOS 上 root 内核的控制器只是辅助程序目录中的 unix socket；Kumo 经由 127.0.0.1 上的过滤器（Kumo 的 root 副本）访问它：需要密钥，并拒绝会开放监听端口的配置、按路径加载、Kumo 以外的 PATCH、内核升级与调试接口，因此即使拿到密钥的程序也无法以 root 开放端口。辅助程序只服务安装它的那个 Kumo 版本，以运行中进程的代码签名（cdhash）核对；未签名的版本只能核对进程名。Linux 的内核以你的身份运行，无需过滤器。关闭增强模式会以无特权方式重启内核。Kumo 0.4.3 留下的可被任何人运行的副本会在启动时提示移除。在 macOS 上移除辅助程序：`sudo launchctl bootout system/app.kumo.tun.$(id -u)`，再删除 `/Library/LaunchDaemons` 中的 plist 与 `/Library/Application Support/Kumo`。

在 macOS 上右键点击 Dock 图标可打开快捷菜单：系统代理、增强模式、代理模式，以及每个代理组的子菜单（切换节点或选最快）。

## 从源码构建

需要 Go 1.27+。无需 cgo，可在任意主机上交叉编译所有平台。

```sh
go run ./scripts/fetch-core            # 下载本机对应的固定版本 mihomo 内核
go tool mygo build                     # 为当前平台打包到 build/
go tool mygo build -platform linux/amd64,windows/amd64,darwin/universal
go tool mygo dev                       # 热重载运行
go test ./...
```

内核版本与 SHA-256 摘要固定在 `scripts/fetch-core/core.lock.json`。`KUMO_DATA` 指定数据目录，
`KUMO_PAGE` 指定启动页面（如 `/nodes`）。`.dmg` 与 Windows 安装程序在 macOS 和 Windows 的 CI 运行器上构建。

## 项目结构

| 路径 | 内容 |
|---|---|
| `clash/` | 控制器客户端（REST + WebSocket） |
| `core/` | 运行内置内核、生成运行配置、管理订阅 |
| `sysproxy/` | macOS、Windows、Linux 的系统代理，含备份与恢复 |
| `main.go`、`state.go`、`engine.go`、`tray.go` | 启动、实时状态、内核与代理生命周期、托盘与菜单 |
| `shell.go`、`look.go`、`chart.go` | 窗口布局、视觉样式、图表 |
| `page_*.go` | 每个页面一个文件 |
| `lang.go`、`lang_zh.go` | 翻译 |
| `scripts/` | 内核下载、截图、退出测试 |

## 致谢

- [MyGo](https://github.com/egoist/mygo)（MIT）— Kumo 所使用的原生 Go UI 框架。
- [mihomo](https://github.com/MetaCubeX/mihomo)（GPL-3.0）— 内置的代理内核。
- [Lucide](https://lucide.dev)（ISC）— 图标。

所有内置组件见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 许可

Kumo 以 [MIT 许可证](LICENSE) 发布。安装包中附带的 mihomo 内核是独立程序，采用 GPL-3.0，
以独立进程运行，其许可证与源码链接随每个安装包提供（`core/LICENSE-mihomo.txt`）。
