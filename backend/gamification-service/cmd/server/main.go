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

    pb "github.com/uber-clone/gamification-service/proto"
)

type Challenge struct {
    ID            string    `gorm:"primaryKey"`
    Name          string    `gorm:"not null"`
    Description   string
    ChallengeType string    `gorm:"not null"` // trip_count, rating, acceptance_rate
    Goal          int       `gorm:"not null"`
    BonusAmount   float64   `gorm:"not null"`
    StartDate     time.Time
    EndDate       time.Time
    IsActive      bool      `gorm:"default:true"`
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

type DriverChallengeProgress struct {
    ID          string     `gorm:"primaryKey"`
    DriverID    string     `gorm:"index;not null"`
    ChallengeID string     `gorm:"index;not null"`
    Progress    int        `gorm:"default:0"`
    Completed   bool       `gorm:"default:false"`
    CompletedAt *time.Time
    AwardedAt   *time.Time
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type LeaderboardEntry struct {
    ID            string    `gorm:"primaryKey"`
    Period        string    `gorm:"index"`
    DriverID      string    `gorm:"index"`
    DriverName    string
    TotalTrips    int
    TotalEarnings float64
    TotalRating   float64
    Rank          int
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

type GamificationServer struct {
    pb.UnimplementedGamificationServiceServer
    DB *gorm.DB
}

// ListActiveChallenges - List active challenges for a driver
func (s *GamificationServer) ListActiveChallenges(ctx context.Context, req *pb.ListChallengesRequest) (*pb.ListChallengesResponse, error) {
    var challenges []Challenge
    now := time.Now()
    if err := s.DB.Where("is_active = ? AND start_date <= ? AND end_date >= ?", true, now, now).Find(&challenges).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list challenges")
    }

    var pbChallenges []*pb.Challenge
    for _, c := range challenges {
        var progress DriverChallengeProgress
        s.DB.Where("driver_id = ? AND challenge_id = ?", req.DriverId, c.ID).First(&progress)

        pbChallenges = append(pbChallenges, &pb.Challenge{
            Id:          c.ID,
            Name:        c.Name,
            Description: c.Description,
            Goal:        int32(c.Goal),
            BonusAmount: c.BonusAmount,
            Progress:    int32(progress.Progress),
            Completed:   progress.Completed,
            StartDate:   c.StartDate.Unix(),
            EndDate:     c.EndDate.Unix(),
        })
    }

    return &pb.ListChallengesResponse{Challenges: pbChallenges}, nil
}

// GetLeaderboard - Get driver leaderboard
func (s *GamificationServer) GetLeaderboard(ctx context.Context, req *pb.GetLeaderboardRequest) (*pb.LeaderboardResponse, error) {
    var entries []LeaderboardEntry
    query := s.DB.Where("period = ?", req.Period).Order("rank ASC")
    if req.Limit > 0 {
        query = query.Limit(int(req.Limit))
    }
    if err := query.Find(&entries).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get leaderboard")
    }

    var pbEntries []*pb.LeaderboardEntry
    for _, e := range entries {
        pbEntries = append(pbEntries, &pb.LeaderboardEntry{
            DriverId:    e.DriverID,
            DriverName:  e.DriverName,
            Rank:        int32(e.Rank),
            TotalTrips:  int32(e.TotalTrips),
            TotalEarnings: e.TotalEarnings,
        })
    }

    return &pb.LeaderboardResponse{Entries: pbEntries}, nil
}

// UpdateDriverProgress - Update driver's challenge progress (Kafka consumer)
func (s *GamificationServer) UpdateDriverProgress(ctx context.Context, req *pb.UpdateProgressRequest) (*pb.Empty, error) {
    var challenges []Challenge
    now := time.Now()
    s.DB.Where("is_active = ? AND start_date <= ? AND end_date >= ?", true, now, now).Find(&challenges)

    for _, c := range challenges {
        var progress DriverChallengeProgress
        result := s.DB.Where("driver_id = ? AND challenge_id = ?", req.DriverId, c.ID).First(&progress)
        if result.Error != nil {
            progress = DriverChallengeProgress{
                ID:          generateID(),
                DriverID:    req.DriverId,
                ChallengeID: c.ID,
                Progress:    0,
                Completed:   false,
                CreatedAt:   time.Now(),
                UpdatedAt:   time.Now(),
            }
            s.DB.Create(&progress)
        }

        if progress.Completed {
            continue
        }

        progress.Progress++
        if progress.Progress >= c.Goal {
            progress.Completed = true
            now := time.Now()
            progress.CompletedAt = &now
            log.Printf("🏆 Driver %s completed challenge %s! Bonus: £%.2f", req.DriverId, c.ID, c.BonusAmount)
        }
        progress.UpdatedAt = time.Now()
        s.DB.Save(&progress)
    }

    return &pb.Empty{}, nil
}

// CreateChallenge - Create a new challenge (admin)
func (s *GamificationServer) CreateChallenge(ctx context.Context, req *pb.CreateChallengeRequest) (*pb.Challenge, error) {
    challenge := &Challenge{
        ID:            generateID(),
        Name:          req.Name,
        Description:   req.Description,
        ChallengeType: req.ChallengeType,
        Goal:          int(req.Goal),
        BonusAmount:   req.BonusAmount,
        StartDate:     time.Unix(req.StartDate, 0),
        EndDate:       time.Unix(req.EndDate, 0),
        IsActive:      true,
        CreatedAt:     time.Now(),
        UpdatedAt:     time.Now(),
    }
    if err := s.DB.Create(challenge).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create challenge")
    }

    return &pb.Challenge{
        Id:          challenge.ID,
        Name:        challenge.Name,
        Description: challenge.Description,
        Goal:        int32(challenge.Goal),
        BonusAmount: challenge.BonusAmount,
        StartDate:   challenge.StartDate.Unix(),
        EndDate:     challenge.EndDate.Unix(),
    }, nil
}

// GetDriverStats - Get driver's gamification stats
func (s *GamificationServer) GetDriverStats(ctx context.Context, req *pb.GetDriverStatsRequest) (*pb.DriverStatsResponse, error) {
    var completedChallenges int64
    s.DB.Model(&DriverChallengeProgress{}).Where("driver_id = ? AND completed = ?", req.DriverId, true).Count(&completedChallenges)

    var leaderboardEntry LeaderboardEntry
    s.DB.Where("driver_id = ? AND period = ?", req.DriverId, "weekly").First(&leaderboardEntry)

    return &pb.DriverStatsResponse{
        CompletedChallenges: int32(completedChallenges),
        CurrentRank:         int32(leaderboardEntry.Rank),
        TotalTrips:          int32(leaderboardEntry.TotalTrips),
    }, nil
}

func generateID() string {
    return "gam_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=gamificationdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Challenge{}, &DriverChallengeProgress{}, &LeaderboardEntry{})

    // Seed sample challenge
    var count int64
    db.Model(&Challenge{}).Count(&count)
    if count == 0 {
        db.Create(&Challenge{
            ID:            generateID(),
            Name:          "Weekend Warrior",
            Description:   "Complete 20 trips this weekend and earn a £50 bonus!",
            ChallengeType: "trip_count",
            Goal:          20,
            BonusAmount:   50.0,
            StartDate:     time.Now(),
            EndDate:       time.Now().AddDate(0, 0, 2),
            IsActive:      true,
            CreatedAt:     time.Now(),
            UpdatedAt:     time.Now(),
        })
        log.Println("Seeded default challenge: Weekend Warrior")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterGamificationServiceServer(grpcServer, &GamificationServer{DB: db})

    lis, err := net.Listen("tcp", ":50066")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Gamification Service running on port 50066")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}