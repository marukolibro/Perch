<p align="center">
  <img src="resources/icon.png" width="128" alt="Perch">
</p>

<h1 align="center">Perch</h1>

<p align="center">
  agent 写代码的时候，一个安静读代码的地方。
</p>

<p align="center">
  <a href="README.md">English</a> · 简体中文
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/screenshots/perch-dark.png">
    <img src=".github/screenshots/perch-light.png" width="880" alt="Perch 正在阅读一个 Go 文件，鼠标所指的符号由语言服务给出说明">
  </picture>
</p>

当大部分代码由 agent 来写，你仍然需要一个地方看清改了什么、仔细读一读、顺手改一行。IDE 对这件事来说太重了。Perch 是一个原生的代码查看器，带轻量编辑，只做这一件事。

- **轻。** 打开一个文件时，空闲内存约 50 MB。界面通过 Metal 原生绘制，没有 WebView；语言服务只在需要时才启动。
- **为阅读 agent 的改动而做。** Perch 记得你读到了哪里，agent 提交之后进度也不会丢。
- **和 agent 一起用也安全。** 它不会动你的 Git 暂存区、`HEAD` 和分支；agent 刚改写了你正在编辑的文件时，它会先问你保留哪个版本。

Perch 使用 Go 和 [MyGo](https://github.com/egoist/mygo) 构建。

## 安装

从 [Releases](https://github.com/marukolibro/Perch/releases) 下载 Perch。

**macOS 12 及以上**，Apple 芯片和 Intel 都可以：打开 `.dmg`，把 Perch 拖进“应用程序”。Perch 使用临时签名，没有经过 Apple 公证，所以第一次打开时 macOS 会拦截。先打开一次，然后到 **系统设置 → 隐私与安全性** 里点 **仍要打开**。也可以在终端里清除隔离标记：

```sh
xattr -dr com.apple.quarantine /Applications/Perch.app
```

**Windows**，x64 或 ARM64：解压 `perch-<版本>-windows-<架构>.zip`，运行 `Perch.exe`。它没有签名，SmartScreen 可能会先提示，点 **更多信息 → 仍要运行**。

**Linux**，x86_64 或 arm64，需要 GTK 3：

- Debian 或 Ubuntu：`sudo apt install ./perch_<版本>_<架构>.deb`
- Fedora 或 openSUSE：`sudo dnf install ./perch-<版本>-1.<架构>.rpm`
- 任意发行版，不需要 root：把 `install.sh` 和 `perch-<版本>-linux-<架构>.tar.gz` 放在一起，运行 `sh install.sh`。它会把 Perch 装到 `~/.local`，并加入应用菜单。`sh install.sh --uninstall` 可以卸载。

Perch 在 macOS 上开发和测试。Windows 和 Linux 版本能编译、测试也能通过，但还没有在这两个系统上实际用过，欢迎反馈。

## 审阅改动

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/screenshots/review-dark.png">
    <img src=".github/screenshots/review-light.png" width="880" alt="Git 页列出一个改动的文件并显示它的差异">
  </picture>
</p>

- **从上次读到的地方接着读。** Git 页列出上次审阅以来改过的文件，只显示你还没读过的部分。改名的文件算作一个改动。标为已审的文件会一直保持已审，直到它再次被修改。
- **和任何版本对比。** 可以在上次审阅、未提交的改动和其他分支之间切换。
- **查看历史。** 浏览提交历史，打开任意一次提交看改动。文件树会标出改过的文件。每个文件都有自己的历史，鼠标停在某一行上，会显示这一行最后是谁改的。
- **编辑器里标出未读的行。** 读文件时，还没审阅的行会在行号旁标出来，并随着写入实时更新。
- **不用切过去也知道。** 你在读别的内容时，如果 agent 改出了待审的文件，Git 按钮上会出现一个低调的圆点。文件审完后圆点会消失。

审阅记录以 Git 树的形式保存在 `refs/perch/` 下，不占用工作区，提交、变基和重启之后都还在。

## 阅读和定位

- 语法高亮、渲染 Markdown（包括 README 里的 HTML），以及 PNG、JPEG、GIF、WebP 和 SVG 预览。
- 快速打开（`⌘P`，也支持 `文件:行号`）、带最近查询的项目搜索、查找替换。
- 通过语言服务：跳转定义、查找引用、当前文件或整个项目的符号、鼠标所指名字的类型和文档，以及路径栏里的符号路径。
- 每个项目重新打开时恢复标签页、光标、滚动位置和布局，并在各自的窗口中打开。
- 支持英文和简体中文，浅色和深色外观。

## 轻量编辑

Perch 可以编辑文本，支持撤销和查找替换，也可以在文件树里新建、重命名、移到废纸篓。重命名后，打开的标签页会跟着更新，未保存的修改也会保留。你编辑文件时如果 agent 改写了它，Perch 会询问保留哪个版本，不会覆盖任何一方。

## Perch 不做什么

Perch 有意保持小巧：没有插件系统、终端、AI 聊天、调试器和重构工具。它也不和 agent 对接：它只负责把改动显示出来，读什么由你决定。

## 快捷键

下表列出默认快捷键。在 **设置 → 快捷键** 中点选要修改的快捷键，可以更换组合键、清除绑定或恢复默认。

在 Windows 和 Linux 上，`⌘` 对应 `Ctrl`，`⌥` 对应 `Alt`，`⌃` 对应 `Ctrl`。

| 操作 | 快捷键 |
| --- | --- |
| 打开文件，或跳到 `文件:行号` | `⌘P` |
| 跳转到行 | `⌃G` |
| 在文件中查找 | `⌘F` |
| 查找替换 | `⌥⌘F` |
| 搜索项目 | `⇧⌘F` |
| 跳到文件中的符号 | `⇧⌘O` |
| 跳到项目中的符号 | `⌘T` |
| 跳转定义 | `F12` 或按住 `⌘` 点击 |
| 查找引用 | `⇧F12` |
| 后退 / 前进 | `⌃-` / `⌃⇧-` |
| 下一个 / 上一个标签页 | `⌃Tab` 或 `⇧⌘]` / `⌃⇧Tab` 或 `⇧⌘[` |
| 第 1 到 8 个标签页 / 最后一个 | `⌘1` 到 `⌘8` / `⌘9` |
| 切换侧栏 / 在文件树中定位 | `⌘B` / `⇧⌘E` |
| 增大 / 减小 / 重置字号 | `⌘=` / `⌘-` / `⌘0` |
| 复制位置引用 | `⇧⌘C` |
| 切换 Markdown 预览 | `⇧⌘V` |
| 显示改动 | `⇧⌘R` |
| 标为已审 | `⌘↩` |
| 下一处 / 上一处改动 | `⌥F5` / `⌥⇧F5` |
| 保存 / 关闭 / 恢复标签页 | `⌘S` / `⌘W` / `⇧⌘T` |
| 新建窗口 / 打开文件夹 | `⇧⌘N` / `⌘O` |
| 设置 | `⌘,` |

## 语言服务

Perch 内置支持 22 种语言，也可以使用任何支持 Language Server Protocol 的服务。打开 **设置 → 语言服务**，可以查看哪些服务已安装、开关某种语言，以及复制安装命令。

要添加其他语言，在该页面选择 **打开配置文件**，加入一项：

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

`command` 必须是 `PATH` 中的程序名或绝对路径。Perch 只从你自己的设置目录读取这个文件，不会运行打开的项目指定的命令。

## 从源码构建

需要 Go 1.27.4 或更高版本，审阅功能需要 Git。

```sh
git clone https://github.com/marukolibro/Perch.git
cd Perch
go tool mygo build
open build/darwin-arm64/Perch.app
```

运行 `go test ./...` 和 `golangci-lint run` 做检查。用 `-platform` 构建其他系统的版本，例如 `go tool mygo build -platform windows/amd64,linux/amd64`。

## 许可证

[MIT](LICENSE)

文件图标来自 Elior "Mallowigi" Boukhobza 的 [Atom Material Icons](https://github.com/AtomMaterialUI/iconGenerator)，使用 [MIT 许可证](internal/fileicons/assets/LICENSE)。
