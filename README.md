<p align="center">
  <img src="resources/icon.png" width="128" alt="Perch">
</p>

<h1 align="center">Perch</h1>

<p align="center">
  A quiet place to read code while your agents work.
</p>

<p align="center">
  English · <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/screenshots/perch-dark.png">
    <img src=".github/screenshots/perch-light.png" width="880" alt="Perch reading a Go file, with the symbol under the pointer explained by its language server">
  </picture>
</p>

When agents write most of the code, you still need a place to see what changed, read it closely and fix a line by hand. An IDE is more than that job needs. Perch is a native code viewer with light editing, and it does only that job.

- **Light.** About 50 MB of memory when idle with a file open. The interface is drawn natively with Metal, with no webview, and language servers start only when you need them.
- **Made for reading agents' work.** Perch remembers how far you have read, and your progress survives an agent's commits.
- **Safe beside an agent.** It never touches your Git index, `HEAD` or branches, and it asks before overwriting a file an agent has just rewritten.

Perch is built in Go with [MyGo](https://github.com/egoist/mygo).

## Install

Download Perch from [Releases](https://github.com/marukolibro/Perch/releases).

**macOS 12 or later**, Apple silicon or Intel: open the `.dmg` and drag Perch to Applications. Perch is signed ad hoc, not notarized by Apple, so macOS blocks it the first time. Open it once, then go to **System Settings → Privacy & Security** and choose **Open Anyway**. Or clear the quarantine flag in a terminal:

```sh
xattr -dr com.apple.quarantine /Applications/Perch.app
```

**Windows**, x64 or ARM64: unzip `perch-<version>-windows-<arch>.zip` and run `Perch.exe`. It is not signed, so SmartScreen may warn you first: choose **More info → Run anyway**.

**Linux**, x86_64 or arm64, with GTK 3:

- Debian or Ubuntu: `sudo apt install ./perch_<version>_<arch>.deb`
- Fedora or openSUSE: `sudo dnf install ./perch-<version>-1.<arch>.rpm`
- Any distribution, without root: put `install.sh` next to `perch-<version>-linux-<arch>.tar.gz` and run `sh install.sh`. It installs Perch in `~/.local` and adds it to the applications menu. `sh install.sh --uninstall` removes it.

Perch is developed and tested on macOS. The Windows and Linux builds compile and pass the tests, but have not been tried on those systems yet. Reports from either are welcome.

## Review what changed

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/screenshots/review-dark.png">
    <img src=".github/screenshots/review-light.png" width="880" alt="The Git page listing a changed file and showing its diff">
  </picture>
</p>

- **Read up to where you stopped.** The Git page lists the files changed since your last review and shows only what you have not read yet. A renamed file shows as one change. Mark a file reviewed and it stays reviewed until it changes again.
- **Compare with anything.** Switch between your last review, uncommitted changes and another branch.
- **See the history.** Browse commits and open any commit's changes. The file tree marks changed files. Each file has its own history, and resting the pointer on a line shows who last changed it.
- **Unread lines in the editor.** While you read a file, the lines you have not reviewed are marked beside the line numbers as they are written.
- **Know without looking.** While you read elsewhere, a quiet dot on the Git button says an agent has left files for you to read. It goes once you have reviewed them.

Reviews are kept as Git trees under `refs/perch/`, so they cost nothing in your working tree and survive commits, rebases and restarts.

## Read and find your way

- Syntax highlighting, rendered Markdown including the HTML in READMEs, and previews of PNG, JPEG, GIF, WebP and SVG.
- Quick open (`⌘P`, also `file:line`), project search with recent queries, and find and replace.
- Through language servers: go to definition, find references, symbols in the file or the project, the type and documentation of the name under the pointer, and the path bar's symbol path.
- Each project reopens with its tabs, caret, scroll position and layout, in its own window.
- English and Simplified Chinese, light and dark.

## Edit lightly

Perch edits text with undo, find and replace, and file operations in the tree: create, rename, move to the Trash. Open tabs follow a rename and keep their unsaved edits. If an agent rewrites a file while you edit it, Perch asks which version to keep instead of overwriting either one.

## What Perch does not do

Perch stays small on purpose. It has no plugin system, terminal, AI chat, debugger or refactoring tools. It does not talk to agents either: it shows what changed, and you decide what to read.

## Keyboard shortcuts

These are the default shortcuts. In **Settings → Shortcuts**, select a shortcut to change it, clear it, or restore its default.

On Windows and Linux, `⌘` is `Ctrl`, `⌥` is `Alt` and `⌃` is `Ctrl`.

| Action | Shortcut |
| --- | --- |
| Go to file, or `file:line` | `⌘P` |
| Go to line | `⌃G` |
| Find in file | `⌘F` |
| Find and replace | `⌥⌘F` |
| Search the project | `⇧⌘F` |
| Go to symbol in file | `⇧⌘O` |
| Go to symbol in project | `⌘T` |
| Go to definition | `F12` or `⌘`-click |
| Find references | `⇧F12` |
| Go back / forward | `⌃-` / `⌃⇧-` |
| Next / previous tab | `⌃Tab` or `⇧⌘]` / `⌃⇧Tab` or `⇧⌘[` |
| Tab 1 to 8 / last tab | `⌘1` to `⌘8` / `⌘9` |
| Toggle sidebar / reveal in file tree | `⌘B` / `⇧⌘E` |
| Zoom in / out / reset | `⌘=` / `⌘-` / `⌘0` |
| Copy line reference | `⇧⌘C` |
| Toggle Markdown preview | `⇧⌘V` |
| Show changes | `⇧⌘R` |
| Mark file reviewed | `⌘↩` |
| Next / previous change | `⌥F5` / `⌥⇧F5` |
| Save / close / reopen tab | `⌘S` / `⌘W` / `⇧⌘T` |
| New window / open folder | `⇧⌘N` / `⌘O` |
| Settings | `⌘,` |

## Language servers

Perch knows 22 languages out of the box and works with any server that speaks the Language Server Protocol. Open **Settings → Language servers** to see which servers are installed, turn languages on or off, and copy install commands.

To add another language, choose **Open Configuration File** on that page and add an entry:

```json
{
  "languageServers": {
    "zig": {
      "name": "Zig",
      "extensions": [".zig"],
      "command": "zls",
      "args": [],
      "rootMarkers": ["build.zig"],
      "install": "brew install zls"
    }
  }
}
```

`command` must be a program name on your `PATH` or an absolute path. Perch reads this file only from your own settings folder and never runs a command named by an opened project.

## Build from source

You need Go 1.27.4 or later, and Git for the review features.

```sh
git clone https://github.com/marukolibro/Perch.git
cd Perch
go tool mygo build
open build/darwin-arm64/Perch.app
```

Run the checks with `go test ./...` and `golangci-lint run`. Build for other systems with `-platform`, such as `go tool mygo build -platform windows/amd64,linux/amd64`.

## License

[MIT](LICENSE)

File icons come from [Atom Material Icons](https://github.com/AtomMaterialUI/iconGenerator) by Elior "Mallowigi" Boukhobza, under the [MIT license](internal/fileicons/assets/LICENSE).
