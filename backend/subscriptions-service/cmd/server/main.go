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

    pb "github.com/uber-clone/subscription-service/proto"
)

// Plan represents a subscription plan
type Plan struct {
    ID          string    `gorm:"primaryKey"`
    Name        string    `gorm:"not null"` // monthly, yearly
    Description string
    PriceGBP    float64   `gorm:"not null"`
    BillingPeriod string  `gorm:"not null"` // month, year
    Benefits    string    `gorm:"type:text"` // JSON array of benefits
    IsActive    bool      `gorm:"default:true"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// Subscription represents a user's active subscription
type Subscription struct {
    ID                   string     `gorm:"primaryKey"`
    UserID               string     `gorm:"uniqueIndex;not null"`
    PlanID               string     `gorm:"not null"`
    Status               string     `gorm:"default:'active'"` // active, cancelled, expired
    StartDate            time.Time  `gorm:"not null"`
    EndDate              time.Time  `gorm:"not null"`
    AutoRenew            bool       `gorm:"default:true"`
    StripeSubscriptionID string     `gorm:"index"`
    CancelledAt          *time.Time
    CreatedAt            time.Time
    UpdatedAt            time.Time
}

// Benefit struct for JSON storage
type Benefit struct {
    Type  string  `json:"type"`  // ride_discount_percent, free_delivery, priority_support
    Value float64 `json:"value"` // discount percentage or 1 for boolean benefits
}

// SubscriptionServer handles gRPC requests
type SubscriptionServer struct {
    pb.UnimplementedSubscriptionServiceServer
    DB *gorm.DB
}

// GetPlans returns all available subscription plans
func (s *SubscriptionServer) GetPlans(ctx context.Context, req *pb.Empty) (*pb.PlansResponse, error) {
    var plans []Plan
    if err := s.DB.Where("is_active = ?", true).Find(&plans).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get plans")
    }

    var pbPlans []*pb.Plan
    for _, p := range plans {
        var benefits []*pb.Benefit
        json.Unmarshal([]byte(p.Benefits), &benefits)
        pbPlans = append(pbPlans, &pb.Plan{
            Id:          p.ID,
            Name:        p.Name,
            Description: p.Description,
            PriceGbp:    p.PriceGBP,
            Benefits:    benefits,
        })
    }

    return &pb.PlansResponse{Plans: pbPlans}, nil
}

// GetActiveSubscription returns the user's active subscription
func (s *SubscriptionServer) GetActiveSubscription(ctx context.Context, req *pb.GetActiveRequest) (*pb.SubscriptionResponse, error) {
    var sub Subscription
    if err := s.DB.Where("user_id = ? AND status = ? AND end_date > ?", req.RiderId, "active", time.Now()).First(&sub).Error; err != nil {
        return &pb.SubscriptionResponse{Status: "none"}, nil
    }

    var plan Plan
    s.DB.Where("id = ?", sub.PlanID).First(&plan)

    var benefits []*pb.Benefit
    json.Unmarshal([]byte(plan.Benefits), &benefits)

    // Extract discount percent for convenience
    var discountPercent float64
    var freeDelivery bool
    for _, b := range benefits {
        if b.Type == "ride_discount_percent" {
            discountPercent = b.Value
        }
        if b.Type == "free_delivery" {
            freeDelivery = b.Value > 0
        }
    }

    return &pb.SubscriptionResponse{
        Id:              sub.ID,
        RiderId:         sub.UserID,
        PlanId:          sub.PlanID,
        PlanName:        plan.Name,
        Status:          sub.Status,
        DiscountPercent: discountPercent,
        FreeDelivery:    freeDelivery,
        StartDate:       sub.StartDate.Unix(),
        EndDate:         sub.EndDate.Unix(),
        AutoRenew:       sub.AutoRenew,
    }, nil
}

// CreateSubscription creates a new subscription for a user
func (s *SubscriptionServer) CreateSubscription(ctx context.Context, req *pb.CreateRequest) (*pb.SubscriptionResponse, error) {
    // Check if user already has an active subscription
    var existing Subscription
    if err := s.DB.Where("user_id = ? AND status = ?", req.RiderId, "active").First(&existing).Error; err == nil {
        return nil, status.Error(codes.AlreadyExists, "user already has an active subscription")
    }

    // Get plan details
    var plan Plan
    if err := s.DB.Where("id = ? AND is_active = ?", req.PlanId, true).First(&plan).Error; err != nil {
        return nil, status.Error(codes.NotFound, "plan not found")
    }

    // Calculate start and end dates
    startDate := time.Now()
    var endDate time.Time
    if plan.BillingPeriod == "month" {
        endDate = startDate.AddDate(0, 1, 0)
    } else {
        endDate = startDate.AddDate(1, 0, 0)
    }

    // In production: create Stripe subscription here
    stripeSubscriptionID := "sub_" + generateID()

    sub := &Subscription{
        ID:                   generateID(),
        UserID:               req.RiderId,
        PlanID:               req.PlanId,
        Status:               "active",
        StartDate:            startDate,
        EndDate:              endDate,
        AutoRenew:            true,
        StripeSubscriptionID: stripeSubscriptionID,
        CreatedAt:            time.Now(),
        UpdatedAt:            time.Now(),
    }

    if err := s.DB.Create(sub).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create subscription")
    }

    // Parse benefits for response
    var benefits []*pb.Benefit
    json.Unmarshal([]byte(plan.Benefits), &benefits)
    var discountPercent float64
    var freeDelivery bool
    for _, b := range benefits {
        if b.Type == "ride_discount_percent" {
            discountPercent = b.Value
        }
        if b.Type == "free_delivery" {
            freeDelivery = b.Value > 0
        }
    }

    return &pb.SubscriptionResponse{
        Id:              sub.ID,
        RiderId:         sub.UserID,
        PlanId:          sub.PlanID,
        PlanName:        plan.Name,
        Status:          sub.Status,
        DiscountPercent: discountPercent,
        FreeDelivery:    freeDelivery,
        StartDate:       sub.StartDate.Unix(),
        EndDate:         sub.EndDate.Unix(),
        AutoRenew:       sub.AutoRenew,
    }, nil
}

// CancelSubscription cancels a user's subscription (stops auto-renew)
func (s *SubscriptionServer) CancelSubscription(ctx context.Context, req *pb.CancelRequest) (*pb.Empty, error) {
    var sub Subscription
    if err := s.DB.Where("user_id = ? AND status = ?", req.RiderId, "active").First(&sub).Error; err != nil {
        return nil, status.Error(codes.NotFound, "no active subscription found")
    }

    now := time.Now()
    sub.Status = "cancelled"
    sub.AutoRenew = false
    sub.CancelledAt = &now
    sub.UpdatedAt = now

    // In production: cancel Stripe subscription at period end
    if err := s.DB.Save(&sub).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to cancel subscription")
    }

    return &pb.Empty{}, nil
}

// IsEligibleForBenefit checks if a user has a specific benefit
func (s *SubscriptionServer) IsEligibleForBenefit(ctx context.Context, req *pb.EligibilityRequest) (*pb.EligibilityResponse, error) {
    var sub Subscription
    if err := s.DB.Where("user_id = ? AND status = ? AND end_date > ?", req.RiderId, "active", time.Now()).First(&sub).Error; err != nil {
        return &pb.EligibilityResponse{Eligible: false}, nil
    }

    var plan Plan
    s.DB.Where("id = ?", sub.PlanID).First(&plan)

    var benefits []Benefit
    json.Unmarshal([]byte(plan.Benefits), &benefits)

    var discountPercent float64
    var freeDelivery bool
    for _, b := range benefits {
        if b.Type == "ride_discount_percent" {
            discountPercent = b.Value
        }
        if b.Type == "free_delivery" {
            freeDelivery = b.Value > 0
        }
    }

    if req.BenefitType == "ride_discount" {
        return &pb.EligibilityResponse{Eligible: discountPercent > 0, DiscountPercent: discountPercent}, nil
    }
    if req.BenefitType == "free_delivery" {
        return &pb.EligibilityResponse{Eligible: freeDelivery, FreeDelivery: freeDelivery}, nil
    }

    return &pb.EligibilityResponse{Eligible: false}, nil
}

// GetUserSubscription returns subscription details for a user
func (s *SubscriptionServer) GetUserSubscription(ctx context.Context, req *pb.GetUserSubscriptionRequest) (*pb.SubscriptionResponse, error) {
    var sub Subscription
    if err := s.DB.Where("user_id = ?", req.UserId).Order("created_at DESC").First(&sub).Error; err != nil {
        return &pb.SubscriptionResponse{Status: "none"}, nil
    }

    var plan Plan
    s.DB.Where("id = ?", sub.PlanID).First(&plan)

    var benefits []*pb.Benefit
    json.Unmarshal([]byte(plan.Benefits), &benefits)

    var discountPercent float64
    var freeDelivery bool
    for _, b := range benefits {
        if b.Type == "ride_discount_percent" {
            discountPercent = b.Value
        }
        if b.Type == "free_delivery" {
            freeDelivery = b.Value > 0
        }
    }

    return &pb.SubscriptionResponse{
        Id:              sub.ID,
        RiderId:         sub.UserID,
        PlanId:          sub.PlanID,
        PlanName:        plan.Name,
        Status:          sub.Status,
        DiscountPercent: discountPercent,
        FreeDelivery:    freeDelivery,
        StartDate:       sub.StartDate.Unix(),
        EndDate:         sub.EndDate.Unix(),
        AutoRenew:       sub.AutoRenew,
    }, nil
}

func generateID() string {
    return "sub_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=subscriptiondb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Plan{}, &Subscription{})

    // Seed default plans if none exist
    var count int64
    db.Model(&Plan{}).Count(&count)
    if count == 0 {
        monthlyBenefits, _ := json.Marshal([]Benefit{
            {Type: "ride_discount_percent", Value: 10},
            {Type: "free_delivery", Value: 1},
        })
        yearlyBenefits, _ := json.Marshal([]Benefit{
            {Type: "ride_discount_percent", Value: 15},
            {Type: "free_delivery", Value: 1},
        })

        db.Create(&Plan{
            ID:            "plan_monthly",
            Name:          "Monthly Pass",
            Description:   "10% off all rides + free delivery on food and groceries",
            PriceGBP:      9.99,
            BillingPeriod: "month",
            Benefits:      string(monthlyBenefits),
            IsActive:      true,
            CreatedAt:     time.Now(),
            UpdatedAt:     time.Now(),
        })
        db.Create(&Plan{
            ID:            "plan_yearly",
            Name:          "Yearly Pass",
            Description:   "15% off all rides + free delivery on food and groceries",
            PriceGBP:      99.99,
            BillingPeriod: "year",
            Benefits:      string(yearlyBenefits),
            IsActive:      true,
            CreatedAt:     time.Now(),
            UpdatedAt:     time.Now(),
        })
        log.Println("Seeded default subscription plans")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterSubscriptionServiceServer(grpcServer, &SubscriptionServer{DB: db})

    lis, err := net.Listen("tcp", ":50061")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Subscription Service running on port 50061")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Subscription Service stopped")
}