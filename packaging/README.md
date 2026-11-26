# Packaging

## macOS

`macos/Info.plist` is the bundle description of `Claude Account Switcher.app`. The build
workflow puts the universal binary into the bundle, sets both versions from `cmd/claude-account-switcher/main.go`, signs it
ad hoc and zips it as `claude-account-switcher-macos.zip`. `LSUIElement` keeps it out of the Dock.
