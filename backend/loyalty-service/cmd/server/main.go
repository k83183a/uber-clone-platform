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

    pb "github.com/uber-clone/loyalty-service/proto"
)

type LoyaltyAccount struct {
    ID             string    `gorm:"primaryKey"`
    UserID         string    `gorm:"uniqueIndex;not null"`
    PointsBalance  int       `gorm:"default:0"`
    LifetimePoints int       `gorm:"default:0"`
    Tier           string    `gorm:"default:'bronze'"`
    TierUpdatedAt  time.Time
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type LoyaltyTransaction struct {
    ID              string    `gorm:"primaryKey"`
    AccountID       string    `gorm:"index;not null"`
    TransactionType string    `gorm:"not null"` // earn, redeem, expire
    Points          int       `gorm:"not null"`
    Source          string
    SourceID        string    `gorm:"index"`
    Description     string
    CreatedAt       time.Time
}

type Tier struct {
    Name            string    `gorm:"primaryKey"`
    MinPoints       int       `gorm:"not null"`
    PointMultiplier float64   `gorm:"default:1"`
    Benefits        string    `gorm:"type:text"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type Reward struct {
    ID          string     `gorm:"primaryKey"`
    Name        string     `gorm:"not null"`
    Description string
    PointsCost  int        `gorm:"not null"`
    RewardType  string     `gorm:"not null"`
    RewardValue float64
    PartnerCode string
    IsActive    bool       `gorm:"default:true"`
    ExpiresAt   *time.Time
    CreatedAt   time.Time
}

type RewardRedemption struct {
    ID             string     `gorm:"primaryKey"`
    RewardID       string     `gorm:"index;not null"`
    AccountID      string     `gorm:"index;not null"`
    RedemptionCode string     `gorm:"uniqueIndex"`
    Used           bool       `gorm:"default:false"`
    RedeemedAt     time.Time
    ExpiresAt      *time.Time
}

type LoyaltyServer struct {
    pb.UnimplementedLoyaltyServiceServer
    DB *gorm.DB
}

// GetAccount - Get loyalty account details
func (s *LoyaltyServer) GetAccount(ctx context.Context, req *pb.GetAccountRequest) (*pb.AccountResponse, error) {
    account, err := s.getOrCreateAccount(req.UserId)
    if err != nil {
        return nil, err
    }

    nextTier := s.getNextTier(account.PointsBalance)
    pointsToNext := 0
    nextTierName := ""
    nextTierMultiplier := 1.0
    if nextTier != nil {
        pointsToNext = nextTier.MinPoints - account.PointsBalance
        nextTierName = nextTier.Name
        nextTierMultiplier = nextTier.PointMultiplier
    }

    return &pb.AccountResponse{
        PointsBalance:     int32(account.PointsBalance),
        LifetimePoints:    int32(account.LifetimePoints),
        Tier:              account.Tier,
        PointsToNextTier:  int32(pointsToNext),
        NextTierName:      nextTierName,
        NextTierMultiplier: nextTierMultiplier,
    }, nil
}

// GetTransactions - Get transaction history
func (s *LoyaltyServer) GetTransactions(ctx context.Context, req *pb.GetTransactionsRequest) (*pb.TransactionsResponse, error) {
    account, err := s.getOrCreateAccount(req.UserId)
    if err != nil {
        return nil, err
    }

    var transactions []LoyaltyTransaction
    query := s.DB.Where("account_id = ?", account.ID).Order("created_at DESC")
    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&transactions).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get transactions")
    }

    var total int64
    s.DB.Model(&LoyaltyTransaction{}).Where("account_id = ?", account.ID).Count(&total)

    var pbTransactions []*pb.Transaction
    for _, t := range transactions {
        pbTransactions = append(pbTransactions, &pb.Transaction{
            Id:        t.ID,
            Type:      t.TransactionType,
            Points:    int32(t.Points),
            Source:    t.Source,
            CreatedAt: t.CreatedAt.Unix(),
        })
    }

    return &pb.TransactionsResponse{Transactions: pbTransactions, Total: int32(total)}, nil
}

// EarnPoints - Add points to user account
func (s *LoyaltyServer) EarnPoints(ctx context.Context, req *pb.EarnPointsRequest) (*pb.Empty, error) {
    account, err := s.getOrCreateAccount(req.UserId)
    if err != nil {
        return nil, err
    }

    multiplier := s.getTierMultiplierByName(account.Tier)
    earnedPoints := int(float64(req.BasePoints) * multiplier)

    account.PointsBalance += earnedPoints
    account.LifetimePoints += earnedPoints
    account.UpdatedAt = time.Now()

    // Check tier upgrade
    newTier := s.calculateTier(account.PointsBalance)
    if newTier != account.Tier {
        account.Tier = newTier
        account.TierUpdatedAt = time.Now()
        log.Printf("🎉 User %s upgraded to %s tier!", req.UserId, newTier)
    }

    if err := s.DB.Save(account).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update account")
    }

    tx := &LoyaltyTransaction{
        ID:              generateID(),
        AccountID:       account.ID,
        TransactionType: "earn",
        Points:          earnedPoints,
        Source:          req.Source,
        SourceID:        req.SourceId,
        CreatedAt:       time.Now(),
    }
    s.DB.Create(tx)

    return &pb.Empty{}, nil
}

// RedeemReward - Exchange points for a reward
func (s *LoyaltyServer) RedeemReward(ctx context.Context, req *pb.RedeemRewardRequest) (*pb.RedeemResponse, error) {
    var reward Reward
    if err := s.DB.Where("id = ? AND is_active = ?", req.RewardId, true).First(&reward).Error; err != nil {
        return nil, status.Error(codes.NotFound, "reward not found")
    }

    account, err := s.getOrCreateAccount(req.UserId)
    if err != nil {
        return nil, err
    }

    if account.PointsBalance < reward.PointsCost {
        return nil, status.Error(codes.FailedPrecondition, "insufficient points")
    }

    account.PointsBalance -= reward.PointsCost
    account.UpdatedAt = time.Now()
    s.DB.Save(account)

    code := generateRedemptionCode()
    redemption := &RewardRedemption{
        ID:             generateID(),
        RewardID:       reward.ID,
        AccountID:      account.ID,
        RedemptionCode: code,
        Used:           false,
        RedeemedAt:     time.Now(),
    }
    s.DB.Create(redemption)

    tx := &LoyaltyTransaction{
        ID:              generateID(),
        AccountID:       account.ID,
        TransactionType: "redeem",
        Points:          -reward.PointsCost,
        Source:          "reward",
        SourceID:        reward.ID,
        CreatedAt:       time.Now(),
    }
    s.DB.Create(tx)

    return &pb.RedeemResponse{
        RedemptionCode: code,
        Message:        "Reward redeemed successfully!",
    }, nil
}

// ListRewards - List available rewards
func (s *LoyaltyServer) ListRewards(ctx context.Context, req *pb.Empty) (*pb.RewardsResponse, error) {
    var rewards []Reward
    now := time.Now()
    s.DB.Where("is_active = ?", true).Where("expires_at IS NULL OR expires_at > ?", now).Find(&rewards)

    var pbRewards []*pb.Reward
    for _, r := range rewards {
        pbRewards = append(pbRewards, &pb.Reward{
            Id:          r.ID,
            Name:        r.Name,
            Description: r.Description,
            PointsCost:  int32(r.PointsCost),
            RewardType:  r.RewardType,
            RewardValue: r.RewardValue,
        })
    }

    return &pb.RewardsResponse{Rewards: pbRewards}, nil
}

// GetTiers - Get all loyalty tiers
func (s *LoyaltyServer) GetTiers(ctx context.Context, req *pb.Empty) (*pb.TiersResponse, error) {
    var tiers []Tier
    if err := s.DB.Order("min_points ASC").Find(&tiers).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get tiers")
    }

    var pbTiers []*pb.Tier
    for _, t := range tiers {
        pbTiers = append(pbTiers, &pb.Tier{
            Name:            t.Name,
            MinPoints:       int32(t.MinPoints),
            PointMultiplier: t.PointMultiplier,
        })
    }

    return &pb.TiersResponse{Tiers: pbTiers}, nil
}

func (s *LoyaltyServer) getOrCreateAccount(userID string) (*LoyaltyAccount, error) {
    var account LoyaltyAccount
    if err := s.DB.Where("user_id = ?", userID).First(&account).Error; err != nil {
        account = LoyaltyAccount{
            ID:             generateID(),
            UserID:         userID,
            PointsBalance:  0,
            LifetimePoints: 0,
            Tier:           "bronze",
            TierUpdatedAt:  time.Now(),
            CreatedAt:      time.Now(),
            UpdatedAt:      time.Now(),
        }
        if err := s.DB.Create(&account).Error; err != nil {
            return nil, status.Error(codes.Internal, "failed to create account")
        }
    }
    return &account, nil
}

func (s *LoyaltyServer) calculateTier(points int) string {
    var tiers []Tier
    s.DB.Order("min_points DESC").Find(&tiers)
    for _, t := range tiers {
        if points >= t.MinPoints {
            return t.Name
        }
    }
    return "bronze"
}

func (s *LoyaltyServer) getNextTier(points int) *Tier {
    var tiers []Tier
    s.DB.Order("min_points ASC").Find(&tiers)
    for _, t := range tiers {
        if points < t.MinPoints {
            return &t
        }
    }
    return nil
}

func (s *LoyaltyServer) getTierMultiplierByName(tierName string) float64 {
    var tier Tier
    if err := s.DB.Where("name = ?", tierName).First(&tier).Error; err != nil {
        return 1.0
    }
    return tier.PointMultiplier
}

func generateID() string {
    return "lty_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateRedemptionCode() string {
    return "LOYALTY_" + time.Now().Format("20060102") + randomString(8)
}

func randomString(n int) string {
    const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
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
        dsn = "host=postgres user=postgres password=postgres dbname=loyaltydb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&LoyaltyAccount{}, &LoyaltyTransaction{}, &Tier{}, &Reward{}, &RewardRedemption{})

    // Seed tiers
    var count int64
    db.Model(&Tier{}).Count(&count)
    if count == 0 {
        tiers := []Tier{
            {Name: "bronze", MinPoints: 0, PointMultiplier: 1.0, Benefits: `{"free_delivery":false,"discount_percent":0}`, CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {Name: "silver", MinPoints: 2000, PointMultiplier: 1.5, Benefits: `{"free_delivery":true,"discount_percent":5}`, CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {Name: "gold", MinPoints: 5000, PointMultiplier: 2.0, Benefits: `{"free_delivery":true,"discount_percent":10}`, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        }
        db.Create(&tiers)
        log.Println("Seeded loyalty tiers: Bronze, Silver, Gold")
    }

    // Seed sample rewards
    db.Model(&Reward{}).Count(&count)
    if count == 0 {
        rewards := []Reward{
            {ID: generateID(), Name: "£5 Ride Credit", Description: "Get £5 off your next ride", PointsCost: 500, RewardType: "discount_voucher", RewardValue: 5, IsActive: true, CreatedAt: time.Now()},
            {ID: generateID(), Name: "Free Delivery", Description: "Free delivery on your next food order", PointsCost: 300, RewardType: "discount_voucher", RewardValue: 3.99, IsActive: true, CreatedAt: time.Now()},
            {ID: generateID(), Name: "£10 Voucher", Description: "£10 off any service", PointsCost: 1000, RewardType: "discount_voucher", RewardValue: 10, IsActive: true, CreatedAt: time.Now()},
        }
        db.Create(&rewards)
        log.Println("Seeded sample rewards")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterLoyaltyServiceServer(grpcServer, &LoyaltyServer{DB: db})

    lis, err := net.Listen("tcp", ":50067")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Loyalty Service running on port 50067")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}