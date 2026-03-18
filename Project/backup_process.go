package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"sanntid/project/config"
)

func heartbeatPort(id string) string {
	n, err := strconv.Atoi(id)
	if err != nil {
		panic(fmt.Sprintf("elevator id must be a number, got %q", id))
	}
	return fmt.Sprintf(":%d", config.HeartbeatBasePort+n)
}

// listenForPrimary blocks until no heartbeat is received for 3 seconds,
// at which point this process becomes primary.
func listenForPrimary(id string) {
	addr, err := net.ResolveUDPAddr("udp", heartbeatPort(id))
	if err != nil {
		panic(fmt.Sprintf("[listenForPrimary] ResolveUDPAddr: %v", err))
	}
	var conn *net.UDPConn
	for {
		conn, err = net.ListenUDP("udp", addr)
		if err == nil {
			break
		}
		fmt.Println("[listenForPrimary] port busy, retrying...")
		time.Sleep(200 * time.Millisecond)
	}
	defer conn.Close()
	fmt.Println("[listenForPrimary] listening for primary heartbeat...")

	buf := make([]byte, 128)
	for {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("[listenForPrimary] no heartbeat, becoming primary")
			return
		}
		if string(buf[:n]) == id {
			continue
		}
		fmt.Printf("[listenForPrimary] heartbeat from %q, waiting...\n", string(buf[:n]))
	}
}

// startBackup spawns a new terminal window running this same executable as backup.
func startBackup(id, port string) {
	exe, _ := os.Executable()
	fmt.Printf("[startBackup] spawning backup: %s --id=%s --port=%s\n", exe, id, port)
	args := `'` + exe + `' --id=` + id + ` --port=` + port
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("osascript", "-e", `tell app "Terminal" to do script "`+args+`"`)
	} else {
		cmd = exec.Command("gnome-terminal", "--", "bash", "-c", args+"; read")
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("[startBackup] could not open terminal window:", err)
	}
}

// sendHeartbeat broadcasts this process's ID over UDP so the backup knows a primary is alive.
func sendHeartbeat(id string) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1"+heartbeatPort(id))
	if err != nil {
		panic(fmt.Sprintf("[sendHeartbeat] ResolveUDPAddr: %v", err))
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		panic(fmt.Sprintf("[sendHeartbeat] DialUDP: %v", err))
	}
	defer conn.Close()
	for {
		conn.Write([]byte(id))
		time.Sleep(100 * time.Millisecond)
	}
}
