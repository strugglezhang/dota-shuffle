# DOTA 内战局

## macOS

双击 `DOTA 内战局.app` 打开。这个通用包支持 macOS 13 及更新版本，包含 Intel 和 Apple Silicon 架构。界面和素材随应用提供，使用 macOS 自带的 WebKit 和 SQLite。

从源码构建需 Go 1.26 或更新版本及 Apple Command Line Tools。

```sh
sh build-macos.sh
```

战史保存在 `~/Library/Application Support/dota-shuffle/dota-shuffle.db`。本机测试版默认使用临时签名。对外分发时可通过 `DOTA_MAC_SIGN_IDENTITY` 选择 Developer ID 证书，并按 Apple 的流程公证。

## Windows

双击 `dota-shuffle.exe` 打开。发行文件使用 Windows GUI 子系统，自带 WebView2 固定版本，首次启动会自动释放到当前用户的缓存目录。

从源码构建时准备 Go 1.26 或更新版本、Python 3、`zip` 和微软的 [WebView2 Fixed Version x64](https://developer.microsoft.com/microsoft-edge/webview2/#download-section)。先解包微软的 CAB，再把含 `msedgewebview2.exe` 的目录传给构建脚本。

```sh
sh build-windows.sh /path/to/expanded-webview2-x64
```

新安装把战史写入 `%APPDATA%\dota-shuffle\dota-shuffle.db`。若 EXE 同目录已有可用的旧版战史库，桌面版会继续使用它。

## 分队与 BO3

每位玩家可以勾选多个位置。分队先最小化位置冲突，再最小化双方有效总分差。分队结果会显示每位首发的实际位置。

在替补行点「换上首发」，选择要换下的首发并确认。被换下的玩家转入替补。BO3 进行中，替补加入被换人的队伍，从下一局起生效，已录比分保留。

BO3 逐局录入战果，先赢两局的一方获胜。战史可以恢复阵容、比分和换人记录，结束后可以沿用报名名单为下一场 BO3 重新分队。
