 #!/bin/bash
port=50050
for svc in location dispatch payment notification food grocery courier geofencing promotions subscriptions incentives operator featureflag safety gamification loyalty qcommerce b2b appointment chat ai-concierge driver document compliance onboarding vat routing; do
  mkdir -p backend/${svc}-service/cmd/server backend/${svc}-service/proto
  port=$((port + 1))
  # Create main.go with proper port
  cat > backend/${svc}-service/cmd/server/main.go << EOF
package main

import (
  "context"
  "log"
  "net"
  "os"
  "os/signal"
  "syscall"
  "time"
  "github.com/joho/godotenv"
  "google.golang.org/grpc"
  "gorm.io/driver/postgres"
  "gorm.io/gorm"
  pb "github.com/uber-clone/${svc}-service/proto"
)

type ServiceServer struct {
  pb.Unimplemented${svc^}ServiceServer
  DB *gorm.DB
}

func (s *ServiceServer) Health(ctx context.Context, req *pb.Empty) (*pb.HealthResponse, error) {
  return &pb.HealthResponse{Status: "healthy", Timestamp: time.Now().Unix()}, nil
}

func main() {
  godotenv.Load()
  dsn := os.Getenv("DB_DSN")
  if dsn == "" {
    dsn = "host=postgres user=postgres password=postgres dbname=${svc}db port=5432 sslmode=disable"
  }
  db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
  if err != nil {
    log.Fatal("Failed to connect to database:", err)
  }
  grpcServer := grpc.NewServer()
  pb.Register${svc^}ServiceServer(grpcServer, &ServiceServer{DB: db})
  lis, err := net.Listen("tcp", ":$port")
  if err != nil {
    log.Fatal("Failed to listen:", err)
  }
  go func() {
    log.Println("✅ ${svc^} Service running on port $port")
    if err := grpcServer.Serve(lis); err != nil {
      log.Fatal("Failed to serve:", err)
    }
  }()
  quit := make(chan os.Signal, 1)
  signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
  <-quit
  grpcServer.GracefulStop()
}
EOF
  # Create proto file
  cat > backend/${svc}-service/proto/${svc}.proto << EOF
syntax = "proto3";
package ${svc};
option go_package = "github.com/uber-clone/${svc}-service/proto";
service ${svc^}Service {
  rpc Health(Empty) returns (HealthResponse);
}
message Empty {}
message HealthResponse {
  string status = 1;
  int64 timestamp = 2;
}
EOF
  echo "✅ Created ${svc}-service on port $port"
done
