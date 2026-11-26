package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		reportError(err, len(os.Args) == 1)
		os.Exit(1)
	}
}

// run 按参数选择桌面模式、网页服务或终端分队。
func run(args []string) error {
	if len(args) == 0 {
		return runDesktop()
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "cli":
		return runCLI(args[1:])
	case "help", "-h", "--help":
		fmt.Println(usageText())
		return nil
	default:
		return fmt.Errorf("未知子命令 %q\n\n%s", args[0], usageText())
	}
}

// usageText 集中维护命令行帮助文案。
func usageText() string {
	return strings.TrimSpace(`
DOTA 内战局：按「位置优先、分数其次」枚举最均衡的两队。

用法:
  dota-shuffle                                  启动桌面模式
  dota-shuffle serve [-addr :8080] [-no-browser]
  dota-shuffle cli -f players.json [-seed 123] [-json]
  dota-shuffle help

分队历史默认保存在程序同目录的 dota-shuffle.db（SQLite），目录不可写时使用用户配置目录。
`)
}

// runServe 处理 “dota-shuffle serve [-addr :8080] [-no-browser]”。
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "HTTP 监听地址")
	noBrowser := fs.Bool("no-browser", false, "仅启动服务，不自动打开浏览器")
	dbPath := fs.String("db", DefaultDBPath(), "SQLite 历史数据库路径")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "用法: dota-shuffle serve [-addr :8080] [-no-browser] [-db path]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, err := OpenStore(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	rawURL := browserURL(*addr)
	fmt.Printf("DOTA 内战局服务已启动: %s\n", rawURL)
	fmt.Printf("历史数据库: %s\n", *dbPath)
	if *noBrowser {
		fmt.Println("已禁用自动打开浏览器，请手动访问上面的地址。")
	} else {
		fmt.Println("正在打开默认浏览器，如未弹出请手动访问上面的地址。")
		go waitAndOpenBrowser(rawURL)
	}
	return serve(*addr, store)
}

// browserURL 把监听地址换算成本机访问地址，供自动打开浏览器使用。
func browserURL(addr string) string {
	port := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		port = addr[i+1:]
	}
	return "http://localhost:" + port + "/"
}

// waitAndOpenBrowser 先轮询健康检查，待服务真正就绪后再唤起浏览器，
// 避免浏览器抢在服务启动前打开而展示错误页。
func waitAndOpenBrowser(rawURL string) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(rawURL + "api/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if err := openBrowser(rawURL); err != nil {
		fmt.Fprintf(os.Stderr, "提示: 自动打开浏览器失败: %v，请手动访问 %s\n", err, rawURL)
	}
}

// runCLI 处理一次性分队：读取阵容文件，输出文本或 JSON 结果。
func runCLI(args []string) error {
	fs := flag.NewFlagSet("cli", flag.ContinueOnError)
	file := fs.String("f", "players.json", "阵容 JSON 文件路径")
	seed := fs.Int64("seed", 0, "随机种子；>0 时在等价最优解中随机摇，同种子结果可复现，0 为确定性最优")
	asJSON := fs.Bool("json", false, "以 JSON 输出结果，便于程序对接")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "用法: dota-shuffle cli -f players.json [-seed 123] [-json]")
		fmt.Fprintln(fs.Output(), "根据玩家的位置与分数（段位兜底），枚举选出最均衡的两队。")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	roster, err := LoadRoster(*file)
	if err != nil {
		return err
	}
	res, err := Balance(roster.Players, *seed)
	if err != nil {
		return err
	}

	if *asJSON {
		return RenderJSON(os.Stdout, res)
	}
	RenderText(os.Stdout, res, *seed)
	return nil
}
