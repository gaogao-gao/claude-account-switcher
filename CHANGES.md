# Changes / 改动

This is a modified version of [Claude Account Switcher](https://github.com/SnlperStripes/claude-account-switcher)
v0.3.6 by SnlperStripes, modified by gaogao-gao. It is not the official build and is not affiliated
with Anthropic or the upstream author. Apache License 2.0, see `LICENSE` and `NOTICE`.

本项目是 SnlperStripes 的 [Claude Account Switcher](https://github.com/SnlperStripes/claude-account-switcher)
v0.3.6 的修改版，由 gaogao-gao 修改。它不是官方版本，与 Anthropic 及上游作者无关。采用 Apache License 2.0，参见 `LICENSE` 和 `NOTICE`。

## 0.3.6-ext.2

- **Settings ▸ Check for updates** shows the running version. A click asks this repository's GitHub
  releases for the latest one; on Windows a newer build is downloaded, checked against its
  `.sha256` and started in place of the old one, on macOS the release page opens. Upstream's
  releases are never offered, and nothing is checked in the background.
- **设置 ▸ 检查更新** 显示当前版本。点击后从本仓库的 GitHub 发布中获取最新版本：Windows 上下载更新的版本、按 `.sha256` 校验并替换旧版本启动，macOS 上打开发布页面。不会提供上游的版本，也不会在后台检查。

## 0.3.6-ext.1

Changes against upstream v0.3.6. / 相对上游 v0.3.6 的改动。

### Sync between accounts / 账号间同步

- Besides the chat list, each chat's archive, pin and title state, scheduled tasks, sidebar
  sections and groups, per-folder permission modes (Local Storage) and projects are synced across
  accounts with three-way merges.
- 除对话列表外，每个对话的归档、置顶和标题状态、定时任务、侧边栏分区与分组、按文件夹的权限模式（Local Storage）和项目，都通过三方合并在各账号间同步。
- Deletions of chats, tasks and projects spread to every account; removed chat entries go to
  `deleted-chats/`, and same-name entries moved on the same day no longer overwrite each other.
- 对话、定时任务和项目的删除会同步到所有账号；被移除的对话条目放入 `deleted-chats/`，同一天移入的同名条目不再互相覆盖。
- A chat that went on separately in two accounts keeps both versions; the older one is saved as a
  branch, titled "(branch)" or "(分支)" by the menu language. A chat the app resumed or compacted
  under a new conversation (`priorCliSessionIds`) does not count as such a fork.
- 在两个账号中各自继续过的对话会保留两个版本，较旧的一个另存为分支，标题按菜单语言加上“(branch)”或“(分支)”。应用恢复或压缩后换了新会话 ID 的对话（`priorCliSessionIds`）不算分叉。
- An account whose projects or scheduled tasks all vanished at once (two or more), or whose file
  or `spaces` entry is gone, counts as reset: it gets the items back instead of deleting them
  everywhere.
- 某账号的项目或定时任务一次全部消失（原有至少两项），或其文件、`spaces` 条目不见了，视为被重置：恢复这些内容，而不是把它当作删除扩散到所有账号。
- `spaces.json`, `scheduled-tasks.json` and Local Storage are backed up before each rewrite. Local
  Storage is only written while its LevelDB `LOCK` can be taken and the database is unchanged
  since it was read.
- 改写前会备份 `spaces.json`、`scheduled-tasks.json` 和 Local Storage。只有在能取得 LevelDB 的 `LOCK` 且数据库自读取后未被改动时，才写入 Local Storage。
- Session files with a UTF-8 BOM are read; `.DS_Store` and other stray files are skipped.
- 能读取带 UTF-8 BOM 的会话文件；跳过 `.DS_Store` 等无关文件。

### Third-party API / 第三方 API

- A third-party provider set by CC Switch or a Claude policy is one more entry in the account
  list, *Third-party (name)*. Clicking it quits Claude, syncs both profiles' chats, sets
  `deploymentMode` in `Claude-3p/claude_desktop_config.json` to `3p` and starts Claude; clicking an
  account or Add account sets it back to `1p`. While Claude runs on the provider, accounts are not
  checked, and Auto-switch, spending weekly limits and Import chats wait.
- CC Switch 或 Claude 策略设置的第三方供应商成为账号列表中的一项“第三方（名称）”。点击它会关闭 Claude，同步两边配置的对话，把 `Claude-3p/claude_desktop_config.json` 中的 `deploymentMode` 设为 `3p` 并启动 Claude；点击账号或“添加账号”会改回 `1p`。Claude 运行在第三方上时不检查账号，自动切换、优先用完每周额度和导入对话会暂停等待。
- **Settings ▸ Sync chats with third-party** (on by default): chats sync both ways between the
  accounts and `Claude-3p` with the rules used between accounts, writing only into the side Claude
  is not running on. A background sync runs only when a third-party profile exists, and never at
  the same time as a switch.
- **设置 ▸ 与第三方同步对话**（默认开启）：对话在各账号与 `Claude-3p` 之间按账号间的规则双向同步，只写入 Claude 当前未运行的一侧。后台同步只在存在第三方配置时运行，且不会与切换同时进行。
- Pinned Claude Code chats (`starred-local-code-sessions` in `claude_desktop_config.json`) carry
  over too. Pins are only added at the end of the other side's list, never removed there.
- 置顶的 Claude Code 对话（`claude_desktop_config.json` 中的 `starred-local-code-sessions`）也会带到另一边。只在另一边的列表末尾追加，不会删除。
- Scratch chats (started without a project folder) stay in the profile they were started in. Copies
  an earlier sync made in the other profile are moved to `deleted-chats/`, and such copies no
  longer feed changes back to the original.
- 临时对话（未选项目文件夹就开始的对话）只留在开始它的配置中。之前同步复制到另一边的副本会移入 `deleted-chats/`，这些副本也不再把改动同步回原对话。

### Switching safety / 切换安全

- A switch writes a `switch-in-progress` marker naming its backup; a switch cut off by a crash is
  rolled back on the next start, while Claude is closed.
- 切换时写入一个指向其备份的 `switch-in-progress` 标记；切换中途崩溃时，下次启动（Claude 关闭时）会回滚。
- The signed-in account's tokens are saved to its profile whenever the app renews or replaces them.
- 应用续期或更换登录令牌后，会及时存入当前账号的档案。
- Backups keep everything from the last 7 days and at least the newest 3; same-second backups no
  longer overwrite each other.
- 备份保留最近 7 天的全部以及至少最新 3 份；同一秒内的备份不再互相覆盖。
- A 429 backs off by the larger of Retry-After and the exponential backoff. Import checks for a
  signed-in account before restarting Claude.
- 遇到 429 时，退避时间取 Retry-After 与指数退避中的较大值。导入对话前会先确认有已登录的账号。
- When started from a Claude Code terminal, the switcher relaunches itself outside Claude's process
  tree. Helper commands time out, panics are logged, and quitting waits for a running switch.
- 从 Claude Code 终端启动时，切换器会在 Claude 的进程树之外重新启动自己。辅助命令有超时，panic 会写入日志，退出前会等待正在进行的切换完成。

### Built-in skills / 内置技能

- New setting, on by default: keep `setup-claude`, `morning`, `google-workspace`,
  `consolidate-memory` and `explain-usage` locked in every account and on third-party (emptied, a
  `.locked-by-owner` placeholder, a deny ACL on Windows or `chflags uchg` on macOS). Contents are
  backed up once to `skill-backups/`. Turning it off unlocks them.
- 新设置，默认开启：在所有账号和第三方中锁定 `setup-claude`、`morning`、`google-workspace`、`consolidate-memory` 和 `explain-usage`（清空内容，放置 `.locked-by-owner` 占位文件，Windows 上加拒绝 ACL，macOS 上设 `chflags uchg`）。原内容备份一次到 `skill-backups/`。关闭该设置即解锁。

### Menu / 菜单

- New layout: accounts, the third-party entry, Add account, an Auto-switch submenu (Off, At
  90–98%, use up expiring weekly limits first) and a Settings submenu (start at login, chat sync
  with third-party, skill locking, import, data folder).
- 新布局：账号、第三方项、添加账号、“自动切换”子菜单（关闭、用量达到 90–98% 时、优先用完即将重置的每周额度）和“设置”子菜单（开机启动、与第三方同步对话、技能锁定、导入、数据文件夹）。
- English and Simplified Chinese, following the system display language. The tooltip is only
  updated when its text changes.
- 支持英文和简体中文，跟随系统显示语言。提示文字只在内容变化时更新。

### macOS / macOS

- Claude is quit with SIGTERM and a timeout instead of AppleScript, so no Automation prompt.
- 用带超时的 SIGTERM 代替 AppleScript 退出 Claude，因此不会弹出“自动化”权限请求。
- Keychain reads time out after a minute, and a failure is retried after 5 minutes.
- 钥匙串读取一分钟超时，失败后 5 分钟再试。
- Only a real cancel (-128) in the resume dialog stops the chats; path comparison is exact.
- 只有在继续对话的对话框中真正取消（-128）才会停止对话；路径比较改为精确匹配。
- The build makes an ad-hoc signed, menu-bar-only `Claude Account Switcher.app` in a zip. Start at
  login uses the LaunchAgent label `io.github.gaogao-gao.claude-account-switcher`, matching the
  bundle id; a login item an earlier build of this fork wrote under upstream's label for the same
  program is replaced.
- 构建产物为 zip 中经临时签名、仅显示在菜单栏的 `Claude Account Switcher.app`。开机启动使用与 bundle id 一致的 LaunchAgent 标签 `io.github.gaogao-gao.claude-account-switcher`；本分支早期版本以上游标签为同一程序写入的登录项会被替换。

### Release / 发布

- Version `0.3.6-ext.1`; module path `github.com/gaogao-gao/claude-account-switcher`. Newer builds
  of this fork replace older ones; an upstream build is told to stay out, and the menu asks the user
  to quit one of them. A fork build that finds another switcher running says so in a dialog.
- 版本 `0.3.6-ext.1`；模块路径 `github.com/gaogao-gao/claude-account-switcher`。本分支的新版本会替换旧版本；上游版本启动时会被拒绝，菜单会提示用户退出其中一个。本分支发现已有切换器在运行时会弹窗说明。
- Self-update is off and points to this repository only (from ext.2 on demand, see above).
  Upstream's website (`docs/`) and winget manifests are removed; Windows resources describe the fork.
- 自动更新关闭，且只指向本仓库（ext.2 起改为手动检查，见上）。移除了上游的网站（`docs/`）和 winget 清单；Windows 资源信息标明为分支版本。
