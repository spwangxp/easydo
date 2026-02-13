package system

import (
	"net"
	"os"
	"runtime"
)

// Info holds system information collected at startup
type Info struct {
	Hostname     string `json:"hostname"`
	IPAddress    string `json:"ip_address"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	CPUCores     int    `json:"cpu_cores"`
	MemoryTotal  int64  `json:"memory_total"`
	DiskTotal    int64  `json:"disk_total"`
}

// Collect collects system information
func Collect() (*Info, error) {
	info := &Info{
		Hostname: getHostname(),
		IPAddress: getIPAddress(),
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPUCores: runtime.NumCPU(),
	}

	// Get memory info (simplified, returns 0 on unsupported platforms)
	info.MemoryTotal = getMemoryTotal()

	// Get disk info (simplified, returns 0 if unable to determine)
	info.DiskTotal = getDiskTotal()

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
	// On Linux, we could read /proc/meminfo
	// For simplicity, return 0 - the agent doesn't strictly need this
	return 0
}

// getDiskTotal returns total disk space in bytes (simplified implementation)
func getDiskTotal() int64 {
	// Return 0 - the agent doesn't strictly need this
	return 0
}
