.PHONY: help deploy-parsers deploy-core deploy-all test-connections status logs

help:
	@echo "VodeneevBet Deployment Makefile"
	@echo ""
	@echo "Available commands:"
	@echo "  make deploy-parsers      - Deploy parser service to vm-parsers"
	@echo "  make deploy-core         - Deploy calculator and API to vm-core-services"
	@echo "  make deploy-all          - Deploy all services to both VMs"
	@echo "  make test-connections    - Test SSH connections to VMs"
	@echo "  make status              - Check status of all services"
	@echo "  make logs-parser         - View parser logs"
	@echo "  make logs-calculator     - View calculator logs"
	@echo "  make logs-api            - View API logs"
	@echo "  make stop-all            - Stop all services"
	@echo "  make start-all           - Start all services"
	@echo ""

deploy-parsers:
	@bash scripts/deploy/deploy-parsers.sh

deploy-core:
	@bash scripts/deploy/deploy-core-services.sh

deploy-all:
	@bash scripts/deploy/deploy-all.sh

test-connections:
	@powershell -ExecutionPolicy Bypass -File scripts/test-connections.ps1

status:
	@echo "=== Parser Service Status ==="
	@ssh vm-parsers "sudo systemctl status vodeneevbet-parser --no-pager -l | head -15" || echo "Failed to get status"
	@echo ""
	@echo "=== Calculator Service Status ==="
	@ssh vm-core-services "sudo systemctl status vodeneevbet-calculator --no-pager -l | head -15" || echo "Failed to get status"
	@echo ""
	@echo "=== API Service Status ==="
	@ssh vm-core-services "sudo systemctl status vodeneevbet-api --no-pager -l | head -15" || echo "Failed to get status"

logs-parser:
	@ssh vm-parsers "sudo journalctl -u vodeneevbet-parser -f"

logs-calculator:
	@ssh vm-core-services "sudo journalctl -u vodeneevbet-calculator -f"

logs-api:
	@ssh vm-core-services "sudo journalctl -u vodeneevbet-api -f"

stop-all:
	@echo "Stopping all services..."
	@ssh vm-parsers "sudo systemctl stop vodeneevbet-parser" || true
	@ssh vm-core-services "sudo systemctl stop vodeneevbet-calculator vodeneevbet-api" || true
	@echo "All services stopped"

start-all:
	@echo "Starting all services..."
	@ssh vm-parsers "sudo systemctl start vodeneevbet-parser" || true
	@ssh vm-core-services "sudo systemctl start vodeneevbet-calculator vodeneevbet-api" || true
	@echo "All services started"
