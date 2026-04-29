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
  pb "github.com/uber-clone/vat-service/proto"
)

type ServiceServer struct {
  pb.UnimplementedVatServiceServer
  DB *gorm.DB
}

func (s *ServiceServer) Health(ctx context.Context, req *pb.Empty) (*pb.HealthResponse, error) {
  return &pb.HealthResponse{Status: "healthy", Timestamp: time.Now().Unix()}, nil
}

func main() {
  godotenv.Load()
  dsn := os.Getenv("DB_DSN")
  if dsn == "" {
    dsn = "host=postgres user=postgres password=postgres dbname=vatdb port=5432 sslmode=disable"
  }
  db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
  if err != nil {
    log.Fatal("Failed to connect to database:", err)
  }
  grpcServer := grpc.NewServer()
  pb.RegisterVatServiceServer(grpcServer, &ServiceServer{DB: db})
  lis, err := net.Listen("tcp", ":50076")
  if err != nil {
    log.Fatal("Failed to listen:", err)
  }
  go func() {
    log.Println("✅ Vat Service running on port 50076")
    if err := grpcServer.Serve(lis); err != nil {
      log.Fatal("Failed to serve:", err)
    }
  }()
  quit := make(chan os.Signal, 1)
  signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
  <-quit
  grpcServer.GracefulStop()
}
