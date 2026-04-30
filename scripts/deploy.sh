#!/bin/bash

set -e

echo "═══════════════════════════════════════════════════════════════"
echo "     UBER CLONE PLATFORM – DEPLOYMENT"
echo "═══════════════════════════════════════════════════════════════"

cd "$(dirname "$0")/.."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}❌ Docker is not running. Please start Docker first.${NC}"
    exit 1
fi

# Create databases
echo -e "${YELLOW}📦 Creating databases...${NC}"
docker-compose up -d postgres
sleep 5

# Create databases for each service
docker exec -i uber_postgres psql -U postgres << EOF
CREATE DATABASE IF NOT EXISTS authdb;
CREATE DATABASE IF NOT EXISTS userdb;
CREATE DATABASE IF NOT EXISTS ridedb;
CREATE DATABASE IF NOT EXISTS paymentdb;
CREATE DATABASE IF NOT EXISTS notificationdb;
EOF

echo -e "${GREEN}✅ Databases created${NC}"

# Build and start all services
echo -e "${YELLOW}🚀 Building and starting services...${NC}"
docker-compose up -d --build

echo ""
echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}  ✅ UBER CLONE PLATFORM IS RUNNING!${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
echo ""
echo "📊 Services:"
echo "   Auth:        localhost:50051"
echo "   User:        localhost:50052"
echo "   Ride:        localhost:50053"
echo "   Location:    localhost:8080 (WebSocket)"
echo "   Dispatch:    localhost:50060"
echo "   Payment:     localhost:50054 (gRPC), localhost:8082 (webhook)"
echo "   Notification: localhost:50055 (gRPC), localhost:8083 (health)"
echo "   API Gateway: localhost:8080"
echo ""
echo "📝 To view logs: docker-compose logs -f"
echo "🛑 To stop: docker-compose down"
echo ""