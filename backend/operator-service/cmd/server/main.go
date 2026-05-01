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
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/uber-clone/operator-service/proto"
)

// Operator represents a legal entity operating in a region
type Operator struct {
    ID               string    `gorm:"primaryKey"`
    Name             string    `gorm:"not null"`
    BusinessModel    string    `gorm:"not null"` // principal, agent
    CommissionPercent float64  `gorm:"default:0"`
    Regions          string    `gorm:"type:text"` // JSON array of region names
    IsActive         bool      `gorm:"default:true"`
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

// OperatorServer handles gRPC requests
type OperatorServer struct {
    pb.UnimplementedOperatorServiceServer
    DB *gorm.DB
}

// GetOperatorForRegion returns the operator assigned to a region
func (s *OperatorServer) GetOperatorForRegion(ctx context.Context, req *pb.GetOperatorRequest) (*pb.OperatorResponse, error) {
    var operators []Operator
    if err := s.DB.Where("is_active = ?", true).Find(&operators).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to fetch operators")
    }

    for _, op := range operators {
        var regions []string
        if err := json.Unmarshal([]byte(op.Regions), &regions); err != nil {
            continue
        }
        for _, r := range regions {
            if strings.EqualFold(r, req.Region) {
                return &pb.OperatorResponse{
                    OperatorId:       op.ID,
                    Name:             op.Name,
                    BusinessModel:    op.BusinessModel,
                    CommissionPercent: op.CommissionPercent,
                    Regions:          regions,
                }, nil
            }
        }
    }

    // Return default operator if none found
    return &pb.OperatorResponse{
        OperatorId:       "default_operator",
        Name:             "Default Operator",
        BusinessModel:    "principal",
        CommissionPercent: 0,
        Regions:          []string{"default"},
    }, nil
}

// ListOperators returns all operators
func (s *OperatorServer) ListOperators(ctx context.Context, req *pb.Empty) (*pb.OperatorsList, error) {
    var operators []Operator
    if err := s.DB.Where("is_active = ?", true).Find(&operators).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list operators")
    }

    var pbOperators []*pb.Operator
    for _, op := range operators {
        var regions []string
        json.Unmarshal([]byte(op.Regions), &regions)

        pbOperators = append(pbOperators, &pb.Operator{
            Id:               op.ID,
            Name:             op.Name,
            BusinessModel:    op.BusinessModel,
            Regions:          regions,
            CommissionPercent: op.CommissionPercent,
        })
    }

    return &pb.OperatorsList{Operators: pbOperators}, nil
}

// CreateOperator creates a new operator (admin endpoint)
func (s *OperatorServer) CreateOperator(ctx context.Context, req *pb.CreateOperatorRequest) (*pb.Operator, error) {
    regionsJSON, _ := json.Marshal(req.Regions)

    operator := &Operator{
        ID:               generateID(),
        Name:             req.Name,
        BusinessModel:    req.BusinessModel,
        CommissionPercent: req.CommissionPercent,
        Regions:          string(regionsJSON),
        IsActive:         true,
        CreatedAt:        time.Now(),
        UpdatedAt:        time.Now(),
    }

    if err := s.DB.Create(operator).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create operator")
    }

    return &pb.Operator{
        Id:               operator.ID,
        Name:             operator.Name,
        BusinessModel:    operator.BusinessModel,
        Regions:          req.Regions,
        CommissionPercent: operator.CommissionPercent,
    }, nil
}

// UpdateOperator updates an existing operator
func (s *OperatorServer) UpdateOperator(ctx context.Context, req *pb.UpdateOperatorRequest) (*pb.Operator, error) {
    var operator Operator
    if err := s.DB.Where("id = ?", req.Id).First(&operator).Error; err != nil {
        return nil, status.Error(codes.NotFound, "operator not found")
    }

    if req.Name != "" {
        operator.Name = req.Name
    }
    if req.BusinessModel != "" {
        operator.BusinessModel = req.BusinessModel
    }
    if req.CommissionPercent != 0 {
        operator.CommissionPercent = req.CommissionPercent
    }
    if len(req.Regions) > 0 {
        regionsJSON, _ := json.Marshal(req.Regions)
        operator.Regions = string(regionsJSON)
    }
    operator.UpdatedAt = time.Now()

    if err := s.DB.Save(&operator).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update operator")
    }

    return &pb.Operator{
        Id:               operator.ID,
        Name:             operator.Name,
        BusinessModel:    operator.BusinessModel,
        Regions:          req.Regions,
        CommissionPercent: operator.CommissionPercent,
    }, nil
}

// DeleteOperator soft-deletes an operator
func (s *OperatorServer) DeleteOperator(ctx context.Context, req *pb.DeleteOperatorRequest) (*pb.Empty, error) {
    if err := s.DB.Where("id = ?", req.Id).Delete(&Operator{}).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to delete operator")
    }
    return &pb.Empty{}, nil
}

func generateID() string {
    return "op_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func randomString(n int) string {
    const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
    b := make([]byte, n)
    for i := range b {
        b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
    }
    return string(b)
}

func main() {
    godotenv.Load()

    dsn := os.Getenv("DB_DSN")
    if dsn == "" {
        dsn = "host=postgres user=postgres password=postgres dbname=operatordb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Operator{})

    // Seed default operators
    var count int64
    db.Model(&Operator{}).Count(&count)
    if count == 0 {
        londonRegions, _ := json.Marshal([]string{"london", "city_of_london", "westminster"})
        birminghamRegions, _ := json.Marshal([]string{"birmingham", "coventry", "wolverhampton"})

        db.Create(&Operator{
            ID:               generateID(),
            Name:             "Uber London Ltd",
            BusinessModel:    "principal",
            CommissionPercent: 0,
            Regions:          string(londonRegions),
            IsActive:         true,
            CreatedAt:        time.Now(),
            UpdatedAt:        time.Now(),
        })
        db.Create(&Operator{
            ID:               generateID(),
            Name:             "Uber Birmingham Agency",
            BusinessModel:    "agent",
            CommissionPercent: 15.0,
            Regions:          string(birminghamRegions),
            IsActive:         true,
            CreatedAt:        time.Now(),
            UpdatedAt:        time.Now(),
        })
        log.Println("Seeded default operators: London (principal), Birmingham (agent)")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterOperatorServiceServer(grpcServer, &OperatorServer{DB: db})

    lis, err := net.Listen("tcp", ":50063")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Operator Service running on port 50063")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Operator Service stopped")
}