# Deployment Guide

The automatic deployment system ensures that the latest version of services is always running on virtual machines.

## Deployment Architecture

- **vm-parsers** (158.160.168.187): Runs Parser Service
- **vm-core-services** (158.160.222.217): Runs Calculator Service

## Quick Start

### Deploy All Services

**Linux/Mac (bash):**
```bash
make deploy-all
# or
./scripts/deploy/deploy-all.sh
```

### Environment Variables for Manual Deployment

The `scripts/deploy/deploy-*.sh` scripts expect you to specify where to pull images from:

```bash
export IMAGE_OWNER="vodeneev"   # namespace in GHCR (usually repository owner)
export IMAGE_TAG="main"         # or specific tag

# if images are private:
export GHCR_TOKEN="..."         # PAT with read:packages
export GHCR_USERNAME="vodeneev" # optional

# if you need to sync ./keys to VM (by default script doesn't touch keys):
export COPY_KEYS=1
```

### Deploy Individual Services

**Parser Service:**
```bash
make deploy-parsers
# or
./scripts/deploy/deploy-parsers.sh
```

**Core Services (Calculator):**
```bash
make deploy-core
# or
./scripts/deploy/deploy-core-services.sh
```

## What the Deployment Script Does

Current deployment scripts use **Docker Compose on VM** (without rsync of code and without building Go on server):

1. **Connection check** — SSH to VM
2. **Directory preparation** — `/opt/vodeneevbet/{parsers,core}`
3. **Upload `docker-compose.yml`** — from `deploy/vm-*/docker-compose.yml`
4. **Sync `configs/`** — placed next to compose (keys are not touched by default)
5. **Pull & up** — `docker compose pull && docker compose up -d`

## Service Management

### Check Status

```bash
make status
```

Or manually:
```bash
# Parser
ssh vm-parsers "sudo docker ps --filter name=vodeneevbet-parser"

# Calculator
ssh vm-core-services "sudo docker ps --filter name=vodeneevbet-calculator"
```

### View Logs

```bash
# Parser logs
make logs-parser
# or
ssh vm-parsers "sudo docker logs -f vodeneevbet-parser"

# Calculator logs
make logs-calculator
# or
ssh vm-core-services "sudo docker logs -f vodeneevbet-calculator"
```

### Stop/Start

```bash
# Stop all services
make stop-all

# Start all services
make start-all
```

### Avoid disk full on parser VM

The parser (especially Pinnacle888 with leagues flow) can produce a lot of logs and use `/tmp` (e.g. Chrome for mirror resolution). To avoid filling the disk:

1. **Docker log rotation** — `deploy/vm-parsers/docker-compose.yml` sets `logging.driver: json-file` with `max-size: 50m`, `max-file: 3` so container logs are rotated.
2. **Parser code** — Only one Pinnacle888 run executes at a time (mutex), and Chrome uses a single `/tmp/pinnacle888_chrome` dir that is cleaned before each run.

If the disk still fills, increase free space or reduce `parser.interval` in config so runs complete before the next tick.

## Requirements

### On Remote Machines Must Be Installed:

1. **Docker**
2. **Docker Compose** (plugin `docker compose` or `docker-compose`)
3. **SSH access** with sudo privileges for the deploy user

### Installing Docker on a New VM (Ubuntu 24.04)

If the VM is fresh and Docker is not installed, run on the server (user must have sudo):

```bash
# Add Docker's official GPG key and repo, then install
sudo apt-get update
sudo apt-get install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo usermod -aG docker "$USER"
# Log out and back in (or new SSH session) for group to apply; then verify:
docker --version && docker compose version
```

After that, the GitHub Actions deploy (or local `./scripts/deploy/deploy-parsers.sh`) can run `sudo docker compose` successfully.

### On Local Machine:

1. **SSH client** with configured access to VM
2. **rsync** (for syncing `configs/`)
3. **Make** (optional)

## CI/CD Deployment Automation

### GitHub Actions

Workflow `.github/workflows/deploy.yml`:

- builds `parser` and `calculator` images in GHCR
- deploys to two VMs via SSH using `docker compose`

**Required Secrets:**
- `SSH_PRIVATE_KEY` — private SSH key (without passphrase)
- `VM_PARSERS_HOST` — IP/DNS of vm-parsers
- `VM_CORE_HOST` — IP/DNS of vm-core-services

**Optional Secrets:**
- `VM_USER` — user on VM (if different)
- `GHCR_TOKEN` + `GHCR_USERNAME` — if images in GHCR are private (PAT with `read:packages`)

## Troubleshooting

### Problem: "Cannot connect to VM"

**Solution:**
1. Check SSH connection: `ssh vm-parsers`
2. Make sure SSH config is set up correctly
3. Check port 22 availability: `Test-NetConnection -ComputerName 158.160.168.187 -Port 22`

### Problem: "Permission denied"

**Solution:**
1. Make sure user `vodeneevm` has sudo privileges
2. Check directory permissions: `ssh vm-parsers "ls -la /opt/vodeneevbet"`

### Problem: "go: command not found"

**Solution:**
For compose deployment, Go is not needed on VM. Make sure Docker and Docker Compose are installed on VM.

### Problem: Service Won't Start

**Solution:**
1. Check logs: `ssh vm-parsers "sudo docker logs --tail=200 vodeneevbet-parser"`
2. Check configuration: `ssh vm-parsers "cat /opt/vodeneevbet/parsers/configs/production.yaml"`
3. Check directory: `ssh vm-parsers "ls -la /opt/vodeneevbet/parsers"`

## File Structure on VM

After deployment, the structure on VM looks like this:

```
/opt/vodeneevbet/
├── parsers/
│   ├── docker-compose.yml
│   ├── .env
│   ├── configs/
│   └── keys/   # usually manually (secrets)
└── core/
    ├── docker-compose.yml
    ├── .env
    ├── configs/
    └── keys/   # usually manually (secrets)
```

## Security

- SSH keys must be protected
- Service account keys and passwords must not be committed to git
- Use `.gitignore` to exclude sensitive files
- Regularly update dependencies: `go mod tidy`
