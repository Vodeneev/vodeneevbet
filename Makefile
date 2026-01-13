.PHONY: help deploy-parsers deploy-core deploy-all status logs

help:
	@echo "VodeneevBet Deployment Makefile"
	@echo ""
	@echo "Available commands:"
	@echo "  make deploy-parsers      - Deploy parser service to vm-parsers"
	@echo "  make deploy-core         - Deploy calculator to vm-core-services"
	@echo "  make deploy-all          - Deploy all services to both VMs"
	@echo "  make status              - Check status of all services"
	@echo "  make logs-parser         - View parser logs"
	@echo "  make logs-calculator     - View calculator logs"
	@echo "  make stop-all            - Stop all services"
	@echo "  make start-all           - Start all services"
	@echo ""

deploy-parsers:
	@bash scripts/deploy/deploy-parsers.sh

deploy-core:
	@bash scripts/deploy/deploy-core-services.sh

deploy-all:
	@bash scripts/deploy/deploy-all.sh

status:
	@echo "=== Parser Service Status ==="
	@ssh vm-parsers "sudo docker ps --filter name=vodeneevbet-parser --format 'table {{.Names}}\t{{.Status}}'" || echo "Failed to get status"
	@echo ""
	@echo "=== Calculator Service Status ==="
	@ssh vm-core-services "sudo docker ps --filter name=vodeneevbet-calculator --format 'table {{.Names}}\t{{.Status}}'" || echo "Failed to get status"

logs-parser:
	@ssh vm-parsers "sudo docker logs -f vodeneevbet-parser"

logs-calculator:
	@ssh vm-core-services "sudo docker logs -f vodeneevbet-calculator"

stop-all:
	@echo "Stopping all services..."
	@ssh vm-parsers "sudo docker rm -f vodeneevbet-parser >/dev/null 2>&1 || true" || true
	@ssh vm-core-services "sudo docker rm -f vodeneevbet-calculator >/dev/null 2>&1 || true" || true
	@echo "All services stopped"

start-all:
	@echo "Starting all services..."
	@bash scripts/deploy/deploy-all.sh
	@echo "All services started"
