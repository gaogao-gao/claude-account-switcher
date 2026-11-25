<div align="center">

<img src="assets/banner.svg" width="880" alt="Claude Account Switcher: switch the Claude desktop app between accounts. No email code, your chats come along.">

# Claude Account Switcher (enhanced fork)

[![Tests](https://github.com/gaogao-gao/claude-account-switcher/actions/workflows/test.yml/badge.svg)](https://github.com/gaogao-gao/claude-account-switcher/actions/workflows/test.yml)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)
![Windows | macOS](https://img.shields.io/badge/platform-Windows%20%7C%20macOS-lightgrey)

</div>

A tray tool for the Claude desktop app that switches locally between several Claude accounts in
one click, keeps chats and their settings in sync between those accounts, and adds a third-party
API endpoint as one more entry next to them. Sign-ins stay on your machine.

一个 Claude 桌面应用的托盘工具：在本机一键切换多个 Claude 账号，在这些账号之间同步对话及其设置，并把第三方 API 端点作为与账号并列的一项加入菜单。登录凭据只保存在本机。

This is a fork of [SnlperStripes/claude-account-switcher](https://github.com/SnlperStripes/claude-account-switcher)
v0.3.6, version `0.3.6-ext.x`. It is not the official build and is not affiliated with or endorsed
by Anthropic or the upstream author. Apache License 2.0; see [LICENSE](LICENSE), [NOTICE](NOTICE)
and [CHANGES.md](CHANGES.md) for what changed.

本项目是 [SnlperStripes/claude-account-switcher](https://github.com/SnlperStripes/claude-account-switcher)
v0.3.6 的分支，版本号为 `0.3.6-ext.x`。它不是官方版本，与 Anthropic 及上游作者均无关联，也未获其认可。采用 Apache License 2.0；参见 [LICENSE](LICENSE)、[NOTICE](NOTICE)，改动见 [CHANGES.md](CHANGES.md)。

---

## Features / 功能

**English**

- **Multiple accounts, one click.** Each account's sign-in is saved when you switch away and put
  back when you switch to it, so you enter an email code once per account. Picking an account in
  the tray menu quits Claude, swaps the sign-in and starts Claude again in about ten seconds.
- **Usage of every account.** 5-hour and weekly usage of every saved account, with reset times.
  The tray icon is a ring that fills with the active account's 5-hour usage.
- **Auto-switch** (off by default). Moves Claude to the account with the most room left once the
  active one reaches 90–98%. A second option first uses up weekly limits that are about to reset
  mostly unused.
- **Chats come along.** The Claude Code chat list of every account is synced into the one you
  switch to, along with each chat's archive, pin and title state, scheduled tasks, sidebar
  sections and groups, per-folder permission modes and projects. Three-way merges decide which
  change wins; deletions spread to every account, with deleted chats kept in a trash folder. A chat
  that went on separately in two accounts keeps both versions, one titled as a branch.
- **Stopped chats go on.** Chats that were in the middle of a reply when Claude had to quit are
  listed in a small window; the checked ones continue on the new account.
- **Third-party API endpoint as a peer entry.** When CC Switch or a Claude policy gives Claude a
  third-party `inferenceProvider`, the menu lists *Third-party (name)* after the accounts. Clicking
  it restarts Claude on that provider; clicking an account switches back. With **Settings ▸ Sync
  chats with third-party** (on by default) chats and pinned chats are synced both ways between the
  accounts and the third-party profile, writing only into the side Claude is not running on.
- **Scratch chats stay home.** Chats started without a project folder belong to the profile they
  were started in and are not copied to the other one.
- **Built-in skill locking** (on by default). A few built-in skills you may not use are emptied and
  locked in every account and on third-party; their files are backed up once. Turning the setting
  off unlocks them.
- **Safer switching.** Claude's files are backed up before each switch, an interrupted switch is
  rolled back on the next start, and renewed tokens are saved to the account right away.
- **English and Chinese menu**, following the system display language.
- **macOS build** as a universal menu bar app (see [macOS](#macos--macos)).

**中文**

- **多账号一键切换。** 切走某个账号时保存它的登录，切回时恢复，所以每个账号只需输入一次邮箱验证码。在托盘菜单中选择账号后，会关闭 Claude、换上该账号的登录并重新启动 Claude，大约十秒完成。
- **所有账号的用量。** 显示每个已保存账号的 5 小时和每周用量及重置时间。托盘图标是一个圆环，按当前账号的 5 小时用量填充。
- **自动切换**（默认关闭）。当前账号用量达到 90–98% 时，把 Claude 切到余量最多的账号。另一个选项会优先用完即将重置、且大部分未用的每周额度。
- **对话随账号同步。** 各账号的 Claude Code 对话列表会同步到你切换到的账号，同时同步每个对话的归档、置顶和标题状态，以及定时任务、侧边栏分区与分组、按文件夹的权限模式和项目。通过三方合并决定以哪一边的修改为准；删除会同步到所有账号，被删除的对话保留在回收站文件夹中。在两个账号中各自继续过的对话会保留两个版本，其中一个标为分支。
- **被中断的对话可继续。** Claude 退出时正在回复的对话会列在一个小窗口中，勾选的对话会在新账号上继续。
- **第三方 API 端点作为并列的一项。** 当 CC Switch 或 Claude 策略为 Claude 设置了第三方 `inferenceProvider` 时，菜单会在账号之后显示“第三方（名称）”。点击它会让 Claude 在该供应商上重启；点击某个账号则切回。开启 **设置 ▸ 与第三方同步对话**（默认开启）时，对话和置顶在各账号与第三方配置之间双向同步，只写入 Claude 当前没有运行的一侧。
- **临时对话留在原处。** 未选择项目文件夹就开始的对话属于开始它的那个配置，不会复制到另一边。
- **内置技能锁定**（默认开启）。在所有账号和第三方中清空并锁定几个你可能用不到的内置技能，原文件备份一次。关闭该设置即解锁。
- **更安全的切换。** 每次切换前备份 Claude 的文件；切换中途中断时，下次启动会回滚；应用续期的令牌会立即保存到对应账号。
- **中英文菜单**，跟随系统显示语言。
- **macOS 版本**，为通用二进制的菜单栏应用（见 [macOS](#macos--macos)）。

## Install / 安装

**English**

Download the build for your platform from this repository's
[Releases](https://github.com/gaogao-gao/claude-account-switcher/releases) and run it. No installer,
no runtime.

- **Windows 10/11:** `claude-account-switcher-windows-amd64.exe`, one file. Works with the
  Microsoft Store app and the direct download of Claude.
- **macOS:** `claude-account-switcher-macos.zip` holds `Claude Account Switcher.app`. Unzip it and
  move it to Applications.

Then turn on **Settings ▸ Start at login** in its menu. The builds are not code-signed: on Windows,
SmartScreen asks once (**More info ▸ Run anyway**); on macOS, run
`xattr -dr com.apple.quarantine "Claude Account Switcher.app"` once. Each build has a `.sha256`
file next to it.

To update, start the newer build: it asks the running one to quit and takes over. Your accounts
and settings stay in the data folder. There is no self-update.

**中文**

从本仓库的 [Releases](https://github.com/gaogao-gao/claude-account-switcher/releases) 下载对应平台的版本并运行即可，无需安装程序或运行时。

- **Windows 10/11：** `claude-account-switcher-windows-amd64.exe`，单个文件。适用于 Microsoft Store 版和直接下载版的 Claude。
- **macOS：** `claude-account-switcher-macos.zip` 内含 `Claude Account Switcher.app`。解压后移到“应用程序”文件夹。

然后在菜单中开启 **设置 ▸ 开机时启动**。这些版本没有代码签名：Windows 上 SmartScreen 会提示一次（**更多信息 ▸ 仍要运行**）；macOS 上需执行一次 `xattr -dr com.apple.quarantine "Claude Account Switcher.app"`。每个版本旁都附有 `.sha256` 文件。

更新时直接启动新版本即可：它会让正在运行的旧版本退出并接替。账号和设置保留在数据文件夹中。没有自动更新。

## First run / 首次使用

**English**

1. Start the switcher while you are signed in to Claude. It picks up that account within a minute.
2. Choose **Add account…**. Claude restarts on its sign-in screen; your first account is kept.
3. Sign in with the second account. The switcher notices, restarts Claude once more, and the chats
   of your other account show up.

From then on, click an account to switch.

> [!IMPORTANT]
> Never use **Sign out** in Claude to change accounts. It can end that sign-in on Anthropic's
> side, and the saved copy stops working; the account then shows "signed out, add it again". Use
> **Add account…** to sign it in again.

**中文**

1. 在已登录 Claude 的状态下启动切换器，它会在一分钟内识别该账号。
2. 选择 **添加账号…**。Claude 会重启到登录界面，第一个账号会被保留。
3. 用第二个账号登录。切换器检测到后会再重启一次 Claude，另一个账号的对话随即出现。

之后点击账号即可切换。

> [!IMPORTANT]
> 不要用 Claude 里的 **退出登录** 来换账号。这可能会在 Anthropic 一侧终止该登录，使保存的副本失效，该账号会显示“已退出登录，请重新添加”。此时用 **添加账号…** 重新登录即可。

## Auto-switch / 自动切换

**English**

Both options in the **Auto-switch** submenu are off until you turn them on.

- **At 90%** to **At 98%**: once the active account reaches that (5-hour or weekly), Claude moves to
  the account with the most room left. It acts only when another account is at least 10 points
  below the threshold, the numbers are fresh, the last switch was over 10 minutes ago and Claude is
  running.
- **Use up expiring weekly limits first**: moves to an account whose weekly limit resets within 24
  hours with half or more unused, unless you switched by hand in the last 5 hours or a Claude Code
  chat was active in the last 15 minutes.

A switch restarts Claude. Anthropic's terms do not forbid having more than one account, but they
do forbid "bypassing any of our systems or protective measures"; whether automatic switching fits
how you use your accounts is your call.

**中文**

**自动切换** 子菜单中的两个选项在你开启之前都是关闭的。

- **用量达到 90%** 至 **98% 时**：当前账号（5 小时或每周用量）达到该值时，把 Claude 切到余量最多的账号。仅当另一个账号比阈值至少低 10 个百分点、数据是最新的、距上次切换超过 10 分钟且 Claude 正在运行时才会执行。
- **优先用完即将重置的每周额度**：切到每周额度将在 24 小时内重置、且还剩一半以上的账号；如果你在过去 5 小时内手动切换过，或过去 15 分钟内有 Claude Code 对话在活动，则不执行。

切换会重启 Claude。Anthropic 的条款并不禁止拥有多个账号，但禁止“绕过我们的任何系统或保护措施”；自动切换是否适合你的使用方式，由你自行判断。

## Third-party API / 第三方 API

**English**

The entry appears while CC Switch (`Claude-3p/configLibrary`) or a Claude policy (the
`Software\Policies\Claude` registry key on Windows) sets an `inferenceProvider`, named after the
config CC Switch applied (*API* without a name). It is checked while Claude runs on it.

- Clicking it quits Claude, syncs the chats of both profiles (when that setting is on), sets
  `deploymentMode` in `Claude-3p/claude_desktop_config.json` to `3p` and starts Claude. Clicking an
  account, or **Add account…**, sets it back to `1p`. That key is the only thing the switcher writes
  in that file.
- Chats and pinned chats sync between the accounts and `Claude-3p` with the same rules as between
  accounts. Pins are only added, never removed, on the other side. Scheduled tasks, projects and
  sidebar settings are not shared with third-party.
- While Claude runs on the provider, no account is checked, and Auto-switch and Import chats wait.

**中文**

当 CC Switch（`Claude-3p/configLibrary`）或 Claude 策略（Windows 上的注册表项 `Software\Policies\Claude`）设置了 `inferenceProvider` 时，菜单会显示这一项，名称取自 CC Switch 当前启用的配置（无名称时为“API”）。Claude 运行在第三方上时该项被勾选。

- 点击它会关闭 Claude，同步两边配置的对话（该设置开启时），把 `Claude-3p/claude_desktop_config.json` 中的 `deploymentMode` 设为 `3p`，然后启动 Claude。点击某个账号或 **添加账号…** 会把它改回 `1p`。这是切换器在该文件中唯一写入的内容。
- 对话和置顶在各账号与 `Claude-3p` 之间同步，规则与账号之间相同。置顶只会在另一边追加，不会删除。定时任务、项目和侧边栏设置不与第三方共享。
- Claude 运行在第三方上时，不检查任何账号，自动切换和导入对话会暂停等待。

## Chats that were working / 被中断的对话

**English**

Claude has to quit for a switch, so a Claude Code chat that is writing a reply or running a tool
stops. Before Claude starts again, a window lists those chats, all checked. **Continue** lets
Claude carry on with the checked ones; **Don't continue**, Escape or closing the window leaves them
where they stopped. With no answer, all of them go on after 30 seconds.

<img src="assets/resume-dialog.png" width="462" alt="The window that asks which stopped chats continue">

The switcher sets Claude's own quit mark (`interruptedByQuitAt`) on the chats you keep checked, in
the account you switch to, and Claude continues them by itself. On Windows the window uses
WebView2, with a plain dialog as fallback; on macOS it uses WebKit.

**中文**

切换需要退出 Claude，所以正在回复或运行工具的 Claude Code 对话会停下。Claude 重新启动前，会弹出一个窗口列出这些对话，默认全部勾选。点击 **继续** 让 Claude 继续勾选的对话；点击 **不继续**、按 Esc 或关闭窗口则让它们停在原处。30 秒内无人操作时全部继续。

切换器会在目标账号中为保持勾选的对话写入 Claude 自己的退出标记（`interruptedByQuitAt`），由 Claude 自行继续。Windows 上该窗口使用 WebView2，不可用时改用普通对话框；macOS 上使用 WebKit。

## macOS / macOS

**English**

- The macOS build passes CI but has not been tested on a real Mac. Reports are welcome via
  [issues](https://github.com/gaogao-gao/claude-account-switcher/issues/new?template=macos_report.yml).
- The first start asks once for access to "Claude Safe Storage" in the keychain, the key Claude uses
  to encrypt its sign-in.
- Claude is quit with SIGTERM, so no Automation permission is needed. Start at login uses a
  LaunchAgent labelled `io.github.gaogao-gao.claude-account-switcher`.
- The third-party profile folder on macOS is assumed to be `Claude-3p` next to Claude's data folder,
  as on Windows; this is unverified.

**中文**

- macOS 版本通过了 CI，但尚未在真实的 Mac 上测试。欢迎通过 [issues](https://github.com/gaogao-gao/claude-account-switcher/issues/new?template=macos_report.yml) 反馈。
- 首次启动时会请求一次读取钥匙串中“Claude Safe Storage”的权限，这是 Claude 加密登录信息所用的密钥。
- 用 SIGTERM 退出 Claude，因此不需要“自动化”权限。开机启动使用标签为 `io.github.gaogao-gao.claude-account-switcher` 的 LaunchAgent。
- macOS 上的第三方配置目录假定与 Windows 相同，为 Claude 数据文件夹旁的 `Claude-3p`，尚未验证。

## Data and privacy / 数据与隐私

**English**

| | |
| --- | --- |
| Data folder | `~/.claude-account-switcher` (`%USERPROFILE%\.claude-account-switcher` on Windows), **Open data folder** in the menu: saved sign-ins, `state.json`, backups, `deleted-chats/`, `skill-backups/`, sync records and the log |
| Tokens | Stored exactly as Claude encrypted them; never written in plain text, never sent anywhere but `api.anthropic.com` |
| Network | `api.anthropic.com` for profiles and usage, nothing else. No telemetry, no update check |
| Claude's files | `config.json` sign-in entries and the cookie file are saved and restored; chat entries, `spaces.json`, scheduled tasks, Local Storage sidebar settings and pinned chats are merged. Each is backed up before it is rewritten. Local Storage is only written while Claude has it closed |
| Remove an account | Quit the switcher, delete its folder under `accounts/` and its entry in `state.json` |
| Uninstall | Turn off **Start at login**, quit, delete the program and the data folder |

`claude-account-switcher -status` prints every saved account with its usage and exits.

**中文**

| | |
| --- | --- |
| 数据文件夹 | `~/.claude-account-switcher`（Windows 上为 `%USERPROFILE%\.claude-account-switcher`），菜单中的 **打开数据文件夹**：保存的登录、`state.json`、备份、`deleted-chats/`、`skill-backups/`、同步记录和日志 |
| 令牌 | 按 Claude 加密后的原样保存；从不以明文写入，除 `api.anthropic.com` 外不发送到任何地方 |
| 网络 | 仅访问 `api.anthropic.com` 获取账号信息和用量。没有遥测，也不检查更新 |
| Claude 的文件 | 保存并恢复 `config.json` 中的登录条目和 Cookie 文件；合并对话条目、`spaces.json`、定时任务、Local Storage 中的侧边栏设置和置顶对话。每项改写前都会备份。Local Storage 只在 Claude 未打开它时写入 |
| 删除账号 | 退出切换器，删除 `accounts/` 下对应的文件夹以及 `state.json` 中的条目 |
| 卸载 | 关闭 **开机时启动**，退出，删除程序和数据文件夹 |

`claude-account-switcher -status` 会列出所有已保存账号及其用量后退出。

## Build / 构建

**English**

Go 1.26 or newer.

```bash
go vet ./...
go test ./...
go build -ldflags "-H=windowsgui -s -w" -o claude-account-switcher.exe .   # Windows
CGO_ENABLED=1 go build -o claude-account-switcher .                        # macOS
```

Release builds come from `.github/workflows/build.yml` on a `v*` tag. Dependencies beyond the
standard library: `fyne.io/systray`, `jchv/go-webview2` (Windows), `webview/webview_go` (macOS),
`syndtr/goleveldb` and `golang.org/x/sys`.

**中文**

需要 Go 1.26 或更新版本。构建命令见上方代码块。发布版本由 `.github/workflows/build.yml` 在推送 `v*` 标签时构建。标准库以外的依赖：`fyne.io/systray`、`jchv/go-webview2`（Windows）、`webview/webview_go`（macOS）、`syndtr/goleveldb` 和 `golang.org/x/sys`。

## Caveats / 注意事项

**English**

- This relies on how the Claude desktop app stores its files today, not on a published interface.
  An app update can change that. When files do not look as expected, the switcher stops, restores
  its backup and shows the error in the menu.
- It switches the Claude desktop app, including its Claude Code chats. The `claude` command in a
  terminal keeps its own sign-in and is not touched.
- Syncing writes into Claude's data while Claude is closed or into the profile it is not running
  on. Keep the backups until you are sure everything looks right.
- This fork and the upstream switcher use the same single-instance port; run only one of them.

**中文**

- 本工具依赖 Claude 桌面应用当前的文件存储方式，而非公开接口，应用更新可能改变这些方式。文件与预期不符时，切换器会停止、恢复备份并在菜单中显示错误。
- 它切换的是 Claude 桌面应用（包括其中的 Claude Code 对话）。终端中的 `claude` 命令有自己的登录，不受影响。
- 同步只在 Claude 关闭时写入它的数据，或只写入 Claude 当前未运行的那个配置。在确认一切正常之前，请保留备份。
- 本分支与上游切换器使用同一个单实例端口，请只运行其中一个。

## Contributing / 参与

**English**

Bug reports and pull requests are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). Report security
problems privately; see [SECURITY.md](SECURITY.md).

**中文**

欢迎提交问题和拉取请求，参见 [CONTRIBUTING.md](CONTRIBUTING.md)。安全问题请私下报告，参见 [SECURITY.md](SECURITY.md)。

## License / 许可

**English**

[Apache 2.0](LICENSE). Based on Claude Account Switcher by SnlperStripes; keep the
[`NOTICE`](NOTICE) file when you pass this on, and say what you changed.

**中文**

[Apache 2.0](LICENSE)。基于 SnlperStripes 的 Claude Account Switcher；转发或分发时请保留 [`NOTICE`](NOTICE) 文件，并说明你的修改。
