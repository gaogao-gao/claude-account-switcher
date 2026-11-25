package main

import "strings"

// texts is what the menu, its tooltips and the status line say. The log
// stays in English.
type texts struct {
	Title, SignIn, OtherCopy, AlreadyRunning                            string
	CouldNotStart, NoSignedIn                                           string
	SwitchTip, AddAccount, AddAccountTip                                string
	AutoSwitch, AutoOff, AutoAt, AutoAtTip, SpendWeekly, SpendWeeklyTip string
	Settings, StartAtLogin, LockSkills, LockSkillsTip                   string
	SyncChats, SyncChatsTip                                             string
	ImportChats, ImportChatsTip, OpenFolder, Quit                       string
	CheckUpdate, CheckUpdateTip, CheckingUpdate, UpToDate               string
	Downloading, StartingNew, UpdateFailed                              string
	Switching, SignInOther, Importing, QuittingAfter                    string
	UsageUnknown, Week, Until, Resets, SignedOut                        string
	NoteFull, NoteSpend, LastAccount                                    string
	ThirdPartyItem, ThirdPartyTip, SwitchingThird                       string
	// Added to the title of a chat's copy kept as a branch.
	Branch string
}

var english = texts{
	Title:          "Claude Account Switcher",
	SignIn:         "Open Claude and sign in",
	OtherCopy:      "Another Claude Account Switcher (%s) tried to start. Keep only one: quit or remove the other.",
	AlreadyRunning: "Another Claude Account Switcher is already running. Quit it from its tray icon, or remove it, then start this one again.",
	CouldNotStart:  "Could not start Claude: ",
	NoSignedIn:     "no signed-in account",
	SwitchTip:      "Switch the Claude app to this account",
	AddAccount:     "Add account…",
	AddAccountTip:  "Restart Claude on its sign-in screen, your current account is kept",
	AutoSwitch:     "Auto-switch",
	AutoOff:        "Off",
	AutoAt:         "At %.0f%%",
	AutoAtTip:      "Switch to the account with the most room left once the active one reaches this",
	SpendWeekly:    "Use up expiring weekly limits first",
	SpendWeeklyTip: "Switch to an account whose weekly limit resets within a day with half or more of it unused",
	Settings:       "Settings",
	StartAtLogin:   "Start at login",
	LockSkills:     "Keep extra built-in skills locked",
	LockSkillsTip:  "Keep built-in skills you don't use out of Claude, in every account",
	SyncChats:      "Sync chats with third-party",
	SyncChatsTip:   "Show your accounts' chats on third-party, and its chats in your accounts",
	ImportChats:    "Import chats from all accounts",
	ImportChatsTip: "Restart Claude so every chat shows up in this account",
	OpenFolder:     "Open data folder",
	CheckUpdate:    "Check for updates (now %s)",
	CheckUpdateTip: "Ask GitHub for the latest release and install it",
	CheckingUpdate: "Checking for updates…",
	UpToDate:       "Up to date (%s)",
	Downloading:    "Downloading version %s…",
	StartingNew:    "Starting version %s…",
	UpdateFailed:   "Update failed: ",
	Quit:           "Quit",
	Switching:      "Switching to %s…",
	SignInOther:    "Sign in with the other account in Claude",
	Importing:      "Importing chats…",
	QuittingAfter:  "Quitting once the switch is done…",
	UsageUnknown:   "usage unknown",
	Week:           "week",
	Until:          " until ",
	Resets:         ", resets ",
	SignedOut:      "signed out, add it again",
	NoteFull:       "Auto-switched at %s, %s was nearly full",
	NoteSpend:      "Auto-switched at %s to use up this account's week before it resets %s",
	LastAccount:    "the last account",
	ThirdPartyItem: "Third-party (%s)",
	ThirdPartyTip:  "Restart Claude on the third-party API provider set up in CC Switch or by policy",
	SwitchingThird: "Switching to third-party (%s)…",
	Branch:         "(branch)",
}

var chinese = texts{
	Title:          "Claude 账号切换器",
	SignIn:         "请打开 Claude 并登录",
	OtherCopy:      "另一个 Claude 账号切换器（%s）试图启动。请只保留一个，退出或删除另一个。",
	AlreadyRunning: "另一个 Claude 账号切换器已在运行。请从它的托盘图标退出它（或将其删除），然后再启动这一个。",
	CouldNotStart:  "无法启动 Claude：",
	NoSignedIn:     "没有已登录的账号",
	SwitchTip:      "把 Claude 应用切换到这个账号",
	AddAccount:     "添加账号…",
	AddAccountTip:  "重启 Claude 并进入登录界面，当前账号会保留",
	AutoSwitch:     "自动切换",
	AutoOff:        "关闭",
	AutoAt:         "用量达到 %.0f%% 时",
	AutoAtTip:      "当前账号用量达到这个值时，切换到余量最多的账号",
	SpendWeekly:    "优先用完即将重置的每周额度",
	SpendWeeklyTip: "切换到每周额度一天内重置、且还剩一半以上的账号",
	Settings:       "设置",
	StartAtLogin:   "开机时启动",
	LockSkills:     "锁定多余的内置技能",
	LockSkillsTip:  "在所有账号中屏蔽不用的 Claude 内置技能",
	SyncChats:      "与第三方同步对话",
	SyncChatsTip:   "让各账号的对话出现在第三方中，反之亦然",
	ImportChats:    "导入所有账号的对话",
	ImportChatsTip: "重启 Claude，让所有对话都出现在当前账号中",
	OpenFolder:     "打开数据文件夹",
	CheckUpdate:    "检查更新（当前 %s）",
	CheckUpdateTip: "从 GitHub 获取最新版本并安装",
	CheckingUpdate: "正在检查更新…",
	UpToDate:       "已是最新版本（%s）",
	Downloading:    "正在下载 %s…",
	StartingNew:    "正在启动 %s…",
	UpdateFailed:   "更新失败：",
	Quit:           "退出",
	Switching:      "正在切换到 %s…",
	SignInOther:    "请在 Claude 中登录另一个账号",
	Importing:      "正在导入对话…",
	QuittingAfter:  "切换完成后退出…",
	UsageUnknown:   "用量未知",
	Week:           "本周",
	Until:          "，恢复于 ",
	Resets:         "，重置于 ",
	SignedOut:      "已退出登录，请重新添加",
	NoteFull:       "已于 %s 自动切换，%s 用量将满",
	NoteSpend:      "已于 %s 自动切换，以便在本账号每周额度于 %s 重置前用完",
	LastAccount:    "上一个账号",
	ThirdPartyItem: "第三方（%s）",
	ThirdPartyTip:  "重启 Claude，改用 CC Switch 或策略中设置的第三方 API 供应商",
	SwitchingThird: "正在切换到第三方（%s）…",
	Branch:         "(分支)",
}

// tr is the language in use, English until setLanguage picks another.
var tr = &english

// setLanguage switches to Chinese when that is the system's display language.
func setLanguage() {
	if isChinese(uiLanguage()) {
		tr = &chinese
	}
}

// isChinese reports whether a language tag such as "zh-Hans-CN", "zh-CN"
// or "zh_CN.UTF-8" names Chinese.
func isChinese(tag string) bool {
	return strings.HasPrefix(strings.ToLower(tag), "zh")
}

// firstAppleLanguage returns the first entry of what
// `defaults read -g AppleLanguages` prints, a list like ("zh-Hans-CN", "en-US").
func firstAppleLanguage(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if tag := strings.Trim(strings.TrimSpace(line), `"(),`); tag != "" {
			return tag
		}
	}
	return ""
}
