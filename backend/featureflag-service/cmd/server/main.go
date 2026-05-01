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

    pb "github.com/uber-clone/featureflag-service/proto"
)

type FeatureFlag struct {
    ID          string    `gorm:"primaryKey"`
    Name        string    `gorm:"uniqueIndex;not null"`
    Enabled     bool      `gorm:"default:false"`
    Description string
    UpdatedBy   string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type FeatureConfig struct {
    ID          string    `gorm:"primaryKey"`
    Key         string    `gorm:"uniqueIndex;not null"`
    Value       string    `gorm:"not null"`
    Description string
    UpdatedBy   string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type FeatureFlagServer struct {
    pb.UnimplementedFeatureFlagServiceServer
    DB *gorm.DB
}

// GetFlag - Get feature flag value
func (s *FeatureFlagServer) GetFlag(ctx context.Context, req *pb.GetFlagRequest) (*pb.FlagResponse, error) {
    var flag FeatureFlag
    if err := s.DB.Where("name = ?", req.FlagName).First(&flag).Error; err != nil {
        return &pb.FlagResponse{Enabled: false}, nil
    }
    return &pb.FlagResponse{Enabled: flag.Enabled}, nil
}

// SetFlag - Set feature flag (admin)
func (s *FeatureFlagServer) SetFlag(ctx context.Context, req *pb.SetFlagRequest) (*pb.Empty, error) {
    var flag FeatureFlag
    result := s.DB.Where("name = ?", req.FlagName).First(&flag)
    if result.Error != nil {
        flag = FeatureFlag{
            ID:        generateID(),
            Name:      req.FlagName,
            Enabled:   req.Enabled,
            CreatedAt: time.Now(),
            UpdatedAt: time.Now(),
        }
        if err := s.DB.Create(&flag).Error; err != nil {
            return nil, status.Error(codes.Internal, "failed to create flag")
        }
    } else {
        flag.Enabled = req.Enabled
        flag.UpdatedAt = time.Now()
        if err := s.DB.Save(&flag).Error; err != nil {
            return nil, status.Error(codes.Internal, "failed to update flag")
        }
    }
    return &pb.Empty{}, nil
}

// GetConfig - Get configuration value
func (s *FeatureFlagServer) GetConfig(ctx context.Context, req *pb.GetConfigRequest) (*pb.ConfigResponse, error) {
    var config FeatureConfig
    if err := s.DB.Where("key = ?", req.ConfigKey).First(&config).Error; err != nil {
        return &pb.ConfigResponse{Value: ""}, nil
    }
    return &pb.ConfigResponse{Value: config.Value}, nil
}

// SetConfig - Set configuration value (admin)
func (s *FeatureFlagServer) SetConfig(ctx context.Context, req *pb.SetConfigRequest) (*pb.Empty, error) {
    var config FeatureConfig
    result := s.DB.Where("key = ?", req.ConfigKey).First(&config)
    if result.Error != nil {
        config = FeatureConfig{
            ID:        generateID(),
            Key:       req.ConfigKey,
            Value:     req.Value,
            CreatedAt: time.Now(),
            UpdatedAt: time.Now(),
        }
        if err := s.DB.Create(&config).Error; err != nil {
            return nil, status.Error(codes.Internal, "failed to create config")
        }
    } else {
        config.Value = req.Value
        config.UpdatedAt = time.Now()
        if err := s.DB.Save(&config).Error; err != nil {
            return nil, status.Error(codes.Internal, "failed to update config")
        }
    }
    return &pb.Empty{}, nil
}

// ListAllFlags - List all feature flags
func (s *FeatureFlagServer) ListAllFlags(ctx context.Context, req *pb.Empty) (*pb.FlagsList, error) {
    var flags []FeatureFlag
    if err := s.DB.Find(&flags).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list flags")
    }
    result := make(map[string]bool)
    for _, f := range flags {
        result[f.Name] = f.Enabled
    }
    return &pb.FlagsList{Flags: result}, nil
}

// ListAllConfigs - List all configurations
func (s *FeatureFlagServer) ListAllConfigs(ctx context.Context, req *pb.Empty) (*pb.ConfigsList, error) {
    var configs []FeatureConfig
    if err := s.DB.Find(&configs).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list configs")
    }
    result := make(map[string]string)
    for _, c := range configs {
        result[c.Key] = c.Value
    }
    return &pb.ConfigsList{Configs: result}, nil
}

func generateID() string {
    return "ff_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=featuredb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&FeatureFlag{}, &FeatureConfig{})

    // Seed default flags
    var count int64
    db.Model(&FeatureFlag{}).Count(&count)
    if count == 0 {
        defaultFlags := []FeatureFlag{
            {ID: generateID(), Name: "ride_bidding.enabled", Enabled: true, Description: "Enable ride bidding feature", CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), Name: "agent_model.enabled", Enabled: true, Description: "Enable agent business model", CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), Name: "surge_pricing.enabled", Enabled: true, Description: "Enable surge pricing", CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), Name: "caz_surcharge.enabled", Enabled: true, Description: "Enable Clean Air Zone surcharge", CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), Name: "loyalty.enabled", Enabled: true, Description: "Enable loyalty points", CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), Name: "subscriptions.enabled", Enabled: true, Description: "Enable subscriptions", CreatedAt: time.Now(), UpdatedAt: time.Now()},
        }
        db.Create(&defaultFlags)
        log.Println("Seeded default feature flags")

        defaultConfigs := []FeatureConfig{
            {ID: generateID(), Key: "caz_surcharge.amount", Value: "2.50", Description: "Clean Air Zone surcharge in GBP", CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), Key: "surge.max_multiplier", Value: "3.0", Description: "Maximum surge multiplier", CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), Key: "incentives.100trips.bonus", Value: "300", Description: "100 trips in 7 days bonus amount", CreatedAt: time.Now(), UpdatedAt: time.Now()},
        }
        db.Create(&defaultConfigs)
        log.Println("Seeded default configurations")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterFeatureFlagServiceServer(grpcServer, &FeatureFlagServer{DB: db})

    lis, err := net.Listen("tcp", ":50064")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Feature Flag Service running on port 50064")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}