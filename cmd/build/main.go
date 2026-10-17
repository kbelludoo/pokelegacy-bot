package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	target := "linux"
	if len(os.Args) > 1 {
		target = os.Args[1]
	}

	var goos string
	switch target {
	case "linux":
		goos = "linux"
	case "windows":
		goos = "windows"
	default:
		fmt.Printf("alvo desconhecido: %s (use linux ou windows)\n", target)
		os.Exit(1)
	}

	fmt.Println("============================================================")
	fmt.Printf("  COMPILANDO BOT GO PARA %s (AMD64 / VPS)\n", goos)
	fmt.Println("============================================================")

	outName := "bot-app-linux"
	if goos == "windows" {
		outName = "bot-app-windows.exe"
	}

	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", outName, ".")
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()

	if err == nil {
		fmt.Println()
		fmt.Println("✔ Sucesso! Binario gerado:", outName)
		if goos == "linux" {
			fmt.Println("Envie para a VPS via SCP:")
			fmt.Println("  scp", outName, "usuario@ip:/caminho/do/bot/")
		}
	} else {
		fmt.Println()
		fmt.Println("✖ Erro durante a compilacao.")
		os.Exit(1)
	}
}
