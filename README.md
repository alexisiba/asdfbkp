# asdfbkp

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev)

**General backup manager for the terminal.** A fast, keyboard-driven TUI to back up USB drives, external disks and Android phones on Linux.

```
╭───────────────────────────────────────────────────╮
│                    _    __   _      _             │
│     __ _   ___  __| |  / _| | |__  | |__  _ __    │
│    / _` | (_-< / _` | |  _| | '_ \ | / / | '_ \   │
│    \__,_| /__/ \__,_| |_|   |_.__/ |_\_\ | .__/   │
│                                          |_|      │
│               general backup manager              │
╰───────────────────────────────────────────────────╯
```

## How this project was made

In the spirit of full transparency: the idea, the design decisions and the testing are mine, but **all of the code was written by [Claude](https://www.anthropic.com/claude), by Anthropic**, using the Claude Opus 5.5 model in [Claude Code](https://claude.com/claude-code).

This is my first Go project. I can read and follow the code, but I couldn't have written it, at least not this fast, without Claude. Every commit credits it as a co-author.

## Features

- **Automatic detection** of mounted USB drives and external disks, and of Android phones over **USB or wireless debugging** (ADB). Phones mounted over MTP are supported as a fallback.
- **Full or custom backups.** Pick folders in an interactive tree with checkboxes. Folders load as you open them and sizes are measured in the background.
- **Profiles.** Save up to 3 named selections per device. Devices are recognized by their hardware serial or UUID, never by a changing drive letter or mount point.
- **Incremental and safe.** Only new or changed files are copied, and nothing is ever deleted from a backup, even when it was deleted from the device. Files are written atomically and keep their modification times.
- **Clear progress** with speed and time left, and a log of every failed file when something goes wrong.
- **Single static binary.** No runtime to install.

## Requirements

- Linux.
- For Android phones: [`adb`](https://developer.android.com/tools/releases/platform-tools) (for example the `android-tools-adb` package) and USB or wireless debugging enabled on the phone. Without `adb`, phones are only detected over MTP, which is slower and less reliable.

## Install

With Go 1.27 or later:

```sh
go install github.com/alexisiba/asdfbkp/cmd/asdfbkp@latest
```

Or build from source:

```sh
git clone https://github.com/alexisiba/asdfbkp.git
cd asdfbkp
go build -o asdfbkp ./cmd/asdfbkp
```

## Usage

```sh
asdfbkp                # back up into the current directory
asdfbkp ~/Backups      # back up into ~/Backups
```

Every device gets its own folder inside the destination, for example `~/Backups/Pixel-8/`. The destination is shown, and can be changed, on the summary screen before anything is copied.

1. **Choose a device** from the list.
2. **Choose what to back up:** a saved profile, a full backup, or a custom selection.
3. **Review the summary:** source, destination, size and free space. Optionally save the selection as a profile.
4. **Watch the copy** and read the result.

### Keys

| Screen | Keys |
|---|---|
| Everywhere | `↑`/`↓` move · `enter` select · `esc` back · `q` quit |
| Devices | `r` rescan · `?` how to enable Android debugging |
| Backup mode | `e` edit a profile · `x` delete a profile |
| Folder tree | `→`/`←` open/close · `space` mark · `a` mark all · `f` show files |
| Summary | `d` change destination (`tab` completes) · `space` save as profile · `n` rename profile · `s` save as new profile |
| Copying | `esc` cancel (asks first; files already copied are kept) |

## How backups are stored

```
~/Backups/
└── Pixel-8/
    ├── .asdfbkp-id                 # marks which device owns this folder
    ├── DCIM/Camera/...             # same paths as on the device
    └── adb-R58M123ABC-logs/        # only when a backup had problems
        └── 2026-10-14_15-30-05.log
```

- **Paths are mirrored** exactly as they are on the device.
- A file counts as **up to date** when its size and modification time match, so running the same backup again only copies what changed.
- **Two devices with the same name** never share a folder: the second one gets `name-2`.
- **Logs** are written only when a backup has failures or stops, and list every failed file and why. They are never deleted automatically.
- **Android app data is never backed up.** `Android/data` and `Android/obb` hold each app's private data and caches, often tens of gigabytes. They are shown as `excluded` in the folder tree and skipped by full backups. Media that apps share, such as `Android/media` (WhatsApp photos and videos, for example), is backed up normally.

Profiles are stored in `~/.config/asdfbkp/profiles.yaml`. They only hold *what* to back up, never the destination.

## Contributing

asdfbkp is fully open source, and everyone is warmly invited to take part. Fork it, clone it, study it, build on it, or use it as a starting point for something of your own.

- **Found a bug or have an idea?** [Open an issue](https://github.com/alexisiba/asdfbkp/issues).
- **Want to improve something?** Pull requests are very welcome, from typo fixes to new features.
- **Have a question?** Open an issue as well; there are no silly questions.

Before opening a pull request, please run the tests:

```sh
go vet ./...
go test ./...
```

## Responsible use

asdfbkp is meant for backing up devices **you own or are authorized to access**. It only reads what the device owner allows: Android requires enabling debugging and accepting this computer on the phone's screen.

The author does not endorse, and is not responsible for, any use of this software to access, copy or distribute other people's data without their consent, or for any other unlawful purpose. You are solely responsible for how you use it and for complying with the laws that apply to you. See [LICENSE](LICENSE): the software is provided "as is", without warranty of any kind.

## License

[MIT](LICENSE) © 2026 Alexis Isidoro Bolaños Avalos
