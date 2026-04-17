# TalePanel — common operations from /opt/talepanel/deploy/panel
.DEFAULT_GOAL := help

help:
	@echo "up          Start the whole panel stack"
	@echo "down        Stop the stack (keep volumes)"
	@echo "restart     Restart api + web"
	@echo "logs        Tail api + web logs"
	@echo "upgrade     Pull latest code, rebuild, restart"
	@echo "admin-new   Create a new admin user"

up:
	docker compose up -d

down:
	docker compose down

restart:
	docker compose restart api web

logs:
	docker compose logs -f api web

upgrade:
	git -C ../.. pull
	docker compose build api web
	docker compose up -d api web

admin-new:
	docker compose run --rm api tale-cli admin create
