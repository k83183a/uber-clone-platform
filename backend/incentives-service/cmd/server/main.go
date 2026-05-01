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

    pb "github.com/uber-clone/incentives-service/proto"
)

// Incentive represents a driver bonus program
type Incentive struct {
    ID               string    `gorm:"primaryKey"`
    Name             string    `gorm:"not null"`
    Description      string
    IncentiveType    string    `gorm:"not null"` // trip_count, rating, cancellation, hourly_guarantee
    Zone             string    // optional zone name
    DayOfWeek        int       `gorm:"default:-1"` // -1 = any day, 0=Sun..6=Sat
    TimeFrom         string    // HH:MM
    TimeTo           string
    GoalTrips        int       // for trip_count type
    MinRating        float64   // minimum rating required (e.g., 4.5)
    MaxCancelRate    float64   // maximum cancellation rate % (e.g., 5.0)
    BonusAmount      float64   `gorm:"not null"`
    StartDate        time.Time
    EndDate          time.Time
    IsActive         bool      `gorm:"default:true"`
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

// DriverIncentiveProgress tracks driver progress for incentives
type DriverIncentiveProgress struct {
    ID               string    `gorm:"primaryKey"`
    DriverID         string    `gorm:"index;not null"`
    IncentiveID      string    `gorm:"index;not null"`
    PeriodStart      time.Time
    PeriodEnd        time.Time
    CompletedTrips   int
    TotalRatingSum   float64
    TotalCancellations int
    IsCompleted      bool      `gorm:"default:false"`
    AwardedAt        *time.Time
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

// DriverRideStats (this would normally be read from ride-service via Kafka, but for MVP we track locally)
// In production, you would consume ride.completed events from Kafka.

// IncentivesServer handles gRPC requests
type IncentivesServer struct {
    pb.UnimplementedIncentivesServiceServer
    DB *gorm.DB
}

// ListActiveIncentives returns incentives available for a driver at a given time/location
func (s *IncentivesServer) ListActiveIncentives(ctx context.Context, req *pb.ListActiveIncentivesRequest) (*pb.ListActiveIncentivesResponse, error) {
    var incentives []Incentive
    now := time.Now()
    query := s.DB.Where("is_active = ? AND start_date <= ? AND end_date >= ?", true, now, now)

    // Filter by zone if provided
    if req.Zone != "" {
        query = query.Where("zone = ? OR zone = ''", req.Zone)
    }

    // Filter by day of week if specific days are set
    if req.DayOfWeek >= 0 {
        query = query.Where("day_of_week = ? OR day_of_week = -1", req.DayOfWeek)
    }

    // Filter by time of day if time range is set
    if req.TimeOfDay != "" {
        // Simple string comparison, ensure HH:MM format
        query = query.Where("(time_from <= ? AND time_to >= ?) OR (time_from IS NULL)", req.TimeOfDay, req.TimeOfDay)
    }

    if err := query.Find(&incentives).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list incentives")
    }

    var pbIncentives []*pb.Incentive
    for _, inc := range incentives {
        pbIncentives = append(pbIncentives, &pb.Incentive{
            Id:            inc.ID,
            Name:          inc.Name,
            Description:   inc.Description,
            IncentiveType: inc.IncentiveType,
            Zone:          inc.Zone,
            GoalTrips:     int32(inc.GoalTrips),
            MinRating:     inc.MinRating,
            MaxCancelRate: inc.MaxCancelRate,
            BonusAmount:   inc.BonusAmount,
        })
    }

    return &pb.ListActiveIncentivesResponse{Incentives: pbIncentives}, nil
}

// GetDriverProgress returns driver's current progress for an incentive
func (s *IncentivesServer) GetDriverProgress(ctx context.Context, req *pb.GetDriverProgressRequest) (*pb.DriverProgressResponse, error) {
    var progress DriverIncentiveProgress
    // Get the most recent period for this driver and incentive (last 7 days)
    if err := s.DB.Where("driver_id = ? AND incentive_id = ?", req.DriverId, req.IncentiveId).Order("period_start DESC").First(&progress).Error; err != nil {
        // If no progress record, create default progress for the last 7 days (simulate fresh start)
        // In production, this would be aggregated from ride events.
        return &pb.DriverProgressResponse{
            CompletedTrips:   0,
            AverageRating:    5.0,
            CancellationRate: 0,
            IsCompleted:      false,
        }, nil
    }

    avgRating := 0.0
    if progress.CompletedTrips > 0 {
        avgRating = progress.TotalRatingSum / float64(progress.CompletedTrips)
    }
    cancelRate := 0.0
    if progress.CompletedTrips > 0 {
        cancelRate = float64(progress.TotalCancellations) / float64(progress.CompletedTrips) * 100
    }

    return &pb.DriverProgressResponse{
        CompletedTrips:   int32(progress.CompletedTrips),
        AverageRating:    avgRating,
        CancellationRate: cancelRate,
        IsCompleted:      progress.IsCompleted,
        PeriodStart:      progress.PeriodStart.Unix(),
        PeriodEnd:        progress.PeriodEnd.Unix(),
    }, nil
}

// CheckAndAwardIncentive is called by a daily cron to evaluate driver progress
func (s *IncentivesServer) CheckAndAwardIncentive(ctx context.Context, req *pb.CheckAndAwardRequest) (*pb.AwardResponse, error) {
    var incentive Incentive
    if err := s.DB.Where("id = ?", req.IncentiveId).First(&incentive).Error; err != nil {
        return nil, status.Error(codes.NotFound, "incentive not found")
    }

    var progress DriverIncentiveProgress
    if err := s.DB.Where("driver_id = ? AND incentive_id = ? AND period_end >= ?", req.DriverId, req.IncentiveId, time.Now().AddDate(0,0,-7)).First(&progress).Error; err != nil {
        return &pb.AwardResponse{Awarded: false, Message: "No progress found"}, nil
    }

    if progress.IsCompleted {
        return &pb.AwardResponse{Awarded: false, Message: "Already awarded"}, nil
    }

    // Calculate average rating and cancellation rate
    avgRating := 0.0
    if progress.CompletedTrips > 0 {
        avgRating = progress.TotalRatingSum / float64(progress.CompletedTrips)
    }
    cancelRate := 0.0
    if progress.CompletedTrips > 0 {
        cancelRate = float64(progress.TotalCancellations) / float64(progress.CompletedTrips) * 100
    }

    if progress.CompletedTrips >= incentive.GoalTrips &&
        avgRating >= incentive.MinRating &&
        cancelRate <= incentive.MaxCancelRate {
        now := time.Now()
        progress.IsCompleted = true
        progress.AwardedAt = &now
        s.DB.Save(&progress)

        // In production: publish event to payment service to add bonus to driver payout
        // s.kafkaProd.PublishIncentiveEarned(req.DriverId, incentive.BonusAmount, incentive.ID)

        return &pb.AwardResponse{
            Awarded:       true,
            BonusAmount:   incentive.BonusAmount,
            Message:       "Bonus awarded! Check your earnings.",
        }, nil
    }

    return &pb.AwardResponse{
        Awarded: false,
        Message: "Conditions not met yet. Keep driving!",
    }, nil
}

// CreateIncentive (admin endpoint)
func (s *IncentivesServer) CreateIncentive(ctx context.Context, req *pb.CreateIncentiveRequest) (*pb.Incentive, error) {
    inc := &Incentive{
        ID:            generateID(),
        Name:          req.Name,
        Description:   req.Description,
        IncentiveType: req.IncentiveType,
        Zone:          req.Zone,
        GoalTrips:     int(req.GoalTrips),
        MinRating:     req.MinRating,
        MaxCancelRate: req.MaxCancelRate,
        BonusAmount:   req.BonusAmount,
        StartDate:     time.Unix(req.StartDate, 0),
        EndDate:       time.Unix(req.EndDate, 0),
        IsActive:      true,
        CreatedAt:     time.Now(),
        UpdatedAt:     time.Now(),
    }

    if err := s.DB.Create(inc).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create incentive")
    }

    return &pb.Incentive{
        Id:            inc.ID,
        Name:          inc.Name,
        Description:   inc.Description,
        IncentiveType: inc.IncentiveType,
        Zone:          inc.Zone,
        GoalTrips:     int32(inc.GoalTrips),
        MinRating:     inc.MinRating,
        MaxCancelRate: inc.MaxCancelRate,
        BonusAmount:   inc.BonusAmount,
    }, nil
}

// UpdateDriverProgress would be called by Kafka consumer on ride.completed events
// For MVP, we simulate a manual update.
func (s *IncentivesServer) UpdateDriverProgress(ctx context.Context, req *pb.UpdateProgressRequest) (*pb.Empty, error) {
    // In production, this would be triggered by ride.completed Kafka events.
    // For now, we just acknowledge.
    log.Printf("Updating progress for driver %s, ride %s", req.DriverId, req.RideId)
    return &pb.Empty{}, nil
}

// Generate example "Complete 100 trips in 7 days with rating >=4.5 and cancellation <=5%"
func (s *IncentivesServer) seedDefaultIncentive() {
    var count int64
    s.DB.Model(&Incentive{}).Where("name = ?", "100 Trips Weekly Bonus").Count(&count)
    if count > 0 {
        return
    }
    inc := &Incentive{
        ID:            generateID(),
        Name:          "100 Trips Weekly Bonus",
        Description:   "Complete 100 trips in 7 days, maintain rating ≥4.5 and cancellation rate ≤5% to earn £300 bonus.",
        IncentiveType: "trip_count_with_quality",
        Zone:          "",
        DayOfWeek:     -1,
        GoalTrips:     100,
        MinRating:     4.5,
        MaxCancelRate: 5.0,
        BonusAmount:   300.0,
        StartDate:     time.Now(),
        EndDate:       time.Now().AddDate(0, 1, 0),
        IsActive:      true,
        CreatedAt:     time.Now(),
        UpdatedAt:     time.Now(),
    }
    s.DB.Create(inc)
    log.Println("Seeded default incentive: 100 trips in 7 days for £300 (with rating and cancellation conditions)")
}

func generateID() string {
    return "inc_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=incentivesdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Incentive{}, &DriverIncentiveProgress{})

    grpcServer := grpc.NewServer()
    server := &IncentivesServer{DB: db}
    pb.RegisterIncentivesServiceServer(grpcServer, server)

    // Seed default incentive
    server.seedDefaultIncentive()

    lis, err := net.Listen("tcp", ":50062")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Incentives Service running on port 50062")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Incentives Service stopped")
}