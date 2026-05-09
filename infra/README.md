# Infrastructure

## Quick Start

```bash
cp .env.example .env
docker-compose up -d postgres redis
```

## Services

| Service    | Port | Purpose              |
|------------|------|----------------------|
| PostgreSQL | 5432 | Primary database     |
| Redis      | 6379 | Real-time quota cache |

## Start Everything

```bash
docker-compose --profile app up -d
```

## Reset Database

```bash
docker-compose down -v && docker-compose up -d postgres redis
```

## Connect to PostgreSQL

```bash
docker-compose exec postgres psql -U totra -d totra
```

## Connect to Redis

```bash
docker-compose exec redis redis-cli
```

## Apply Seed Data (dev only)

```bash
docker-compose exec -e PGPASSWORD=totra_secret postgres psql -U totra -d totra -c "$(cat infra/postgres/002_seed_dev.sql)"
```
