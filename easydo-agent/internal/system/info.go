package system

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Info holds system information collected at startup
type Info struct {
	Hostname            string              `json:"hostname"`
	IPAddress           string              `json:"ip_address"`
	OS                  string              `json:"os"`
	Arch                string              `json:"arch"`
	CPUCores            int                 `json:"cpu_cores"`
	MemoryTotal         int64               `json:"memory_total"`
	DiskTotal           int64               `json:"disk_total"`
	BaseInfo            string              `json:"base_info"`
	BaseInfoCollectedAt int64               `json:"base_info_collected_at"`
	Runtime             RuntimeCapabilities `json:"runtime"`
}

// Collect collects system information
func Collect() (*Info, error) {
	info := &Info{
		Hostname:  getHostname(),
		IPAddress: getIPAddress(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		CPUCores:  runtime.NumCPU(),
	}

	baseInfo, memoryTotal, diskTotal := collectMachineBaseInfo(info)
	info.BaseInfo = baseInfo
	info.BaseInfoCollectedAt = time.Now().Unix()
	info.MemoryTotal = memoryTotal
	info.DiskTotal = diskTotal
	info.Runtime = ProbeRuntimeCapabilities()

	return info, nil
}

// getHostname returns the machine hostname
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// getIPAddress returns the primary IP address of the machine
func getIPAddress() string {
	// Try to get the IP address of the default route
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		// Check if it's a valid IP address
		ipNet, ok := addr.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}

	return ""
}

// getMemoryTotal returns total memory in bytes (simplified implementation)
func getMemoryTotal() int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return value * 1024
	}
	return 0
}

func getDiskTotal() int64 {
	output, err := exec.Command("sh", "-lc", `lsblk -b -dn -o SIZE 2>/dev/null | awk '{sum += $1} END {if (sum > 0) print sum}'`).Output()
	if err == nil {
		if value, parseErr := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64); parseErr == nil {
			return value
		}
	}
	if rootTotal := getRootDiskTotal(); rootTotal > 0 {
		return rootTotal
	}
	return 0
}

func getRootDiskTotal() int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0
	}
	return int64(stat.Blocks) * int64(stat.Bsize)
}

func getOSRelease() (string, string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS, ""
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		values[parts[0]] = strings.Trim(parts[1], `"`)
	}
	name := values["PRETTY_NAME"]
	if name == "" {
		name = values["NAME"]
	}
	return defaultString(name, runtime.GOOS), values["VERSION_ID"]
}

func getKernelVersion() string {
	output, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func getCPUModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if strings.TrimSpace(parts[0]) == "model name" {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func getGPUDevices() []map[string]interface{} {
	output, err := exec.Command("sh", "-lc", `if command -v nvidia-smi >/dev/null 2>&1; then nvidia-smi --query-gpu=index,name,memory.total --format=csv,noheader,nounits; fi`).Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	devices := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		index, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		memoryMB, _ := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		devices = append(devices, map[string]interface{}{
			"index":       index,
			"vendor":      "nvidia",
			"model":       strings.TrimSpace(parts[1]),
			"memoryBytes": memoryMB * 1024 * 1024,
		})
	}
	return devices
}

func collectMachineBaseInfo(info *Info) (string, int64, int64) {
	if info == nil {
		return "", 0, 0
	}
	osName, osVersion := getOSRelease()
	rootTotal := getRootDiskTotal()
	diskTotal := getDiskTotal()
	memoryTotal := getMemoryTotal()
	gpus := getGPUDevices()
	payload := map[string]interface{}{
		"schemaVersion": 1,
		"status":        "success",
		"source":        "agent_self",
		"collectedAt":   time.Now().Unix(),
		"machine": map[string]interface{}{
			"hostname":    info.Hostname,
			"primaryIpv4": info.IPAddress,
			"os": map[string]interface{}{
				"name":    osName,
				"version": osVersion,
			},
			"kernelVersion": getKernelVersion(),
			"arch":          info.Arch,
			"cpu": map[string]interface{}{
				"model":        getCPUModel(),
				"logicalCores": info.CPUCores,
			},
			"memory": map[string]interface{}{
				"totalBytes": memoryTotal,
			},
			"storage": map[string]interface{}{
				"rootTotalBytes": rootTotal,
				"totalDiskBytes": diskTotal,
			},
			"gpu": map[string]interface{}{
				"count":   len(gpus),
				"devices": gpus,
			},
		},
	}
	if memoryTotal == 0 || diskTotal == 0 {
		payload["status"] = "partial"
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", memoryTotal, diskTotal
	}
	return string(data), memoryTotal, diskTotal
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
