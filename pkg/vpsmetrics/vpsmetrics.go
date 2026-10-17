package vpsmetrics

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Metrics struct {
	Uptime         string `json:"uptime"`
	Load           string `json:"load"`
	RAMTotalMb     int    `json:"ramTotalMb"`
	RAMUsedMb      int    `json:"ramUsedMb"`
	RAMAvailableMb int    `json:"ramAvailableMb"`
	RAMPercent     int    `json:"ramPercent"`
	SwapTotalMb    int    `json:"swapTotalMb"`
	SwapUsedMb     int    `json:"swapUsedMb"`
}

func defaults() Metrics {
	return Metrics{
		Uptime:         "N/A",
		Load:           "N/A",
		RAMTotalMb:     1024,
		RAMUsedMb:      512,
		RAMAvailableMb: 512,
		RAMPercent:     50,
		SwapTotalMb:    2048,
		SwapUsedMb:     0,
	}
}

func Collect() Metrics {
	uptimeSec, err := readFirstFloat("/proc/uptime")
	if err != nil {
		return defaults()
	}
	load := "0, 0, 0"
	if parts, err := readFields("/proc/loadavg", 3); err == nil && len(parts) > 0 {
		if len(parts) > 3 {
			parts = parts[:3]
		}
		load = strings.Join(parts, ", ")
	}
	mem, err := readMeminfo()
	if err != nil {
		return defaults()
	}
	return Metrics{
		Uptime:         formatUptime(uptimeSec),
		Load:           load,
		RAMTotalMb:     mem.totalMb,
		RAMUsedMb:      mem.usedMb,
		RAMAvailableMb: mem.availMb,
		RAMPercent:     mem.percent,
		SwapTotalMb:    mem.swapTotalMb,
		SwapUsedMb:     mem.swapUsedMb,
	}
}

func formatUptime(sec float64) string {
	total := int(sec)
	days := total / 86400
	hours := (total % 86400) / 3600
	mins := (total % 3600) / 60
	switch {
	case days > 0:
		return fmt.Sprintf("up %d days, %d:%02d", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("up %d:%02d", hours, mins)
	default:
		return fmt.Sprintf("up %d min", mins)
	}
}

func readFirstFloat(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, fmt.Errorf("arquivo vazio: %s", path)
	}
	return strconv.ParseFloat(fields[0], 64)
}

func readFields(path string, n int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(data))[:min(n, len(strings.Fields(string(data))))], nil
}

type memInfo struct {
	totalMb     int
	usedMb      int
	availMb     int
	percent     int
	swapTotalMb int
	swapUsedMb  int
}

func readMeminfo() (memInfo, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return memInfo{}, err
	}
	kb := map[string]int64{}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		vals := strings.Fields(parts[1])
		if len(vals) == 0 {
			continue
		}
		n, err := strconv.ParseInt(vals[0], 10, 64)
		if err != nil {
			continue
		}
		kb[parts[0]] = n
	}
	total := kb["MemTotal"]
	if total <= 0 {
		return memInfo{}, fmt.Errorf("MemTotal ausente")
	}
	free := kb["MemFree"]
	buffers := kb["Buffers"]
	cached := kb["Cached"] + kb["SReclaimable"]
	avail := kb["MemAvailable"]
	if avail == 0 {
		avail = free
	}
	used := total - free - buffers - cached
	if used < 0 {
		used = 0
	}
	swapTotal := kb["SwapTotal"]
	swapFree := kb["SwapFree"]
	swapUsed := swapTotal - swapFree
	if swapUsed < 0 {
		swapUsed = 0
	}
	return memInfo{
		totalMb:     int(total / 1024),
		usedMb:      int(used / 1024),
		availMb:     int(avail / 1024),
		percent:     int((used*100 + total/2) / total),
		swapTotalMb: int(swapTotal / 1024),
		swapUsedMb:  int(swapUsed / 1024),
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
