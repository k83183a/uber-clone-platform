package main

import (
    "context"
    "encoding/json"
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

    pb "github.com/uber-clone/promotions-service/proto"
)

type Promotion struct {
    ID                 string    `gorm:"primaryKey"`
    Code               string    `gorm:"uniqueIndex;not null"`
    Name               string
    Description        string
    DiscountType       string    `gorm:"not null"` // percentage, fixed
    DiscountValue      float64   `gorm:"not null"`
    MinOrderAmount     float64   `gorm:"default:0"`
    MaxDiscountAmount  float64   `gorm:"default:0"`
    StartDate          time.Time `gorm:"not null"`
    EndDate            time.Time `gorm:"not null"`
    UsageLimit         int       `gorm:"default:1"`
    PerUserLimit       int       `gorm:"default:1"`
    ApplicableServices string    `gorm:"type:text"` // JSON array
    IsActive           bool      `gorm:"default:true"`
    CreatedAt          time.Time
    UpdatedAt          time.Time
}

type PromotionRedemption struct {
    ID             string    `gorm:"primaryKey"`
    PromotionID    string    `gorm:"index;not null"`
    UserID         string    `gorm:"index;not null"`
    ServiceType    string
    ServiceID      string
    DiscountAmount float64
    RedeemedAt     time.Time
}

type PromotionsServer struct {
    pb.UnimplementedPromotionsServiceServer
    DB *gorm.DB
}

// ValidatePromotion - Validate a promo code
func (s *PromotionsServer) ValidatePromotion(ctx context.Context, req *pb.ValidatePromotionRequest) (*pb.ValidatePromotionResponse, error) {
    var promo Promotion
    if err := s.DB.Where("code = ? AND is_active = ?", req.Code, true).First(&promo).Error; err != nil {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "Invalid promo code"}, nil
    }

    now := time.Now()
    if now.Before(promo.StartDate) || now.After(promo.EndDate) {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "Promotion expired"}, nil
    }

    if promo.MinOrderAmount > 0 && req.Subtotal < promo.MinOrderAmount {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "Minimum order amount not met"}, nil
    }

    // Check usage limits
    var totalRedemptions int64
    s.DB.Model(&PromotionRedemption{}).Where("promotion_id = ?", promo.ID).Count(&totalRedemptions)
    if promo.UsageLimit > 0 && int(totalRedemptions) >= promo.UsageLimit {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "Promotion fully redeemed"}, nil
    }

    var userRedemptions int64
    s.DB.Model(&PromotionRedemption{}).Where("promotion_id = ? AND user_id = ?", promo.ID, req.UserId).Count(&userRedemptions)
    if promo.PerUserLimit > 0 && int(userRedemptions) >= promo.PerUserLimit {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "You have already used this promotion"}, nil
    }

    // Check service applicability
    if promo.ApplicableServices != "" {
        var services []string
        json.Unmarshal([]byte(promo.ApplicableServices), &services)
        if !contains(services, req.ServiceType) {
            return &pb.ValidatePromotionResponse{Valid: false, Message: "Promotion not applicable for this service"}, nil
        }
    }

    discount := s.calculateDiscount(promo, req.Subtotal)
    if promo.MaxDiscountAmount > 0 && discount > promo.MaxDiscountAmount {
        discount = promo.MaxDiscountAmount
    }

    return &pb.ValidatePromotionResponse{
        Valid:          true,
        DiscountAmount: discount,
        PromotionId:    promo.ID,
        Message:        "Promotion applied",
    }, nil
}

// RedeemPromotion - Redeem a promotion
func (s *PromotionsServer) RedeemPromotion(ctx context.Context, req *pb.RedeemPromotionRequest) (*pb.Empty, error) {
    var promo Promotion
    if err := s.DB.Where("code = ?", req.Code).First(&promo).Error; err != nil {
        return nil, status.Error(codes.NotFound, "promotion not found")
    }

    redemption := &PromotionRedemption{
        ID:             generateID(),
        PromotionID:    promo.ID,
        UserID:         req.UserId,
        ServiceType:    req.ServiceType,
        ServiceID:      req.ServiceId,
        DiscountAmount: req.DiscountAmount,
        RedeemedAt:     time.Now(),
    }
    s.DB.Create(redemption)

    return &pb.Empty{}, nil
}

// ListActivePromotions - List active promotions for a user
func (s *PromotionsServer) ListActivePromotions(ctx context.Context, req *pb.ListActivePromotionsRequest) (*pb.ListActivePromotionsResponse, error) {
    var promotions []Promotion
    now := time.Now()
    s.DB.Where("is_active = ? AND start_date <= ? AND end_date >= ?", true, now, now).Find(&promotions)

    var pbPromotions []*pb.Promotion
    for _, p := range promotions {
        // Check user usage
        var userRedemptions int64
        s.DB.Model(&PromotionRedemption{}).Where("promotion_id = ? AND user_id = ?", p.ID, req.UserId).Count(&userRedemptions)
        if p.PerUserLimit > 0 && int(userRedemptions) >= p.PerUserLimit {
            continue
        }

        pbPromotions = append(pbPromotions, &pb.Promotion{
            Id:           p.ID,
            Code:         p.Code,
            Name:         p.Name,
            Description:  p.Description,
            DiscountType: p.DiscountType,
            DiscountValue: p.DiscountValue,
            MinOrderAmount: p.MinOrderAmount,
        })
    }

    return &pb.ListActivePromotionsResponse{Promotions: pbPromotions}, nil
}

// CreatePromotion - Create a new promotion (admin)
func (s *PromotionsServer) CreatePromotion(ctx context.Context, req *pb.CreatePromotionRequest) (*pb.Promotion, error) {
    servicesJSON, _ := json.Marshal(req.ApplicableServices)
    promo := &Promotion{
        ID:                 generateID(),
        Code:               req.Code,
        Name:               req.Name,
        Description:        req.Description,
        DiscountType:       req.DiscountType,
        DiscountValue:      req.DiscountValue,
        MinOrderAmount:     req.MinOrderAmount,
        MaxDiscountAmount:  req.MaxDiscountAmount,
        StartDate:          time.Unix(req.StartDate, 0),
        EndDate:            time.Unix(req.EndDate, 0),
        UsageLimit:         int(req.UsageLimit),
        PerUserLimit:       int(req.PerUserLimit),
        ApplicableServices: string(servicesJSON),
        IsActive:           true,
        CreatedAt:          time.Now(),
        UpdatedAt:          time.Now(),
    }
    if err := s.DB.Create(promo).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create promotion")
    }

    return &pb.Promotion{
        Id:           promo.ID,
        Code:         promo.Code,
        Name:         promo.Name,
        Description:  promo.Description,
        DiscountType: promo.DiscountType,
        DiscountValue: promo.DiscountValue,
        MinOrderAmount: promo.MinOrderAmount,
    }, nil
}

// GetAIPromotions - AI-generated personalized promotions
func (s *PromotionsServer) GetAIPromotions(ctx context.Context, req *pb.GetAIPromotionsRequest) (*pb.ListActivePromotionsResponse, error) {
    // In production: use ML model based on user behavior
    // For MVP: return sample AI recommendations
    recommendations := []*pb.Promotion{
        {
            Id:           "ai_1",
            Code:         "WELCOME20",
            Name:         "Welcome Back!",
            Description:  "20% off your next ride",
            DiscountType: "percentage",
            DiscountValue: 20,
            MinOrderAmount: 5,
        },
        {
            Id:           "ai_2",
            Code:         "FREEDELIVERY",
            Name:         "Free Delivery",
            Description:  "Free delivery on your next food order",
            DiscountType: "fixed",
            DiscountValue: 0, // Delivery fee is removed separately
            MinOrderAmount: 10,
        },
    }

    return &pb.ListActivePromotionsResponse{Promotions: recommendations}, nil
}

func (s *PromotionsServer) calculateDiscount(promo Promotion, subtotal float64) float64 {
    if promo.DiscountType == "percentage" {
        discount := subtotal * (promo.DiscountValue / 100)
        if promo.MaxDiscountAmount > 0 && discount > promo.MaxDiscountAmount {
            discount = promo.MaxDiscountAmount
        }
        return discount
    }
    // fixed amount
    if promo.DiscountValue > subtotal {
        return subtotal
    }
    return promo.DiscountValue
}

func contains(slice []string, item string) bool {
    for _, s := range slice {
        if s == item {
            return true
        }
    }
    return false
}

func generateID() string {
    return "promo_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=promodb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }
    db.AutoMigrate(&Promotion{}, &PromotionRedemption{})

    grpcServer := grpc.NewServer()
    pb.RegisterPromotionsServiceServer(grpcServer, &PromotionsServer{DB: db})

    lis, err := net.Listen("tcp", ":50060")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Promotions Service running on port 50060")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}