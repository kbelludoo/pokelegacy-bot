package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reTokenFull = regexp.MustCompile(`442007154\|[A-Za-z0-9._-]*`)
	reTokenID   = regexp.MustCompile(`442007154`)
	reVPSIP     = regexp.MustCompile(`129\.153\.157\.17`)
	reLeak      = regexp.MustCompile(`442007154|KXEhfh|129\.153\.157\.17`)
	rePlace     = regexp.MustCompile(`SEU_USER_ID|SEU_IP_DA_VPS`)
)

var fixRepos = []string{"kick-miner-go", "sigma-bot", "leveling-bot"}

const workDir = "/tmp/history-fix"

func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runGit(dir string, args ...string) (string, error) {
	return runIn(dir, "git", args...)
}

func fixFileContent(content string) string {
	content = reTokenFull.ReplaceAllString(content, "SEU_USER_ID|SEU_TOKEN_AQUI")
	content = reTokenID.ReplaceAllString(content, "SEU_USER_ID")
	content = reVPSIP.ReplaceAllString(content, "SEU_IP_DA_VPS")
	return content
}

func walkFix(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		if strings.HasPrefix(rel, ".git/") || rel == ".git" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !reTokenID.Match(data) && !reVPSIP.Match(data) {
			return nil
		}
		fixed := fixFileContent(string(data))
		if fixed != string(data) {
			os.WriteFile(path, []byte(fixed), info.Mode())
		}
		return nil
	})
}

func cmdFix() {
	os.Setenv("FILTER_BRANCH_SQUELCH_WARNING", "1")
	for _, repo := range fixRepos {
		fmt.Printf("===== %s =====\n", repo)
		dir := filepath.Join(workDir, repo)
		os.RemoveAll(dir)
		cloneURL := fmt.Sprintf("https://github.com/kbelludoo/%s.git", repo)
		if _, err := runIn("", "git", "clone", "-q", cloneURL, dir); err != nil {
			fmt.Printf("[!] clone failed: %v\n", err)
			continue
		}
		if out, err := runGit(dir, "log", "--oneline", "-1"); err == nil {
			fmt.Print(out)
		}
		if err := walkFix(dir); err != nil {
			fmt.Printf("[!] walk fix failed: %v\n", err)
			continue
		}
		runGit(dir, "add", "-A")
		runGit(dir, "commit", "-m", "security: scrub token and VPS IP from history", "--allow-empty")
		fmt.Println("-- verifying every commit in rewritten history...")
		dirty := false
		out, err := runGit(dir, "rev-list", "--all")
		if err == nil {
			for _, c := range strings.Fields(out) {
				grepOut, grepErr := runGit(dir, "grep", "-a", "-q", "-E", "442007154|KXEhfh|129\\.153\\.157\\.17", c, "--", ".")
				_ = grepOut
				if grepErr == nil {
					fmt.Printf("STILL DIRTY in %s\n", c)
					dirty = true
				}
			}
		}
		if !dirty {
			fmt.Println("HISTORY CLEAN")
		}
		fmt.Println("-- placeholder check (should exist in current HEAD):")
		if out, err := runGit(dir, "grep", "-n", "-E", "SEU_TOKEN_AQUI|SEU_IP_DA_VPS", "HEAD", "--", "."); err == nil {
			fmt.Print(out)
		}
		if _, err := runGit(dir, "push", "-q", "--force", "origin", "refs/heads/*"); err == nil {
			fmt.Println("-- force pushed")
		} else {
			fmt.Printf("[!] force push failed: %v\n", err)
		}
	}
	fmt.Println("ALL DONE")
}

func cmdVerify() {
	for _, r := range fixRepos {
		fmt.Printf("===== %s =====\n", r)
		dir := filepath.Join(workDir, r)
		if out, err := runGit(dir, "log", "--oneline", "-2"); err == nil {
			fmt.Println("-- last commits:")
			fmt.Print(out)
		}
		if out, err := runGit(dir, "rev-parse", "HEAD"); err == nil {
			fmt.Println("-- local HEAD:  " + strings.TrimSpace(out))
		}
		if out, err := runGit(dir, "ls-remote", "origin", "refs/heads/main"); err == nil {
			fields := strings.Fields(out)
			if len(fields) > 0 {
				fmt.Println("-- remote main: " + fields[0])
			}
		}
		fmt.Println("-- leak scan HEAD:")
		if out, err := runGit(dir, "grep", "-n", "-E", "442007154|KXEhfh|129\\.153\\.157\\.17", "HEAD", "--", "."); err != nil {
			fmt.Println("  (no leaks in HEAD)")
		} else {
			fmt.Print(out)
		}
		fmt.Println("-- placeholder present:")
		if out, err := runGit(dir, "grep", "-n", "-E", "SEU_USER_ID|SEU_IP_DA_VPS", "HEAD", "--", "."); err == nil {
			fmt.Print(out)
		}
		fmt.Println()
	}
	fmt.Println("===== FULL HISTORY RE-SCAN (all 3 repos) =====")
	for _, r := range fixRepos {
		dir := filepath.Join(workDir, r)
		dirty := false
		out, err := runGit(dir, "rev-list", "--all")
		if err != nil {
			continue
		}
		for _, c := range strings.Fields(out) {
			if _, grepErr := runGit(dir, "grep", "-a", "-q", "-E", "442007154|KXEhfh|129\\.153\\.157\\.17", c, "--", "."); grepErr == nil {
				fmt.Printf("X %s commit %s still dirty\n", r, c)
				dirty = true
			}
		}
		if !dirty {
			fmt.Printf("OK %s: all commits clean\n", r)
		}
	}
}

func cmdCheck() {
	for _, r := range fixRepos {
		fmt.Printf("===== %s =====\n", r)
		dir := filepath.Join(workDir, r)
		fmt.Println("-- refs/original (old pre-filter refs):")
		if out, err := runGit(dir, "for-each-ref", "--format=%(refname)", "refs/original/"); err == nil {
			lines := strings.Split(strings.TrimSpace(out), "\n")
			for i, l := range lines {
				if i >= 5 {
					break
				}
				fmt.Println(l)
			}
		}
		fmt.Println("-- fetch remote fresh:")
		runGit(dir, "fetch", "-q", "origin", "+refs/heads/*:refs/remotes/origin/*")
		if out, err := runGit(dir, "rev-parse", "HEAD"); err == nil {
			fmt.Println("local HEAD:     " + strings.TrimSpace(out))
		}
		if out, err := runGit(dir, "rev-parse", "origin/main"); err == nil {
			fmt.Println("remote main now: " + strings.TrimSpace(out))
		}
		fmt.Println("-- leak scan on remote main:")
		if _, err := runGit(dir, "grep", "-a", "-q", "-E", "442007154|KXEhfh|129\\.153\\.157\\.17", "origin/main", "--", "."); err != nil {
			fmt.Println("  remote main CLEAN")
		} else {
			fmt.Println("  DIRTY on remote:")
			if out, err := runGit(dir, "grep", "-n", "-E", "442007154|KXEhfh|129\\.153\\.157\\.17", "origin/main", "--", "."); err == nil {
				lines := strings.Split(out, "\n")
				for i, l := range lines {
					if i >= 5 {
						break
					}
					fmt.Println(l)
				}
			}
		}
		fmt.Println("-- is remote main descendant of local HEAD?")
		if _, err := runGit(dir, "merge-base", "--is-ancestor", "HEAD", "origin/main"); err == nil {
			fmt.Println("  yes (telemetry commits on top)")
		} else {
			fmt.Println("  NO - remote diverged")
		}
		fmt.Println()
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("uso: fix-history <fix|verify|check>")
		fmt.Println("  fix     - clona, scrub de token/IP no histórico e force push")
		fmt.Println("  verify  - confere HEAD e histórico completo dos 3 repos")
		fmt.Println("  check   - confere remote main vs local")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "fix":
		cmdFix()
	case "verify":
		cmdVerify()
	case "check":
		cmdCheck()
	default:
		fmt.Printf("comando desconhecido: %s\n", os.Args[1])
		os.Exit(1)
	}
}
