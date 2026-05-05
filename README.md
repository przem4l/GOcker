# GOcker

A minimalist container implementation written in Go to demonstrate Linux process isolation techniques.

## Features

Containers are not true "virtual machines"; they are simply isolated Linux processes. GOcker achieves this isolation by utilizing core Linux kernel APIs to provide a fully restricted environment:

- **Process Isolation (PID Namespace):** Process trees are completely decoupled. The container only sees its internal processes, beginning with its own PID 1.
- **Hostname Isolation (UTS Namespace):** The container receives an independent hostname, identifying itself uniquely (e.g., `gocker-container`).
- **Filesystem Isolation (`pivot_root` & Mount Namespace):** Safely swaps the root directory for the running process, jailing it securely in a specific folder.
- **Network Isolation (NET Namespace):** The container is entirely disconnected from the host's network stack, receiving only an isolated loopback interface.
- **User Isolation (User Namespace):** The container maps the host user/group IDs to root (UID/GID 0) inside the container, providing an additional layer of security boundary.
- **Resource Limiting & Scheduling (Cgroups v2):** Restricts resource usage to prevent host starvation. The application is capped at a maximum of 10 simultaneous processes and ~100MB of RAM (with swap memory fully disabled). Uses unique instance identifiers and pipe synchronization to ensure limits are rigidly applied before the container runs, avoiding race conditions and allowing concurrent container execution.
- **Filesystem Security:** The base `rootfs` is explicitly mounted as **read-only** to prevent malicious modifications. It securely provisions fully writable in-memory `tmpfs` directories with `MS_NOSUID` flags for `/tmp` and `/dev`, and dynamically constructs standard OCI device nodes.
- **Internal Security:** The internal `child` bootstrapper requires internal environment tokens, safely rejecting direct user invocations (e.g. users typing `./gocker child`).

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
Enter the interactive shell (requires the `rootfs` directory in the project's root folder). You must provide a hostname and a command:
```bash
sudo ./gocker run gocker-container /bin/sh
```

> **Note on Cleanup:** When the container terminates, GOcker attempts to remove its resource limits. If the system reports that the resource is "busy", it usually means a background process is still shutting down. In such cases, no action is required—GOcker will simply reuse the existing directory the next time it runs.

**2. Test Process Isolation (PID Namespace):**
While inside the container, execute `ps aux`. You will only see your own shell and the `ps` command, and the shell's PID will be `1`.
```bash
ps aux
```

**3. Test Network Isolation (NET Namespace):**
Try listing the network interfaces from within the container. The system hides the host's interfaces (e.g., `eth0` or `wlan0`), showing only the completely isolated, yet automatically activated, loopback interface (`lo`).
```bash
ip a

# You can now test it with ping:
ping -c 2 127.0.0.1
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
bomb() { bomb | bomb & }; bomb
# > sh: can't fork / Resource temporarily unavailable
```

**6. Test Resource Limiting (Cgroups v2 Memory & Swap limits):**
The container is restricted to ~100MB of RAM, and Swap memory is strictly disallowed. Use `dd` to forcefully write exceeding limits to the RAM disk (`/tmp` tmpfs). It will instantly invoke the host's OOM (Out Of Memory) Killer, terminating the process and cleanly saving the host.
```bash
dd if=/dev/zero of=/tmp/wypelnienie bs=1M count=150
# > Parent wait error. Details: signal: killed
```

## Limitations
While GOcker implements the core foundations of a container, it lacks several advanced features commonly found in complete engines (like Docker):
*   **OverlayFS:** Currently, we bind the system as read-only. A real engine uses an overlay union filesystem so users can seemingly write to root, storing those diffs dynamically in an "*upper layer*".
*   **Virtual Networking (Veth):** GOcker create a Network namespace but do not configure a *veth* pair bridging the container to the host to grant it internet access.
*   **Security Profiles:** GOcker users, but do not drop default Linux Capabilities nor apply an explicit Seccomp profile to reject dangerous syscalls.
*   **User Namespaces (Rootless):** Currently, the application requires root privileges (sudo) to run and map namespaces.