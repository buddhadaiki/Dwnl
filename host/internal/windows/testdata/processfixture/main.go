// This program is a workspace-only fixture for real Windows job/process tests.
// It has no network, registry, installation, or user-data behavior.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"dwnl/host/internal/ports"
	owned "dwnl/host/internal/windows"
	win "golang.org/x/sys/windows"
)

func sleep() {
	for {
		time.Sleep(time.Second)
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(80)
	}
	exe, _ := os.Executable()
	switch args[0] {
	case "env":
		values := map[string]string{}
		for _, name := range args[1:] {
			values[name] = os.Getenv(name)
		}
		data, _ := json.Marshal(values)
		fmt.Println(string(data))
		return
	case "pwd":
		dir, err := os.Getwd()
		if err != nil {
			os.Exit(93)
		}
		fmt.Println(dir)
		return
	case "echo":
		data, _ := json.Marshal(args[1:])
		fmt.Println(string(data))
		fmt.Fprintln(os.Stderr, "fixture stderr")
		return
	case "line":
		count, _ := strconv.Atoi(args[1])
		fmt.Println(strings.Repeat("x", count))
		if count > 65536 {
			sleep()
		}
		return
	case "handle":
		value, _ := strconv.ParseUint(args[1], 10, 64)
		win.SetEvent(win.Handle(value))
		fmt.Println("unrelated handle absent")
		return
	case "host":
		runner, err := owned.NewRunner(args[1])
		if err != nil {
			os.Exit(91)
		}
		runner.Run(context.Background(), ports.ExecSpec{Executable: ports.YTDLP, Args: []string{"tree"}, Dir: args[1]}, func(line ports.ProcessLine) {
			if line.Stream == ports.Stdout {
				fmt.Println(line.Text)
			}
		})
		os.Exit(92)
	case "sleep":
		sleep()
	case "grandchild":
		fmt.Println(os.Getpid())
		sleep()
	case "child":
		cmd := exec.Command(exe, "grandchild")
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			os.Exit(81)
		}
		if err := cmd.Start(); err != nil {
			os.Exit(82)
		}
		var grandchild int
		if _, err := fmt.Fscanln(pipe, &grandchild); err != nil {
			os.Exit(83)
		}
		fmt.Printf("%d %d\n", os.Getpid(), grandchild)
		sleep()
	case "tree":
		cmd := exec.Command(exe, "child")
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			os.Exit(84)
		}
		if err := cmd.Start(); err != nil {
			os.Exit(85)
		}
		var child, grandchild int
		if _, err := fmt.Fscanln(pipe, &child, &grandchild); err != nil {
			os.Exit(86)
		}
		data, _ := json.Marshal([]int{os.Getpid(), child, grandchild})
		fmt.Println("tree:" + string(data))
		sleep()
	case "exit":
		code, _ := strconv.Atoi(args[1])
		os.Exit(code)
	case "breakaway":
		cmd := exec.Command(exe, "sleep")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: win.CREATE_BREAKAWAY_FROM_JOB}
		if err := cmd.Start(); err == nil {
			cmd.Process.Kill()
			cmd.Wait()
			os.Exit(87)
		}
		fmt.Println("breakaway rejected")
		return
	}
	os.Exit(89)
}
