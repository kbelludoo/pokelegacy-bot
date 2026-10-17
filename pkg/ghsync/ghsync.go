package ghsync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const Owner = "kbelludoo"

func GH(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), msg)
	}
	return out.Bytes(), nil
}

func contentURL(repo, path string) string {
	return fmt.Sprintf("repos/%s/%s/contents/%s", Owner, repo, path)
}

func FileSHA(repo, path string) (string, error) {
	out, err := GH(20*time.Second, "api", contentURL(repo, path), "--jq", ".sha")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func Content(repo, path string) ([]byte, error) {
	out, err := GH(30*time.Second, "api", contentURL(repo, path), "--jq", ".content")
	if err != nil {
		return nil, err
	}
	return decodeBase64(string(out))
}

func ContentAt(repo, path, ref string) ([]byte, error) {
	out, err := GH(30*time.Second, "api", fmt.Sprintf("%s?ref=%s", contentURL(repo, path), ref), "--jq", ".content")
	if err != nil {
		return nil, err
	}
	return decodeBase64(string(out))
}

func List(repo, path string) ([]string, error) {
	out, err := GH(30*time.Second, "api", contentURL(repo, path), "--jq", ".[].name")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, `"`)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func Put(repo, path, message string, content []byte, sha string) error {
	payload := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
	}
	if sha != "" {
		payload["sha"] = sha
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "ghput-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_, err = GH(30*time.Second, "api", "-X", "PUT", contentURL(repo, path), "--input", name)
	return err
}

func API(timeout time.Duration, args ...string) ([]byte, error) {
	return GH(timeout, append([]string{"api"}, args...)...)
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	if s == "" {
		return nil, fmt.Errorf("conteudo base64 vazio")
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		raw, err2 := base64.RawStdEncoding.DecodeString(s)
		if err2 != nil {
			return nil, err
		}
		return raw, nil
	}
	return raw, nil
}
