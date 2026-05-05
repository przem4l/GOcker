//go:build linux

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func main() {
	if os.Getuid() != 0 {
    	log.Fatal("Must be run as root")
	}
	if len(os.Args) < 2 {
		log.Fatal("Usage: gocker run <hostname> <command> [args...]")
	}
	switch os.Args[1] {
	case "run":
		if len(os.Args) < 4 {
			log.Fatal("Usage: gocker run <hostname> <command> <args>")
		}
		parent()
	case "child":
		child()
	default:
		log.Fatalf("Unknown command")
	}
}

func parent() {
	rootfs, err := filepath.Abs("rootfs")
	if err != nil {
		log.Fatalf("Failed to resolve rootfs path: %v", err)
	}
	if _, err := os.Stat(rootfs); err != nil {
		log.Fatalf("rootfs not found at %s: %v", rootfs, err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		log.Fatalf("Failed to create pipe: %v", err)
	}

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{r}
	cmd.Env = append(os.Environ(), "GOCKER_CHILD=1", "GOCKER_ROOTFS="+rootfs)

	if err := cmd.Start(); err != nil {
        fmt.Fprintf(os.Stderr, "Parent start error. Details: %v", err)
        os.Exit(1)
    }
	r.Close()
	// cgroups for children (limited to 10 processes + 100MB of RAM)
	cgroupPath := fmt.Sprintf("/sys/fs/cgroup/gocker-%d", cmd.Process.Pid) 
	if err := os.MkdirAll(cgroupPath, 0700); err != nil {
		log.Fatalf("Failed to create cgroup: %v", err)
	}
	if err := os.WriteFile(cgroupPath+"/memory.max", []byte("100000000"), 0700); err != nil {
		log.Fatalf("Failed to set memory limit: %v", err)
	}
	if err := os.WriteFile(cgroupPath+"/memory.swap.max", []byte("0"), 0700); err != nil {
		fmt.Printf("Warning: couldn't disable swap: %v\n", err)
	}
	if err := os.WriteFile(cgroupPath+"/pids.max", []byte("10"), 0700); err != nil {
		log.Fatalf("Failed to set pids limit: %v", err)
	}
	pid := fmt.Sprintf("%d", cmd.Process.Pid)
	if err := os.WriteFile(cgroupPath+"/cgroup.procs", []byte(pid), 0700); err != nil {
		log.Fatalf("Failed to add process to cgroup: %v", err)
	}

	// Signal child that cgroups are ready
	w.Close()

	defer func() {
		fmt.Printf("Cleaning up cgroups at %s... ", cgroupPath)
		if err := os.WriteFile(cgroupPath+"/cgroup.kill", []byte("1"), 0700); err != nil {
			fmt.Printf("Warning: failed to kill cgroup: %v\n", err)
		}

		if err := os.RemoveAll(cgroupPath); err != nil {
			fmt.Printf("Warning: %v (it may be busy)\n", err)
		} else {
			fmt.Println("Done.")
		}
	}()
	if err := cmd.Wait(); err != nil {
        fmt.Fprintf(os.Stderr, "Parent wait error. Details: %v", err)
    }
}

func makedev(major, minor uint32) int {
    maj, min := uint64(major), uint64(minor)
    return int(((maj & 0xfff) << 8) | ((maj &^ 0xfff) << 32) | (min & 0xff) | ((min &^ 0xff) << 12))
}

func child() {
	if os.Getenv("GOCKER_CHILD") == "" {
		log.Fatal("This command is for internal use only")
	}

	// Wait for parent to set up cgroups
	pipe := os.NewFile(3, "pipe")
	b := make([]byte, 1)
	if _, err := pipe.Read(b); err != nil && err != io.EOF {
		log.Fatalf("Failed to read sync pipe: %v", err)
	}
	pipe.Close()

	// Set loopback interface up in the new network namespace
	if err := exec.Command("ip", "link", "set", "lo", "up").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not bring up lo: %v\n", err)
	}

	syscall.Umask(0022)
	if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		log.Fatalf("Failed to make / private: %v", err)
	}
	rootfs := os.Getenv("GOCKER_ROOTFS")
	if rootfs == "" {
		log.Fatal("GOCKER_ROOTFS not set")
	}
	if err := syscall.Mount(rootfs, rootfs, "", syscall.MS_BIND, ""); err != nil {
		log.Fatalf("Failed to bind mount rootfs: %v", err)
	}
	if err := os.MkdirAll(rootfs+"/oldrootfs", 0700); err != nil {
		log.Fatalf("Failed to create oldrootfs directory: %v", err)
	}
	if err := syscall.PivotRoot(rootfs, rootfs+"/oldrootfs"); err != nil {
		log.Fatalf("Failed to pivot_root: %v", err)
	}
	if err := os.Chdir("/"); err != nil {
		log.Fatalf("Failed to chdir to /: %v", err)
	}
	if err := syscall.Mount("tmpfs", "/tmp", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, ""); err != nil {
		log.Fatalf("Failed to mount /tmp: %v", err)
	}
	if err := syscall.Mount("proc", "/proc", "proc", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_NOEXEC, ""); err != nil {
		log.Fatalf("Failed to mount /proc: %v", err)
	}
	defer func() {
		fmt.Println("Cleaning... Unmounting /proc...")
		syscall.Unmount("/proc", 0)
	}()
	if err := syscall.Mount("tmpfs", "/dev", "tmpfs", syscall.MS_NOSUID, "mode=755"); err != nil {
		log.Fatalf("Failed to mount /dev: %v", err)
	}
	defer func() {
		fmt.Println("Cleaning... Unmounting /dev...")
		syscall.Unmount("/dev", 0)
	}()

	oldUmask := syscall.Umask(0000)
	/* OCI - Standard Device Nodes */
	if err := syscall.Mknod("/dev/null", 0666|syscall.S_IFCHR, makedev(1, 3)); err != nil {
		log.Fatalf("Failed to create /dev/null: %v", err)
	}
	if err := syscall.Mknod("/dev/zero", 0666|syscall.S_IFCHR, makedev(1, 5)); err != nil {
		log.Fatalf("Failed to create /dev/zero: %v", err)
	}
	if err := syscall.Mknod("/dev/random", 0666|syscall.S_IFCHR, makedev(1, 8)); err != nil {
		log.Fatalf("Failed to create /dev/random: %v", err)
	}
	if err := syscall.Mknod("/dev/urandom", 0666|syscall.S_IFCHR, makedev(1, 9)); err != nil {
		log.Fatalf("Failed to create /dev/urandom: %v", err)
	}
	if err := syscall.Mknod("/dev/full", 0666|syscall.S_IFCHR, makedev(1, 7)); err != nil {
		log.Fatalf("Failed to create /dev/full: %v", err)
	}
	if err := syscall.Mknod("/dev/tty", 0666|syscall.S_IFCHR, makedev(5, 0)); err != nil {
		log.Fatalf("Failed to create /dev/tty: %v", err)
	}
	if err := syscall.Mknod("/dev/console", 0666|syscall.S_IFCHR, makedev(5, 1)); err != nil {
		log.Fatalf("Failed to create /dev/console: %v", err)
	}
	syscall.Umask(oldUmask)

	if err := syscall.Sethostname([]byte(os.Args[2])); err != nil {
		log.Fatalf("Failed to set hostname: %v", err)
	}

	if err := syscall.Unmount("/oldrootfs", syscall.MNT_DETACH); err != nil {
		log.Fatalf("Failed to unmount /oldrootfs: %v", err)
	}
	if err := syscall.Rmdir("/oldrootfs"); err != nil {
		log.Fatalf("Failed to remove /oldrootfs: %v", err)
	}
	if err := syscall.Mount("", "/", "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
		log.Fatalf("Failed to remount / as read-only: %v", err)
	}

	cmd := exec.Command(os.Args[3], os.Args[4:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Println("Process error in container:", err)
	}
}
