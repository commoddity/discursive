package tunnel

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// PIDsFromPSLines returns PIDs from ps output lines matching cloudflared and the local port.
func PIDsFromPSLines(lines []string, port uint16) []int {
	needle := fmt.Sprintf("127.0.0.1:%d", port)
	var pids []int
	for _, line := range lines {
		if !strings.Contains(line, "cloudflared") || !strings.Contains(line, needle) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// KillStaleUnix kills cloudflared processes targeting loopback:port.
func KillStaleUnix(port uint16) int {
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(string(out), "\n")
	pids := PIDsFromPSLines(lines, port)
	killed := 0
	for _, pid := range pids {
		if pid == os.Getpid() {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		_ = proc.Kill()
		killed++
	}
	return killed
}
