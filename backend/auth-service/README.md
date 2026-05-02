# Auth Service

JWT authentication service for Uber Clone platform.

## Features
- User registration and login
- JWT access/refresh tokens
- Token validation and refresh
- Logout (token invalidation)
- Prometheus metrics on :9090
- Health check on :9090/health

## Run
```bash
go run cmd/server/main.go