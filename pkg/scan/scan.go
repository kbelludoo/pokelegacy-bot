package scan

import (
	"regexp"
	"strconv"
	"strings"
)

type Pattern struct {
	Name  string
	RE    *regexp.Regexp
	Keep  func(string) bool
	Limit int
}

type Finding struct {
	Source  string
	Pattern string
	Matches []string
}

func Scan(source, content string, pats []Pattern) []Finding {
	var findings []Finding
	for _, p := range pats {
		var found []string
		seen := map[string]bool{}
		for _, m := range p.RE.FindAllString(content, -1) {
			if p.Keep != nil && !p.Keep(m) {
				continue
			}
			if seen[m] {
				continue
			}
			seen[m] = true
			if len(m) > 80 {
				m = m[:80] + "…"
			}
			found = append(found, m)
			if p.Limit > 0 && len(found) >= p.Limit {
				break
			}
		}
		if len(found) > 0 {
			findings = append(findings, Finding{Source: source, Pattern: p.Name, Matches: found})
		}
	}
	return findings
}

var (
	ReVPSIP       = regexp.MustCompile(`129\.153\.157\.17`)
	ReEmail       = regexp.MustCompile(`(?i)[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	ReIPv4        = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	ReSecrets     = regexp.MustCompile(`(?i)129\.153\.157\.17|ssh-key-2026|BEGIN OPENSSH|client_secret|refresh_token|password|passwd`)
	ReSecretsWide = regexp.MustCompile(`(?i)client_secret|refresh_token|password|passwd|ssh-rsa|BEGIN OPENSSH`)
	ReNick        = regexp.MustCompile(`(?i)kbelludoo[0-9]*|lohmoreira`)
	ReNickKbell   = regexp.MustCompile(`(?i)\bkbell[a-z0-9]*`)
	ReToken       = regexp.MustCompile(`442007154\|[A-Za-z0-9_.-]+`)
	ReTokenFull   = regexp.MustCompile(`442007154\|KXEhfh`)
	ReBearer      = regexp.MustCompile(`(?i)Bearer [A-Za-z0-9._-]{10,}`)
	ReIdentity    = regexp.MustCompile(`"(name|username|alias|charName|trainerName|token|bearerToken)"\s*:\s*"[^"]{0,50}"`)
	ReUserField   = regexp.MustCompile(`(?i)"(username|userId|login|displayName)"\s*:\s*"[^"]*"`)
	ReNickField   = regexp.MustCompile(`"(name|alias|charName|trainerName|username|nick|leader|partyLeader|members?)"\s*:\s*"[^"]*"`)
	RePlaceholder = regexp.MustCompile(`.{0,30}(SeuUsuario|kbelludoo|sua_conta|seuusuario).{0,30}`)
	ReAnyKey      = regexp.MustCompile(`"[A-Za-z0-9_ #]+"(?:\s*):`)
	ReBotCard     = regexp.MustCompile(`class="bot-name"[^>]*>[^<]*`)
	ReDashURL     = regexp.MustCompile(`https://kbelludoo\.github\.io/[a-z-]+/`)
	ReFetchCall   = regexp.MustCompile(`fetch\([^)]{0,120}`)
	ReAnyURL      = regexp.MustCompile(`https?://[a-zA-Z0-9./_?=&%:-]+`)
	ReTunnelURL   = regexp.MustCompile(`https://[a-zA-Z0-9.-]*\.trycloudflare\.com`)
	ReConnectedAs = regexp.MustCompile(`Conectado como [^)]*\)`)
)

func PublicIP(m string) bool {
	return PublicIPWith(m, nil)
}

func PublicIPWith(m string, allow map[string]bool) bool {
	parts := strings.Split(m, ".")
	if len(parts) != 4 {
		return false
	}
	nums := make([]int, 4)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n > 255 {
			return false
		}
		nums[i] = n
	}
	if allow[m] {
		return false
	}
	first := nums[0]
	if first == 127 || first == 10 || first == 0 {
		return false
	}
	if first == 192 && nums[1] == 168 {
		return false
	}
	if first == 172 && nums[1] >= 16 && nums[1] <= 31 {
		return false
	}
	return true
}

var defaultAllowedIPs = map[string]bool{
	"127.0.0.1":       true,
	"0.0.0.0":         true,
	"255.255.255.255": true,
	"1.1.1.1":         true,
	"8.8.8.8":         true,
}

func AllowedIP(m string) bool {
	return !PublicIPWith(m, defaultAllowedIPs)
}
