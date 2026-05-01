//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"log"
)

func main() {
	switch os.Args[1] {
	case "run":
		parent()
	case "child":
		child()
	default:
		log.Fatalf("Unknown command")
	}
}

func parent() {
	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Println("parent error", err)
		os.Exit(1)
	}
}

func child() {
	must(os.MkdirAll("rootfs/oldrootfs", 0700)) 
  must(syscall.Mount("rootfs", "rootfs", "", syscall.MS_BIND, ""))
  must(syscall.PivotRoot("rootfs", "rootfs/oldrootfs"))
	must(os.Chdir("/"))
	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Println("child error", err)
		os.Exit(1)
	}
}

func must(err error) {
	if err != nil {
        log.Fatalf("Fatal error. Details: %v", err)
    }
}