# GOcker

A minimalist container implementation written in Go to demonstrate Linux process isolation techniques.

## Features

Containers are not true "virtual machines"; they are simply isolated Linux processes. GOcker achieves this isolation by utilizing core Linux kernel APIs to provide a fully restricted environment:

- **Process Isolation (PID Namespace):** Process trees are completely decoupled. The container only sees its internal processes, beginning with its own PID 1.
- **Hostname Isolation (UTS Namespace):** The container receives an independent hostname, identifying itself uniquely (e.g., `gocker-container`).
- **Filesystem Isolation (`pivot_root` & Mount Namespace):** Safely swaps the root directory for the running process, jailing it securely in a specific folder.
- **Network Isolation (NET Namespace):** The container is entirely disconnected from the host's network stack, receiving only an isolated loopback interface.
- **Resource Limiting (Cgroups v2):** Restricts resource usage to prevent host starvation. The application is capped at a maximum of 10 simultaneous processes and ~100MB of RAM.
- **Filesystem Security:** The base `rootfs` is explicitly mounted as **read-only** to prevent malicious modifications. It securely provisions fully writable in-memory `tmpfs` directories for `/tmp` and mounts standard OCI `/dev` nodes.

## Requirements
- **Operating System:** Linux (native or WSL2)
- **Privileges:** Root (sudo)
- **Environment:** A `rootfs` directory containing a Linux distribution must be present in the project root.

### Preparing the `rootfs`
Before running GOcker, you need a base Linux filesystem. Alpine Linux is heavily recommended for its small footprint.

**Option 1: Using Docker**
```bash
mkdir -p rootfs
docker export $(docker create alpine) | tar -C rootfs -xf -
```

**Option 2: Without Docker**
```bash
mkdir rootfs
# Dynamically fetch the latest stable Alpine minirootfs
LATEST=$(curl -s https://dl-cdn.alpinelinux.org/alpine/latest-stable/releases/x86_64/ | grep -oE 'alpine-minirootfs-[0-9]+\.[0-9]+\.[0-9]+-x86_64\.tar\.gz' | head -1)
wget "https://dl-cdn.alpinelinux.org/alpine/latest-stable/releases/x86_64/$LATEST"
tar -xvf "$LATEST" -C rootfs
```

## Usage

Build the project to generate the binary:
```bash
go build -o gocker gocker.go
```

### Examples

**1. Run a container shell:**
Enter the interactive shell (requires the `rootfs` directory in the project's root folder):
```bash
sudo ./gocker run /bin/sh
```

**2. Test Process Isolation (PID Namespace):**
While inside the container, execute `ps aux`. You will only see your own shell and the `ps` command, and the shell's PID will be `1`.
```bash
ps aux
```

**3. Test Network Isolation (NET Namespace):**
Try listing the network interfaces from within the container. The system hides the host's interfaces (e.g., `eth0` or `wlan0`), showing only the completely isolated loopback interface (`lo`).
```bash
ip a
```

**4. Test Filesystem Security (Read-Only Rootfs):**
Your `rootfs` is mounted as read-only. Any attempt to write a malicious file will be rejected by the kernel, but writing to the `/tmp` RAM-disk works perfectly.
```bash
# Inside the container
touch /hacked.txt
# > touch: /hacked.txt: Read-only file system

touch /tmp/test.txt
# > (success - written to RAM disk)
```

**5. Test Resource Limiting (Cgroups v2 PIDs limit):**
The container is restricted to a maximum of 10 processes. A dangerous "fork-bomb" attack, which could freeze the host machine if executed natively, is completely harmless here and merely hits your configured limit:
```bash
# sh fork-bomb - totally safe for the host
f(){ f|f& };f
# > sh: can't fork / Resource temporarily unavailable
```

## Limitations
While GOcker implements the core foundations of a container, it lacks several advanced features commonly found in complete engines (like Docker):
*   **OverlayFS:** Currently, we bind the system as read-only. A real engine uses an overlay union filesystem so users can seemingly write to root, storing those diffs dynamically in an "*upper layer*".
*   **Virtual Networking (Veth):** GOcker create a Network namespace but do not configure a *veth* pair bridging the container to the host to grant it internet access.
*   **Security Profiles:** GOcker users, but do not drop default Linux Capabilities nor apply an explicit Seccomp profile to reject dangerous syscalls.
*   **User Namespaces (Rootless):** Currently, the application requires root privileges (sudo) to run and map namespaces.