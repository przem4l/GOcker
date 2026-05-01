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

	if err := cmd.Start(); err != nil {
        fmt.Printf("Parent start error. Details: %v", err)
        os.Exit(1)
    }
	// cgroups for childs (limited to 10 processes)
	cgroupPath := "/sys/fs/cgroup/gocker" 
	must(os.MkdirAll(cgroupPath, 0700))
	must(os.WriteFile(cgroupPath+"/pids.max", []byte("10"), 0700))
	pid := fmt.Sprintf("%d", cmd.Process.Pid)
    must(os.WriteFile(cgroupPath+"/cgroup.procs", []byte(pid), 0700))
	defer os.Remove(cgroupPath)
	if err := cmd.Wait(); err != nil {
        fmt.Printf("Parent wait error. Details: %v", err)
        os.Exit(1)
    }
}

func child() {
	must(syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, "")) // setting main file system as private
	must(syscall.Mount("rootfs", "rootfs", "", syscall.MS_BIND, "")) // bind mount rootfs to itself so it can be pivoted
	must(os.MkdirAll("rootfs/oldrootfs", 0700))
	must(syscall.PivotRoot("rootfs", "rootfs/oldrootfs"))
	must(os.Chdir("/"))
	must(syscall.Mount("proc", "/proc", "proc", 0, "")) // mount the proc filesystem
	defer func() {
		fmt.Println("Cleaning... Unmounting /proc...")
		syscall.Unmount("/proc", 0)
	}()
	must(syscall.Mount("tmpfs", "/dev", "tmpfs", 0, "")) // mount a temporary filesystem for device nodes
	defer func() {
		fmt.Println("Cleaning... Unmounting /dev...")
		syscall.Unmount("/dev", 0)
	}()
	/* OCI - Standard Device Nodes */
	must(syscall.Mknod("/dev/null", 0666|syscall.S_IFCHR, (1<<8)|3)) // (deleting data)
	must(syscall.Mknod("/dev/zero", 0666|syscall.S_IFCHR, (1<<8)|5)) // (clearing storage)
	must(syscall.Mknod("/dev/random", 0666|syscall.S_IFCHR, (1<<8)|8)) // (encryption)
	must(syscall.Mknod("/dev/urandom", 0666|syscall.S_IFCHR, (1<<8)|9)) // (encryption)
	must(syscall.Mknod("/dev/full", 0666|syscall.S_IFCHR, (1<<8)|7)) // (testing error handling)
	must(syscall.Mknod("/dev/tty", 0666|syscall.S_IFCHR, 5<<8)) // (direct user contact)
	must(syscall.Mknod("/dev/console", 0666|syscall.S_IFCHR, (5<<8)|1)) // (main output and critical logging)

	must(syscall.Sethostname([]byte("gocker-container")))

	must(syscall.Unmount("/oldrootfs", syscall.MNT_DETACH))
	must(os.Remove("/oldrootfs"))

	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Println("Process error in container:", err)
	}
}

func must(err error) {
	if err != nil {
			log.Fatalf("Fatal error. Details: %v", err)
		}
}