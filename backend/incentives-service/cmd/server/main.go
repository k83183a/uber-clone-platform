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

type Incentive struct {
    ID            string    `gorm:"primaryKey"`
    Name          string    `gorm:"not null"`
    Description   string
    IncentiveType string    `gorm:"not null"` // trip_count_with_quality, hourly_guarantee
    Zone          string
    DayOfWeek     int       `gorm:"default:-1"`
    TimeFrom      string
    TimeTo        string
    GoalTrips     int
    MinRating     float64   `gorm:"default:0"`
    MaxCancelRate float64   `gorm:"default:100"`
    BonusAmount   float64   `gorm:"not null"`
    StartDate     time.Time
    EndDate       time.Time
    IsActive      bool      `gorm:"default:true"`
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

type DriverProgress struct {
    ID                 string    `gorm:"primaryKey"`
    DriverID           string    `gorm:"index;not null"`
    IncentiveID        string    `gorm:"index;not null"`
    PeriodStart        time.Time
    PeriodEnd          time.Time
    CompletedTrips     int
    TotalRatingSum     float64
    TotalCancelledByDriver int
    IsCompleted        bool      `gorm:"default:false"`
    AwardedAt          *time.Time
    CreatedAt          time.Time
    UpdatedAt          time.Time
}

type IncentivesServer struct {
    pb.UnimplementedIncentivesServiceServer
    DB *gorm.DB
}

// ListActiveIncentives - List incentives available for a driver
func (s *IncentivesServer) ListActiveIncentives(ctx context.Context, req *pb.ListActiveIncentivesRequest) (*pb.ListActiveIncentivesResponse, error) {
    var incentives []Incentive
    now := time.Now()
    query := s.DB.Where("is_active = ? AND start_date <= ? AND end_date >= ?", true, now, now)

    if req.Zone != "" {
        query = query.Where("zone = ? OR zone = ''", req.Zone)
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
            GoalTrips:     int32(inc.GoalTrips),
            MinRating:     inc.MinRating,
            MaxCancelRate: inc.MaxCancelRate,
            BonusAmount:   inc.BonusAmount,
        })
    }

    return &pb.ListActiveIncentivesResponse{Incentives: pbIncentives}, nil
}

// GetDriverProgress - Get driver's progress for an incentive
func (s *IncentivesServer) GetDriverProgress(ctx context.Context, req *pb.GetDriverProgressRequest) (*pb.DriverProgressResponse, error) {
    var progress DriverProgress
    endDate := time.Now()
    startDate := endDate.AddDate(0, 0, -7) // Last 7 days

    if err := s.DB.Where("driver_id = ? AND incentive_id = ? AND period_start >= ?", req.DriverId, req.IncentiveId, startDate).First(&progress).Error; err != nil {
        // Create progress record if not exists
        progress = DriverProgress{
            ID:             generateID(),
            DriverID:       req.DriverId,
            IncentiveID:    req.IncentiveId,
            PeriodStart:    startDate,
            PeriodEnd:      endDate,
            CompletedTrips: 0,
            TotalRatingSum: 0,
            CreatedAt:      time.Now(),
            UpdatedAt:      time.Now(),
        }
        s.DB.Create(&progress)
    }

    avgRating := 0.0
    if progress.CompletedTrips > 0 {
        avgRating = progress.TotalRatingSum / float64(progress.CompletedTrips)
    }
    cancelRate := 0.0
    if progress.CompletedTrips > 0 {
        cancelRate = float64(progress.TotalCancelledByDriver) / float64(progress.CompletedTrips+progress.TotalCancelledByDriver) * 100
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

// CheckAndAwardIncentive - Check if driver qualifies for bonus and award
func (s *IncentivesServer) CheckAndAwardIncentive(ctx context.Context, req *pb.CheckAndAwardRequest) (*pb.AwardResponse, error) {
    var incentive Incentive
    if err := s.DB.Where("id = ?", req.IncentiveId).First(&incentive).Error; err != nil {
        return nil, status.Error(codes.NotFound, "incentive not found")
    }

    var progress DriverProgress
    if err := s.DB.Where("driver_id = ? AND incentive_id = ?", req.DriverId, req.IncentiveId).First(&progress).Error; err != nil {
        return &pb.AwardResponse{Awarded: false, Message: "No progress found"}, nil
    }

    if progress.IsCompleted {
        return &pb.AwardResponse{Awarded: false, Message: "Already awarded"}, nil
    }

    avgRating := 0.0
    if progress.CompletedTrips > 0 {
        avgRating = progress.TotalRatingSum / float64(progress.CompletedTrips)
    }
    cancelRate := 0.0
    if progress.CompletedTrips > 0 {
        cancelRate = float64(progress.TotalCancelledByDriver) / float64(progress.CompletedTrips+progress.TotalCancelledByDriver) * 100
    }

    if progress.CompletedTrips >= incentive.GoalTrips &&
        avgRating >= incentive.MinRating &&
        cancelRate <= incentive.MaxCancelRate {
        now := time.Now()
        progress.IsCompleted = true
        progress.AwardedAt = &now
        s.DB.Save(&progress)

        log.Printf("🏆 Driver %s earned £%.2f for completing %d trips with rating %.1f and cancel rate %.1f%%",
            req.DriverId, incentive.BonusAmount, progress.CompletedTrips, avgRating, cancelRate)

        return &pb.AwardResponse{
            Awarded:     true,
            BonusAmount: incentive.BonusAmount,
            Message:     "Bonus awarded! Check your earnings.",
        }, nil
    }

    remaining := incentive.GoalTrips - progress.CompletedTrips
    return &pb.AwardResponse{
        Awarded: false,
        Message: fmt.Sprintf("You need %d more trips. Keep your rating above %.1f and cancellation below %.1f%%",
            remaining, incentive.MinRating, incentive.MaxCancelRate),
    }, nil
}

// UpdateDriverProgress - Update driver's progress (called by Kafka consumer on ride.completed)
func (s *IncentivesServer) UpdateDriverProgress(ctx context.Context, req *pb.UpdateProgressRequest) (*pb.Empty, error) {
    var incentives []Incentive
    now := time.Now()
    s.DB.Where("is_active = ? AND incentive_type = ? AND start_date <= ? AND end_date >= ?",
        true, "trip_count_with_quality", now, now).Find(&incentives)

    for _, inc := range incentives {
        var progress DriverProgress
        endDate := now
        startDate := endDate.AddDate(0, 0, -7)

        if err := s.DB.Where("driver_id = ? AND incentive_id = ? AND period_start >= ?", req.DriverId, inc.ID, startDate).First(&progress).Error; err != nil {
            progress = DriverProgress{
                ID:             generateID(),
                DriverID:       req.DriverId,
                IncentiveID:    inc.ID,
                PeriodStart:    startDate,
                PeriodEnd:      endDate,
                CompletedTrips: 0,
                TotalRatingSum: 0,
                CreatedAt:      time.Now(),
                UpdatedAt:      time.Now(),
            }
            s.DB.Create(&progress)
        }

        if progress.IsCompleted {
            continue
        }

        // Update progress
        progress.CompletedTrips++
        progress.TotalRatingSum += req.DriverRating
        if req.WasCancelledByDriver {
            progress.TotalCancelledByDriver++
        }
        progress.UpdatedAt = time.Now()
        s.DB.Save(&progress)
    }

    return &pb.Empty{}, nil
}

// CreateIncentive - Create a new incentive (admin)
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
        GoalTrips:     int32(inc.GoalTrips),
        MinRating:     inc.MinRating,
        MaxCancelRate: inc.MaxCancelRate,
        BonusAmount:   inc.BonusAmount,
    }, nil
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

    db.AutoMigrate(&Incentive{}, &DriverProgress{})

    // Seed default incentive: 100 trips in 7 days with rating and cancel rules
    var count int64
    db.Model(&Incentive{}).Count(&count)
    if count == 0 {
        db.Create(&Incentive{
            ID:            generateID(),
            Name:          "100 Trips Weekly Bonus",
            Description:   "Complete 100 trips in 7 days, maintain rating ≥4.5 and cancellation ≤5% to earn £300",
            IncentiveType: "trip_count_with_quality",
            GoalTrips:     100,
            MinRating:     4.5,
            MaxCancelRate: 5.0,
            BonusAmount:   300.0,
            StartDate:     time.Now(),
            EndDate:       time.Now().AddDate(0, 1, 0),
            IsActive:      true,
            CreatedAt:     time.Now(),
            UpdatedAt:     time.Now(),
        })
        log.Println("Seeded default incentive: 100 trips in 7 days for £300 (with rating and cancellation rules)")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterIncentivesServiceServer(grpcServer, &IncentivesServer{DB: db})

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
}