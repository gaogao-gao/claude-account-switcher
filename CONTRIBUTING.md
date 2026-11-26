# Contributing

Thanks for wanting to help. Bug reports, macOS test runs and pull requests are all welcome.

## The most useful things right now

- **Run it on a Mac.** The macOS build passes CI but has never run on a real Mac. Open a [macOS test report](https://github.com/gaogao-gao/claude-account-switcher/issues/new?template=macos_report.yml), even if everything worked.
- **Report breakage after a Claude update.** The switcher depends on how Claude stores its files. When an update changes that, a bug report with the Claude version and a log excerpt is the fastest way to a fix.

## Build and test

Go 1.26 or newer.

```bash
go vet ./...
go test ./...
go build -ldflags "-H=windowsgui -s -w" -o claude-account-switcher.exe ./cmd/claude-account-switcher   # Windows
CGO_ENABLED=1 go build -o claude-account-switcher ./cmd/claude-account-switcher                        # macOS
```

Every pull request runs vet and tests on Windows and macOS.

## Where things are

All Go code is one `main` package in `cmd/claude-account-switcher/`; each file has its tests next
to it in `*_test.go`.

| File | What it does |
| --- | --- |
| `main.go` | Start-up, flags, log |
| `tray.go`, `lang.go`, `icon.go` | The tray menu, its English and Chinese texts, the usage ring |
| `switcher.go` | Accounts, `state.json`, the switch itself, auto-switch |
| `profiles.go`, `desktop.go` | Saving and restoring a sign-in, backups; reading Claude's data folder |
| `crypto_*.go` | Decrypting Claude's token cache (DPAPI, keychain) |
| `usage.go` | Profile and usage requests to `api.anthropic.com`, polling, backoff |
| `sessions.go`, `chatstate.go`, `merge3.go` | The chat list and each chat's archive, pin and title state, three-way merges |
| `tasks.go`, `spaces.go`, `localstorage.go`, `lsdb.go` | Scheduled tasks, projects, sidebar settings in Local Storage |
| `thirdparty.go`, `crossprofile.go`, `pins.go` | The third-party entry and syncing chats and pins with it |
| `skills*.go` | Locking built-in skills |
| `resume.go`, `resume.html` | The window that continues chats a switch stopped |
| `instance.go`, `relaunch.go` | One switcher at a time; leaving Claude's process tree |
| `update.go` | **Check for updates**: this repository's latest release, download, swap, restart |
| `platform_*.go`, `dialog_*.go`, `webview_*.go`, `lslock_*.go` | Finding, quitting and starting Claude, start at login, OS specifics |
| `winres/` | Windows version info and manifest for the exe |

## Ground rules

These keep the tool safe to run on someone's real sign-ins:

- **Tokens never leave the machine** except to `api.anthropic.com`. Never log them, never write them in plain text.
- **Touch only what you must.** Change only the entries a feature needs, back up a file before rewriting it, and never write into a profile Claude is running on.
- **Fail loudly, restore the backup.** If Claude's files do not look as expected, stop, put the backup back and show the error in the menu. Do not guess.
- **No new dependencies** without talking about it in an issue first.

## Pull requests

- One change per pull request. Small is easier to review.
- Say what you tested on: Windows Store app, Windows direct download, macOS.
- Commit messages in plain English, imperative, like the existing history ("Poll usage less often and back off after 429").
- By contributing you agree that your work is licensed under [Apache 2.0](LICENSE), like the rest of the project.
