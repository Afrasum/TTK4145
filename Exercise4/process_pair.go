package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"
)

func listen_for_primary() int {
	listen_addr, _ := net.ResolveUDPAddr("udp", ":30000")
	listen_conn, _ := net.ListenUDP("udp", listen_addr)

	defer func() {
		if err := listen_conn.Close(); err != nil {
			fmt.Println("Error closing connection:", err)
		}
	}()

	buffer := make([]byte, 1024)
	num := 0

	parseFails := 0

	for {
		err := listen_conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err != nil {
			return num
		}
		n, _, err := listen_conn.ReadFromUDP(buffer)
		if err != nil {
			return num
		}
		_, err = fmt.Sscanf(string(buffer[:n]), "%d", &num)
		if err != nil {
			parseFails++
			if parseFails > 5 {
				fmt.Println("Too many parse failures, exiting.")
				return num
			}
		} else {
			parseFails = 0
			fmt.Printf("Received: %d\n", num)
		}
	}
}

func start_backup() {
	fmt.Println("Starting backup process...")
	dir, _ := os.Getwd()

	cmd := exec.Command(
		"osascript",
		"-e",
		`tell app "Terminal" to do script "cd `+dir+`; ./exercise4"`,
	)
	err := cmd.Start()
	if err != nil {
		fmt.Println("Error starting backup process:", err)
	}
}

func primary_counter(start_num int) {
	fmt.Printf("Waiting 3 seconds before starting primary counter...\n")
	time.Sleep(3 * time.Second)

	fmt.Println("Starting primary_counter")

	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:30000")
	conn, _ := net.DialUDP("udp", nil, addr)
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Println("Error closing connection:", err)
		}
	}()

	counter := start_num

	for {
		msg := fmt.Sprintf("%d", counter)
		_, err := conn.Write([]byte(msg))
		if err != nil {
			fmt.Println("Error sending message:", err)
			time.Sleep(1 * time.Second)
			continue
		}
		fmt.Printf("Sent: %d\n", counter)
		counter++
		time.Sleep(1 * time.Second)
	}
}

func main() {
	start_num := listen_for_primary() + 1
	fmt.Println("Primary process died, taking over...")

	start_backup()

	fmt.Println("Backup process started")

	primary_counter(start_num)
}
