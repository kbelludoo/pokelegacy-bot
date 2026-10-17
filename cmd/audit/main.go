package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"bot-template-go/pkg/ghsync"
	"bot-template-go/pkg/scan"
	"bot-template-go/pkg/stealth"
)

var repos = []string{
	"huntera-bot", "bot-template-go", "sigma-bot", "tibidle-bot",
	"leveling-bot", "pokeidle-go", "pokeidleworld-bot",
	"twitch-miner-go", "kick-miner-go",
}

var reposWithBaiak = append(append([]string{}, repos...), "baiak-idle-bot")

var extRe = regexp.MustCompile(`(?i)\.(html|json|js|md|txt|css)$`)

func fetchURL(url string) (string, int, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(data), resp.StatusCode, nil
}

func uniqueLimit(items []string, limit int) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if seen[it] {
			continue
		}
		seen[it] = true
		out = append(out, it)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func cmdAnon(deep bool) {
	issues := 0
	if !deep {
		fmt.Println("=== MODE ANON SCAN ===")
	} else {
		fmt.Println("=== MODE ANON DEEP SCAN ===")
	}
	for _, r := range repos {
		files, err := ghsync.List(r, "docs")
		if err != nil {
			fmt.Printf("[!] %s: cannot list docs/ -> %v\n", r, err)
			continue
		}
		for _, f := range files {
			if !extRe.MatchString(f) {
				continue
			}
			raw, err := ghsync.Content(r, "docs/"+f)
			if err != nil {
				fmt.Printf("[!] %s/%s: cannot read\n", r, f)
				continue
			}
			content := string(raw)
			var findings []scan.Finding
			if !deep {
				findings = scan.Scan("docs/"+f, content, []scan.Pattern{
					{Name: "ip-private-loopback", RE: regexp.MustCompile(`127\.0\.0\.1|localhost`)},
					{Name: "ip-vps", RE: scan.ReVPSIP},
					{Name: "ip-rfc1918", RE: regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b`)},
					{Name: "secret-keywords", RE: regexp.MustCompile(`(?i)\b(?:oauth[_-]?token|client_secret|refresh_token|password|passwd|apikey|api_key|ssh-rsa|BEGIN OPENSSH)\b`)},
					{Name: "real-username", RE: regexp.MustCompile(`(?i)\bkbell\b|\bbelludoo\b`)},
					{Name: "ssh-key-path", RE: regexp.MustCompile(`(?i)ssh-key-2026`)},
				})
			} else {
				findings = scan.Scan("docs/"+f, content, []scan.Pattern{
					{Name: "email", RE: scan.ReEmail},
					{Name: "public-ip", RE: scan.ReIPv4, Keep: scan.AllowedIP},
					{Name: "token-like", RE: regexp.MustCompile(`\b(?:[A-Za-z0-9_\-]{20,}\.){2}[A-Za-z0-9_\-]{20,}\b`)},
					{Name: "bearer", RE: regexp.MustCompile(`\bBearer\s+[A-Za-z0-9._\-]{15,}`)},
					{Name: "oauth-uri", RE: regexp.MustCompile(`(?i)twitch\.tv/(?:oauth|login)[^\s"']*`)},
					{Name: "client-id", RE: regexp.MustCompile(`(?i)\bclient[_-]?id['":\s]+['"]?[A-Za-z0-9]{20,}`)},
					{Name: "private-key", RE: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
				})
			}
			for _, fd := range findings {
				issues++
				fmt.Printf("X [%s] docs/%s -> %s: %s\n", r, f, fd.Pattern, strings.Join(fd.Matches, ", "))
			}
		}
	}
	if issues == 0 {
		if deep {
			fmt.Println("ALL CLEAN (deep scan)")
		} else {
			fmt.Println("ALL CLEAN")
		}
	} else {
		fmt.Printf("TOTAL FINDINGS: %d\n", issues)
	}
}

func cmdVerify() {
	fmt.Println("====================================================")
	fmt.Println("       VERIFICAÇÃO DE MODO ANÔNIMO GLOBAL          ")
	fmt.Println("====================================================")

	sha, err := ghsync.FileSHA("huntera-bot", "docs/index.html")
	if err == nil && sha != "" {
		raw, err := ghsync.Content("huntera-bot", "docs/index.html")
		if err == nil {
			if strings.Contains(string(raw), "129.153.157.17") {
				fmt.Println("[-] Encontrado IP em huntera-bot/docs/index.html. Limpando...")
				clean := strings.ReplaceAll(string(raw), "129.153.157.17", "127.0.0.1")
				if err := ghsync.Put("huntera-bot", "docs/index.html", "security: sanitize raw VPS IP from index template", []byte(clean), sha); err == nil {
					fmt.Println("✓ [huntera-bot] IP removido com sucesso de docs/index.html!")
				}
			} else {
				fmt.Println("✓ [huntera-bot] docs/index.html já está 100% limpo de IP.")
			}
		}
	} else {
		fmt.Println("[!] huntera-bot index check: não encontrado ou erro")
	}

	statusRepos := []string{"bot-template-go", "sigma-bot", "tibidle-bot", "leveling-bot", "pokeidle-go", "pokeidleworld-bot", "twitch-miner-go"}
	reURL := regexp.MustCompile(`https?://[^\s",]+`)
	for _, repo := range statusRepos {
		raw, err := ghsync.Content(repo, "docs/status.json")
		if err != nil {
			fmt.Printf("[?] [%s] docs/status.json não encontrado ou erro: %v\n", repo, err)
			continue
		}
		noURLs := reURL.ReplaceAllString(string(raw), "")
		hasNick := stealth.NickRE.MatchString(noURLs)
		hasEmail := stealth.EmailRE.MatchString(noURLs)
		hasIP := regexp.MustCompile(`129\.153|141\.148|193\.122`).MatchString(noURLs)
		if !hasNick && !hasEmail && !hasIP {
			fmt.Printf("✓ [%s] docs/status.json: 100%% SEGURO E ANÔNIMO\n", repo)
		} else {
			fmt.Printf("❌ [%s] docs/status.json EXPOSTO: nick=%v, email=%v, ip=%v\n", repo, hasNick, hasEmail, hasIP)
		}
	}
}

func cmdLive() {
	leaks := 0
	fmt.Println("---- live pages ----")
	for _, u := range repos {
		body, code, err := fetchURL(fmt.Sprintf("https://kbelludoo.github.io/%s/", u))
		if err != nil {
			fmt.Printf("%s => ERRO %v\n", u, err)
			continue
		}
		fmt.Printf("%s => HTTP %d (size %d)\n", u, code, len(body))
	}
	fmt.Println("---- status.json endpoints ----")
	for _, u := range repos {
		body, code, err := fetchURL(fmt.Sprintf("https://kbelludoo.github.io/%s/status.json", u))
		if err != nil {
			fmt.Printf("%s/status.json => ERRO %v\n", u, err)
			continue
		}
		fmt.Printf("%s/status.json => HTTP %d (size %d)\n", u, code, len(body))
	}
	fmt.Println("---- scanning live pages for leaks ----")
	for _, u := range repos {
		for _, path := range []string{"/", "/status.json"} {
			url := fmt.Sprintf("https://kbelludoo.github.io/%s%s", u, path)
			body, _, err := fetchURL(url)
			if err != nil {
				continue
			}
			hits := []string{}
			for _, fd := range scan.Scan(url, body, []scan.Pattern{
				{Name: "secrets", RE: scan.ReSecrets},
			}) {
				hits = append(hits, fd.Matches...)
			}
			hits = uniqueLimit(hits, 5)
			ips := []string{}
			for _, fd := range scan.Scan(url, body, []scan.Pattern{
				{Name: "ips", RE: scan.ReIPv4, Keep: func(m string) bool {
					return !scan.AllowedIP(m) && !strings.HasPrefix(m, "172.") && m != "1.1.1.1" && m != "8.8.8.8"
				}},
			}) {
				ips = append(ips, fd.Matches...)
			}
			ips = uniqueLimit(ips, 5)
			if len(hits) > 0 {
				fmt.Printf("X %s => %s\n", url, strings.Join(hits, " "))
				leaks++
			}
			if len(ips) > 0 {
				fmt.Printf("X %s => IP: %s\n", url, strings.Join(ips, " "))
				leaks++
			}
		}
	}
	if leaks == 0 {
		fmt.Println("LIVE PAGES CLEAN")
	}
}

func cmdDocs(history bool) {
	fmt.Println("=== md/docs files scan across all repos ===")
	for _, r := range reposWithBaiak {
		files, err := ghsync.List(r, "docs")
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f, ".md") && !strings.HasSuffix(f, ".txt") {
				continue
			}
			raw, err := ghsync.Content(r, "docs/"+f)
			if err != nil {
				continue
			}
			content := string(raw)
			findings := scan.Scan(r+"/docs/"+f, content, []scan.Pattern{
				{Name: "secrets", RE: scan.ReSecrets},
				{Name: "token", RE: scan.ReToken},
				{Name: "public-ip", RE: scan.ReIPv4, Keep: scan.AllowedIP},
			})
			for _, fd := range findings {
				fmt.Printf("X %s => %s: %s\n", fd.Source, fd.Pattern, strings.Join(fd.Matches, " "))
			}
		}
	}
	fmt.Println("MD SCAN DONE")
	fmt.Println()
	fmt.Println("=== README.md at repo root scan ===")
	for _, r := range reposWithBaiak {
		raw, err := ghsync.Content(r, "README.md")
		if err != nil {
			continue
		}
		content := string(raw)
		findings := scan.Scan(r+"/README.md", content, []scan.Pattern{
			{Name: "secrets", RE: scan.ReSecrets},
			{Name: "token", RE: scan.ReToken},
			{Name: "public-ip", RE: scan.ReIPv4, Keep: scan.AllowedIP},
		})
		for _, fd := range findings {
			fmt.Printf("X %s => %s: %s\n", fd.Source, fd.Pattern, strings.Join(fd.Matches, " "))
		}
	}
	fmt.Println("README SCAN DONE")

	if history {
		fmt.Println()
		fmt.Println("=== search full git history for token prefix in kick-miner-go ===")
		cmdTokenHistory("kick-miner-go")
	}
}

func cmdTokenHistory(repo string) {
	out, err := ghsync.GH(30*time.Second, "api", fmt.Sprintf("repos/kbelludoo/%s/commits?per_page=100", repo), "--jq", ".[].sha")
	if err != nil {
		fmt.Printf("[!] %s: cannot list commits\n", repo)
		return
	}
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		files, err := ghsync.GH(30*time.Second, "api", fmt.Sprintf("repos/kbelludoo/%s/git/trees/%s?recursive=1", repo, s), "--jq", ".tree[].path")
		if err != nil {
			continue
		}
		for _, f := range strings.Split(string(files), "\n") {
			f = strings.TrimSpace(f)
			if f == "" || f == "docs/index.html" {
				continue
			}
			raw, err := ghsync.ContentAt(repo, f, s)
			if err != nil {
				continue
			}
			if strings.Contains(string(raw), "442007154|KXEhfh") {
				fmt.Printf("HISTORY LEAK: commit %s file %s\n", s, f)
				found = true
			}
		}
	}
	if !found {
		fmt.Println("history clean outside docs/index.html")
	}
	fmt.Println("DONE")
}

func cmdNames() {
	fmt.Println("=== names found in each live status.json ===")
	for _, u := range repos {
		body, _, err := fetchURL(fmt.Sprintf("https://kbelludoo.github.io/%s/status.json", u))
		if err != nil {
			continue
		}
		fmt.Printf("-- %s\n", u)
		findings := scan.Scan(u, body, []scan.Pattern{
			{Name: "names", RE: scan.ReNickField, Limit: 25},
		})
		for _, fd := range findings {
			for _, m := range fd.Matches {
				fmt.Println(m)
			}
		}
	}
	fmt.Println()
	fmt.Println("=== leveling-bot full names check ===")
	body, _, err := fetchURL("https://kbelludoo.github.io/leveling-bot/status.json")
	if err == nil {
		findings := scan.Scan("leveling", body, []scan.Pattern{
			{Name: "keys", RE: scan.ReAnyKey, Limit: 30},
		})
		for _, fd := range findings {
			for _, m := range fd.Matches {
				fmt.Println(m)
			}
		}
	}
}

func cmdHub() {
	body, _, err := fetchURL("https://kbelludoo.github.io/bot-template-go/status.json")
	if err != nil {
		fmt.Printf("[!] cannot fetch hub status: %v\n", err)
		return
	}
	fmt.Println("--- hub status: token-like fields ---")
	findings := scan.Scan("hub", body, []scan.Pattern{
		{Name: "fields", RE: regexp.MustCompile(`"(bearerToken|token|password|username)"\s*:\s*"[^"]{0,40}`)},
	})
	for _, fd := range findings {
		for _, m := range fd.Matches {
			fmt.Println(m)
		}
	}
	fmt.Println("--- hub page bot card titles ---")
	page, _, err := fetchURL("https://kbelludoo.github.io/bot-template-go/")
	if err == nil {
		cardFindings := scan.Scan("hub-page", page, []scan.Pattern{
			{Name: "bot-name", RE: scan.ReBotCard},
		})
		for _, fd := range cardFindings {
			for _, m := range fd.Matches {
				fmt.Println(m)
			}
		}
		fmt.Println("--- hub page: dashboardUrl links ---")
		linkFindings := scan.Scan("hub-page", page, []scan.Pattern{
			{Name: "dash-url", RE: scan.ReDashURL},
		})
		for _, fd := range linkFindings {
			for _, m := range uniqueLimit(fd.Matches, 30) {
				fmt.Println(m)
			}
		}
	}
}

func cmdPorts() {
	fmt.Println("=== kick-miner :3009 endpoints ===")
	for _, p := range []string{"/", "/api/status", "/api/config", "/api/accounts", "/status", "/config"} {
		out, _, err := fetchURL("http://127.0.0.1:3009" + p)
		if err != nil {
			fmt.Printf("3009%s => ERRO %v\n", p, err)
			continue
		}
		if strings.Contains(out, "KXEhfh") {
			fmt.Printf("TOKEN LEAK on 3009%s\n", p)
		}
		if len(out) > 150 {
			out = out[:150]
		}
		fmt.Printf("3009%s => %s\n", p, out)
	}
	fmt.Println()
	fmt.Println("=== twitch-miner :3010 endpoints ===")
	reSensitive := regexp.MustCompile(`(?i)KXEhfh|oauth|bearer|token`)
	for _, p := range []string{"/", "/api/status", "/api/config", "/status"} {
		out, _, err := fetchURL("http://127.0.0.1:3010" + p)
		if err != nil {
			fmt.Printf("3010%s => ERRO %v\n", p, err)
			continue
		}
		if reSensitive.MatchString(out) {
			fmt.Printf("SENSITIVE on 3010%s\n", p)
		}
		if len(out) > 150 {
			out = out[:150]
		}
		fmt.Printf("3010%s => %s\n", p, out)
	}
	fmt.Println()
	fmt.Println("=== is 3009/3010 reachable from public IP? ===")
	pubip, _, err := fetchURL("https://ifconfig.me")
	if err != nil {
		fmt.Printf("public ip test: ERRO %v\n", err)
		return
	}
	pubip = strings.TrimSpace(pubip)
	fmt.Printf("public ip test: %s\n", pubip)
	for _, port := range []string{"3009", "3010", "3007"} {
		conn, err := net.DialTimeout("tcp", pubip+":"+port, 5*time.Second)
		if err != nil {
			fmt.Printf("port %s closed/filtered\n", port)
		} else {
			conn.Close()
			fmt.Printf("port %s OPEN to internet\n", port)
		}
	}
}

func cmdTokens() {
	fmt.Println("=== search all repo docs for real token prefix 442007154|KXEhfh ===")
	for _, r := range repos {
		files, err := ghsync.List(r, "docs")
		if err != nil {
			continue
		}
		for _, f := range files {
			raw, err := ghsync.Content(r, "docs/"+f)
			if err != nil {
				continue
			}
			if strings.Contains(string(raw), "442007154|KXEhfh") {
				fmt.Printf("LEAK: %s/docs/%s\n", r, f)
			}
		}
	}
	fmt.Println("SEARCH DONE")
	fmt.Println()
	fmt.Println("=== search full git history for token prefix in kick-miner-go ===")
	cmdTokenHistory("kick-miner-go")
}

func cmdReadme() {
	fmt.Println("=== current README.md of kick-miner-go: token lines ===")
	raw, err := ghsync.Content("kick-miner-go", "README.md")
	if err != nil {
		fmt.Println("[!] cannot read README")
	} else {
		findings := scan.Scan("README", string(raw), []scan.Pattern{
			{Name: "token", RE: scan.ReTokenFull},
			{Name: "token-prefix", RE: scan.ReToken},
			{Name: "bearerToken", RE: regexp.MustCompile(`bearerToken|KXEhfh`)},
		})
		if len(findings) == 0 {
			fmt.Println("current README clean")
		}
		for _, fd := range findings {
			for _, m := range fd.Matches {
				fmt.Println(m)
			}
		}
	}
	fmt.Println()
	fmt.Println("=== commits touching README.md ===")
	out, err := ghsync.GH(30*time.Second, "api", "repos/kbelludoo/kick-miner-go/commits?path=README.md&per_page=20", "--jq", `.[] | .sha[0:12] + " " + .commit.message`)
	if err == nil {
		fmt.Print(string(out))
	}
	fmt.Println()
	fmt.Println("=== docs/index.html token line (current) ===")
	idx, err := ghsync.Content("kick-miner-go", "docs/index.html")
	if err == nil {
		findings := scan.Scan("index", string(idx), []scan.Pattern{
			{Name: "token", RE: scan.ReToken},
		})
		for _, fd := range findings {
			for _, m := range fd.Matches {
				fmt.Printf("token_part_length=%d\n", len(m))
			}
		}
	}
	fmt.Println("DONE")
}

func cmdPage(repo string) {
	fmt.Printf("=== fonte da página %s ===\n", repo)
	body, err := ghsync.Content(repo, "docs/index.html")
	if err != nil {
		fmt.Printf("[!] cannot read docs/index.html: %v\n", err)
		return
	}
	fmt.Printf("size %d\n", len(body))
	content := string(body)
	fmt.Println("== fetches ==")
	for _, fd := range scan.Scan("page", content, []scan.Pattern{
		{Name: "fetch", RE: scan.ReFetchCall, Limit: 30},
	}) {
		for _, m := range fd.Matches {
			fmt.Println(m)
		}
	}
	fmt.Println("== urls ==")
	seen := map[string]bool{}
	for _, fd := range scan.Scan("page", content, []scan.Pattern{
		{Name: "url", RE: scan.ReAnyURL},
	}) {
		for _, m := range fd.Matches {
			if !seen[m] {
				seen[m] = true
				fmt.Println(m)
			}
			if len(seen) >= 30 {
				break
			}
		}
	}
	fmt.Println("== names leak check ==")
	findings := scan.Scan("page", content, []scan.Pattern{
		{Name: "nick", RE: stealth.NickRE, Limit: 10},
	})
	for _, fd := range findings {
		for _, m := range fd.Matches {
			fmt.Println(m)
		}
	}
}

func cmdDump() {
	fmt.Println("=== baiak-idle-bot docs ===")
	files, err := ghsync.List("baiak-idle-bot", "docs")
	if err == nil {
		for _, f := range files {
			fmt.Println(f)
		}
	}
	for _, f := range []string{"index.html", "status.json"} {
		_, code, err := fetchURL("https://kbelludoo.github.io/baiak-idle-bot/" + f)
		if err != nil {
			fmt.Printf("baiak %s => ERRO %v\n", f, err)
			continue
		}
		fmt.Printf("baiak %s => HTTP %d\n", f, code)
	}
	fmt.Println("=== ALL 9 pages: quick re-scan for VPS IP / key paths ===")
	for _, u := range reposWithBaiak {
		body, _, err := fetchURL("https://kbelludoo.github.io/" + u + "/")
		if err != nil {
			continue
		}
		findings := scan.Scan(u, body, []scan.Pattern{
			{Name: "vps-ip", RE: scan.ReVPSIP},
			{Name: "ssh-key", RE: regexp.MustCompile(`(?i)ssh-key-2026|BEGIN OPENSSH`)},
		})
		for _, fd := range findings {
			fmt.Printf("X %s => %s\n", u, strings.Join(fd.Matches, " "))
		}
	}
	fmt.Println("SCAN DONE")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("uso: audit <anon|verify|live|docs|names|hub|ports|tokens|readme|page|dump> [flags]")
		fmt.Println("  anon [-deep]  - varredura de vazamentos em docs/ dos repos")
		fmt.Println("  verify        - verifica e corrige modo anônimo global")
		fmt.Println("  live          - baixa páginas publicadas e varre por leaks")
		fmt.Println("  docs [-history] - scan md/docs + README + histórico opcional")
		fmt.Println("  names         - extrai campos de identidade dos status.json live")
		fmt.Println("  hub           - auditoria específica do hub bot-template-go")
		fmt.Println("  ports         - testa endpoints locais 3009/3010")
		fmt.Println("  tokens        - procura token real em docs e histórico")
		fmt.Println("  readme        - auditoria do README kick-miner-go")
		fmt.Println("  page <repo>   - analisa fonte da página docs/index.html")
		fmt.Println("  dump          - dump rápido das páginas + scan VPS IP")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "anon":
		deep := false
		for _, a := range os.Args[2:] {
			if a == "-deep" {
				deep = true
			}
		}
		cmdAnon(deep)
	case "verify":
		cmdVerify()
	case "live":
		cmdLive()
	case "docs":
		history := false
		for _, a := range os.Args[2:] {
			if a == "-history" {
				history = true
			}
		}
		cmdDocs(history)
	case "names":
		cmdNames()
	case "hub":
		cmdHub()
	case "ports":
		cmdPorts()
	case "tokens":
		cmdTokens()
	case "readme":
		cmdReadme()
	case "page":
		if len(os.Args) < 3 {
			fmt.Println("uso: audit page <repo>")
			os.Exit(1)
		}
		cmdPage(os.Args[2])
	case "dump":
		cmdDump()
	default:
		fmt.Printf("comando desconhecido: %s\n", os.Args[1])
		os.Exit(1)
	}
}
