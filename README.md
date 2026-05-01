# GOcker

A minimalist container implementation written in Go to demonstrate Linux process isolation techniques.

## Features
- **Process Isolation** (PID namespace)
- **Hostname Isolation** (UTS namespace)
- **Filesystem Isolation** (Mount namespace via `pivot_root`)

## Requirements
- **Operating System:** Linux (native or WSL2)
- **Privileges:** Root/sudo access
- **Environment:** A `rootfs` directory containing a Linux distribution must be present in the project root.

## Usage
**Bash:**
```
go build -o gocker main.go
sudo ./gocker run /bin/sh
