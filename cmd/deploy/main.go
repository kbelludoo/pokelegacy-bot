package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCapture(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func ts() string {
	return time.Now().Format("20060102-150405")
}

func curlGet(url string) string {
	out, _ := runCapture("curl", "-s", "-m", "10", url)
	return out
}

func leakAudit(url string) {
	body := curlGet(url)
	lower := strings.ToLower(body)
	if strings.Contains(lower, "kbelludoo") || strings.Contains(lower, "lohmoreira") {
		fmt.Println("VAZAMENTO AINDA PRESENTE")
	} else {
		fmt.Println("LIMPO")
	}
}

func cmdHunteraGo() {
	stamp := ts()
	run("cp", "/home/ubuntu/huntera-go/huntera-bot", "/home/ubuntu/huntera-go/huntera-bot.bak-"+stamp)
	fmt.Println("backup: huntera-bot.bak-" + stamp)
	run("tmux", "kill-session", "-t", "huntera-go")
	time.Sleep(1 * time.Second)
	run("pkill", "-x", "huntera-bot")
	time.Sleep(2 * time.Second)
	run("pkill", "-9", "-x", "huntera-bot")
	time.Sleep(1 * time.Second)
	run("cp", "/tmp/huntera-bot-stealth", "/home/ubuntu/huntera-go/huntera-bot")
	run("chmod", "+x", "/home/ubuntu/huntera-go/huntera-bot")
	run("bash", "/home/ubuntu/huntera-go/start.sh")
	time.Sleep(8 * time.Second)
	fmt.Println("== teste API local ==")
	out := curlGet("http://127.0.0.1:3000/api/status")
	fmt.Println(out)
	fmt.Println("== teste via túnel público ==")
	out = curlGet("https://ron-flexible-respective-caring.trycloudflare.com/api/status")
	fmt.Println(out)
	fmt.Println("== leak audit via túnel ==")
	leakAudit("https://ron-flexible-respective-caring.trycloudflare.com/api/status")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("uso: deploy <huntera-go>")
		fmt.Println("  huntera-go  - aplica binário stealth e reinicia via start.sh")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "huntera-go":
		cmdHunteraGo()
	default:
		fmt.Printf("comando desconhecido: %s\n", os.Args[1])
		os.Exit(1)
	}
}
