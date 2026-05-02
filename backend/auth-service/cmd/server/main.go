package main

import (
    "log"
    "net"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/joho/godotenv"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "google.golang.org/grpc"
    "google.golang.org/grpc/health/grpc_health_v1"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/uber-clone/auth-service/proto"
    "github.com/uber-clone/auth-service/internal/handler"
    "github.com/uber-clone/auth-service/internal/service"
    "github.com/uber-clone/auth-service/internal/repository"
    "github.com/uber-clone/auth-service/internal/kafka"
)

type healthServer struct{}

func (s *healthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
    return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (s *healthServer) Watch(req *grpc_health_v1.HealthCheckRequest, stream grpc_health_v1.Health_WatchServer) error {
    return nil
}

func main() {
    godotenv.Load()

    dsn := os.Getenv("DB_DSN")
    if dsn == "" {
        dsn = "host=postgres user=postgres password=postgres dbname=authdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
        PrepareStmt: true,
    })
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    sqlDB, _ := db.DB()
    sqlDB.SetMaxIdleConns(10)
    sqlDB.SetMaxOpenConns(100)
    sqlDB.SetConnMaxLifetime(time.Hour)

    db.AutoMigrate(&model.User{})

    jwtSecret := []byte(os.Getenv("JWT_SECRET"))
    if len(jwtSecret) == 0 {
        log.Fatal("JWT_SECRET environment variable is required")
    }

    repo := repository.NewAuthRepository(db)
    authService := service.NewAuthService(repo, jwtSecret)

    kafkaBroker := os.Getenv("KAFKA_BROKER")
    if kafkaBroker != "" {
        kafkaProducer, _ := kafka.NewProducer(kafkaBroker)
        defer kafkaProducer.Close()
    }

    grpcHandler := handler.NewGrpcHandler(authService)

    grpcServer := grpc.NewServer(
        grpc.MaxRecvMsgSize(10*1024*1024),
        grpc.MaxSendMsgSize(10*1024*1024),
    )
    pb.RegisterAuthServiceServer(grpcServer, grpcHandler)
    grpc_health_v1.RegisterHealthServer(grpcServer, &healthServer{})

    http.Handle("/metrics", promhttp.Handler())
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ok"}`))
    })
    go http.ListenAndServe(":9090", nil)

    lis, err := net.Listen("tcp", ":50051")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Auth Service running on port 50051")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    sqlDB.Close()
    log.Println("Auth Service stopped")
}