.PHONY: test lint build up down help

help:
	@echo "Targets:"
	@echo "  make test   - run tests for all services"
	@echo "  make lint   - run linters / static checks for all services"
	@echo "  make build  - build all Docker images"
	@echo "  make up     - start the full stack (docker-compose)"
	@echo "  make down   - stop the full stack"

test:
	cd gateway && go test ./...
	cd admin && go test ./...
	cd dashboard && npm run test:run
	cd parser && pytest

lint:
	cd gateway && go vet ./...
	cd admin && go vet ./...
	cd dashboard && npm run lint

build:
	docker-compose --profile app build

up:
	docker-compose --profile app up -d --wait

down:
	docker-compose --profile app down
