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

    pb "github.com/uber-clone/promotions-service/proto"
)

// Promotion represents a discount code or offer
type Promotion struct {
    ID                   string    `gorm:"primaryKey"`
    Code                 string    `gorm:"uniqueIndex;not null"`
    Name                 string
    Description          string
    DiscountType         string    `gorm:"not null"` // percentage, fixed_amount
    DiscountValue        float64   `gorm:"not null"`
    MinOrderAmount       float64   `gorm:"default:0"`
    MaxDiscountAmount    float64   `gorm:"default:0"`
    StartDate            time.Time `gorm:"not null"`
    EndDate              time.Time `gorm:"not null"`
    UsageLimit           int       `gorm:"default:1"`      // total times this code can be used
    PerUserLimit         int       `gorm:"default:1"`      // times per user
    ApplicableServices   string    `gorm:"type:text"`      // JSON array: ["ride","food","grocery","courier"]
    ApplicableRideTypes  string    `gorm:"type:text"`      // JSON array: ["uberX","uberXL","green"]
    IsActive             bool      `gorm:"default:true"`
    IsAutoPromotion      bool      `gorm:"default:false"`   // AI-generated promotion
    TriggerCondition     string    `gorm:"type:text"`       // JSON for auto-trigger conditions
    CreatedBy            string
    CreatedAt            time.Time
    UpdatedAt            time.Time
}

// PromotionRedemption tracks user redemptions
type PromotionRedemption struct {
    ID           string    `gorm:"primaryKey"`
    PromotionID  string    `gorm:"index;not null"`
    UserID       string    `gorm:"index;not null"`
    ServiceType  string
    ServiceID    string
    DiscountAmount float64
    RedeemedAt   time.Time
}

// PromotionsServer handles gRPC requests
type PromotionsServer struct {
    pb.UnimplementedPromotionsServiceServer
    DB *gorm.DB
}

// ValidatePromotion validates a promo code for a user and service
func (s *PromotionsServer) ValidatePromotion(ctx context.Context, req *pb.ValidatePromotionRequest) (*pb.ValidatePromotionResponse, error) {
    var promotion Promotion
    if err := s.DB.Where("code = ? AND is_active = ?", req.Code, true).First(&promotion).Error; err != nil {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "Invalid promo code"}, nil
    }

    now := time.Now()
    if now.Before(promotion.StartDate) || now.After(promotion.EndDate) {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "Promotion has expired"}, nil
    }

    if promotion.MinOrderAmount > 0 && req.Subtotal < promotion.MinOrderAmount {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "Minimum order amount not met"}, nil
    }

    // Check usage limits
    var totalRedemptions int64
    s.DB.Model(&PromotionRedemption{}).Where("promotion_id = ?", promotion.ID).Count(&totalRedemptions)
    if promotion.UsageLimit > 0 && int(totalRedemptions) >= promotion.UsageLimit {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "Promotion usage limit reached"}, nil
    }

    var userRedemptions int64
    s.DB.Model(&PromotionRedemption{}).Where("promotion_id = ? AND user_id = ?", promotion.ID, req.UserId).Count(&userRedemptions)
    if promotion.PerUserLimit > 0 && int(userRedemptions) >= promotion.PerUserLimit {
        return &pb.ValidatePromotionResponse{Valid: false, Message: "You have already used this promotion"}, nil
    }

    // Check applicable services
    if promotion.ApplicableServices != "" {
        var services []string
        json.Unmarshal([]byte(promotion.ApplicableServices), &services)
        if !contains(services, req.ServiceType) {
            return &pb.ValidatePromotionResponse{Valid: false, Message: "Promotion not applicable for this service"}, nil
        }
    }

    // Calculate discount amount
    discountAmount := calculateDiscount(promotion, req.Subtotal)
    if promotion.MaxDiscountAmount > 0 && discountAmount > promotion.MaxDiscountAmount {
        discountAmount = promotion.MaxDiscountAmount
    }

    return &pb.ValidatePromotionResponse{
        Valid:          true,
        DiscountAmount: discountAmount,
        PromotionId:    promotion.ID,
        Message:        "Promotion applied",
    }, nil
}

// RedeemPromotion marks a promotion as used for a user
func (s *PromotionsServer) RedeemPromotion(ctx context.Context, req *pb.RedeemPromotionRequest) (*pb.Empty, error) {
    var promotion Promotion
    if err := s.DB.Where("code = ?", req.Code).First(&promotion).Error; err != nil {
        return nil, status.Error(codes.NotFound, "promotion not found")
    }

    redemption := &PromotionRedemption{
        ID:           generateID(),
        PromotionID:  promotion.ID,
        UserID:       req.UserId,
        ServiceType:  req.ServiceType,
        ServiceID:    req.ServiceId,
        DiscountAmount: req.DiscountAmount,
        RedeemedAt:   time.Now(),
    }

    if err := s.DB.Create(redemption).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to record redemption")
    }

    return &pb.Empty{}, nil
}

// ListActivePromotions lists promotions available for a user
func (s *PromotionsServer) ListActivePromotions(ctx context.Context, req *pb.ListActivePromotionsRequest) (*pb.ListActivePromotionsResponse, error) {
    var promotions []Promotion
    now := time.Now()

    query := s.DB.Where("is_active = ? AND start_date <= ? AND end_date >= ?", true, now, now)

    if req.ServiceType != "" {
        query = query.Where("applicable_services LIKE ?", "%"+req.ServiceType+"%")
    }

    if err := query.Find(&promotions).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list promotions")
    }

    // Check per-user usage and filter
    var pbPromotions []*pb.Promotion
    for _, p := range promotions {
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

// CreatePromotion creates a new promotion (admin endpoint)
func (s *PromotionsServer) CreatePromotion(ctx context.Context, req *pb.CreatePromotionRequest) (*pb.Promotion, error) {
    // Validate JSON fields
    servicesJSON, _ := json.Marshal(req.ApplicableServices)
    rideTypesJSON, _ := json.Marshal(req.ApplicableRideTypes)

    promotion := &Promotion{
        ID:                  generateID(),
        Code:                req.Code,
        Name:                req.Name,
        Description:         req.Description,
        DiscountType:        req.DiscountType,
        DiscountValue:       req.DiscountValue,
        MinOrderAmount:      req.MinOrderAmount,
        MaxDiscountAmount:   req.MaxDiscountAmount,
        StartDate:           time.Unix(req.StartDate, 0),
        EndDate:             time.Unix(req.EndDate, 0),
        UsageLimit:          int(req.UsageLimit),
        PerUserLimit:        int(req.PerUserLimit),
        ApplicableServices:  string(servicesJSON),
        ApplicableRideTypes: string(rideTypesJSON),
        IsActive:            true,
        CreatedAt:           time.Now(),
        UpdatedAt:           time.Now(),
    }

    if err := s.DB.Create(promotion).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create promotion")
    }

    return &pb.Promotion{
        Id:           promotion.ID,
        Code:         promotion.Code,
        Name:         promotion.Name,
        Description:  promotion.Description,
        DiscountType: promotion.DiscountType,
        DiscountValue: promotion.DiscountValue,
        MinOrderAmount: promotion.MinOrderAmount,
    }, nil
}

// GetAIPromotions returns AI-generated personalized promotions for a user
func (s *PromotionsServer) GetAIPromotions(ctx context.Context, req *pb.GetAIPromotionsRequest) (*pb.ListActivePromotionsResponse, error) {
    // In production: Use ML model to analyze user behavior
    // For MVP: return some default offers based on user's ride count
    var userRides int64
    // This would come from ride-service via gRPC
    // For now, generate simple personalization

    var promotions []*pb.Promotion
    // Example: Offer discount if user hasn't ridden in a while
    promotions = append(promotions, &pb.Promotion{
        Id:            "ai_1",
        Code:          "PERSONAL20",
        Name:          "Welcome Back!",
        Description:   "20% off your next ride just for you",
        DiscountType:  "percentage",
        DiscountValue: 20,
        MinOrderAmount: 5,
    })

    return &pb.ListActivePromotionsResponse{Promotions: promotions}, nil
}

func calculateDiscount(promo Promotion, subtotal float64) float64 {
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
        dsn = "host=postgres user=postgres password=postgres dbname=promotionsdb port=5432 sslmode=disable"
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
    log.Println("Promotions Service stopped")
}