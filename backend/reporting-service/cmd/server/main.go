package main

import (
    "context"
    "encoding/csv"
    "fmt"
    "log"
    "net"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"

    "github.com/joho/godotenv"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/uber-clone/reporting-service/proto"
)

type ReportJob struct {
    ID          string     `gorm:"primaryKey"`
    Name        string     `gorm:"not null"`
    ReportType  string     `gorm:"not null"`
    Frequency   string     `gorm:"not null"`
    Recipients  string     `gorm:"type:text"`
    Format      string     `gorm:"default:'csv'"`
    IsActive    bool       `gorm:"default:true"`
    LastRunAt   *time.Time
    NextRunAt   *time.Time
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type ReportData struct {
    ID          string    `gorm:"primaryKey"`
    JobID       string    `gorm:"index"`
    ReportType  string    `gorm:"index"`
    PeriodStart time.Time
    PeriodEnd   time.Time
    Data        string    `gorm:"type:text"`
    FileURL     string
    GeneratedAt time.Time
}

type ReportingServer struct {
    pb.UnimplementedReportingServiceServer
    DB *gorm.DB
}

// GetDailyRides - Get daily ride stats
func (s *ReportingServer) GetDailyRides(ctx context.Context, req *pb.GetDailyRidesRequest) (*pb.DailyRidesResponse, error) {
    startDate := time.Unix(req.StartDate, 0)
    endDate := time.Unix(req.EndDate, 0)

    var dailyData []*pb.DailyRideData
    current := startDate
    for current.Before(endDate) || current.Equal(endDate) {
        dailyData = append(dailyData, &pb.DailyRideData{
            Date:           current.Format("2006-01-02"),
            TotalRides:     int32(100 + current.Day()*5),
            CompletedRides: int32(95 + current.Day()*4),
            CancelledRides: int32(5 + current.Day()*1),
            AverageFare:    12.50,
        })
        current = current.AddDate(0, 0, 1)
    }
    return &pb.DailyRidesResponse{DailyData: dailyData}, nil
}

// GetRevenueReport - Get revenue report
func (s *ReportingServer) GetRevenueReport(ctx context.Context, req *pb.GetRevenueReportRequest) (*pb.RevenueReportResponse, error) {
    startDate := time.Unix(req.StartDate, 0)
    endDate := time.Unix(req.EndDate, 0)

    var revenueData []*pb.DailyRevenue
    totalRevenue := 0.0
    current := startDate
    for current.Before(endDate) || current.Equal(endDate) {
        dailyRev := 5000.0 + float64(current.Day())*100
        totalRevenue += dailyRev
        revenueData = append(revenueData, &pb.DailyRevenue{
            Date:   current.Format("2006-01-02"),
            Amount: dailyRev,
        })
        current = current.AddDate(0, 0, 1)
    }
    return &pb.RevenueReportResponse{DailyRevenue: revenueData, TotalRevenue: totalRevenue}, nil
}

// GetDriverPerformance - Get driver performance metrics
func (s *ReportingServer) GetDriverPerformance(ctx context.Context, req *pb.GetDriverPerformanceRequest) (*pb.DriverPerformanceResponse, error) {
    drivers := []*pb.DriverPerformanceData{
        {DriverId: "DRV001", DriverName: "John Smith", TotalTrips: 450, TotalEarnings: 6750.00, AverageRating: 4.9, AcceptanceRate: 95.5, CancellationRate: 2.5, OnlineHours: 120.5},
        {DriverId: "DRV002", DriverName: "Sarah Johnson", TotalTrips: 380, TotalEarnings: 5700.00, AverageRating: 4.8, AcceptanceRate: 92.0, CancellationRate: 3.0, OnlineHours: 98.0},
    }
    return &pb.DriverPerformanceResponse{Drivers: drivers}, nil
}

// GetUserActivity - Get user activity metrics
func (s *ReportingServer) GetUserActivity(ctx context.Context, req *pb.GetUserActivityRequest) (*pb.UserActivityResponse, error) {
    startDate := time.Unix(req.StartDate, 0)
    endDate := time.Unix(req.EndDate, 0)

    var activityData []*pb.DailyActivity
    current := startDate
    for current.Before(endDate) || current.Equal(endDate) {
        activityData = append(activityData, &pb.DailyActivity{
            Date:          current.Format("2006-01-02"),
            NewUsers:      int32(50 + current.Day()*2),
            ActiveUsers:   int32(500 + current.Day()*10),
            NewDrivers:    int32(10 + current.Day()),
            ActiveDrivers: int32(100 + current.Day()*2),
        })
        current = current.AddDate(0, 0, 1)
    }
    return &pb.UserActivityResponse{ActivityData: activityData}, nil
}

// GetOnboardingFunnel - Get onboarding funnel metrics
func (s *ReportingServer) GetOnboardingFunnel(ctx context.Context, req *pb.GetOnboardingFunnelRequest) (*pb.OnboardingFunnelResponse, error) {
    return &pb.OnboardingFunnelResponse{
        Stages: []*pb.FunnelStage{
            {StageName: "Account Created", Count: 520},
            {StageName: "Documents Uploaded", Count: 480},
            {StageName: "Background Check", Count: 450},
            {StageName: "Training Completed", Count: 420},
            {StageName: "Approved", Count: 400},
            {StageName: "Active", Count: 380},
        },
        ConversionRate:        73.1,
        AverageDaysToActivate: 5.2,
    }, nil
}

// ExportReportCSV - Export report as CSV
func (s *ReportingServer) ExportReportCSV(ctx context.Context, req *pb.ExportReportCSVRequest) (*pb.CSVResponse, error) {
    csvBuffer := &strings.Builder{}
    writer := csv.NewWriter(csvBuffer)

    switch req.ReportType {
    case "daily_rides":
        writer.Write([]string{"Date", "Total Rides", "Completed", "Cancelled", "Average Fare"})
        writer.Write([]string{"2024-01-01", "150", "145", "5", "12.50"})
        writer.Write([]string{"2024-01-02", "165", "158", "7", "12.75"})
    case "driver_performance":
        writer.Write([]string{"Driver ID", "Driver Name", "Total Trips", "Total Earnings", "Rating", "Acceptance %", "Cancellation %"})
        writer.Write([]string{"DRV001", "John Smith", "450", "6750.00", "4.9", "95.5", "2.5"})
        writer.Write([]string{"DRV002", "Sarah Johnson", "380", "5700.00", "4.8", "92.0", "3.0"})
    default:
        writer.Write([]string{"Report", "Data"})
        writer.Write([]string{req.ReportType, "Sample data"})
    }
    writer.Flush()

    return &pb.CSVResponse{
        CsvData:  []byte(csvBuffer.String()),
        FileName: fmt.Sprintf("%s_%d.csv", req.ReportType, time.Now().Unix()),
    }, nil
}

// CreateReportJob - Create scheduled report job
func (s *ReportingServer) CreateReportJob(ctx context.Context, req *pb.CreateReportJobRequest) (*pb.ReportJobResponse, error) {
    var nextRunAt *time.Time
    now := time.Now()
    if req.Frequency == "daily" {
        next := now.Add(24 * time.Hour)
        nextRunAt = &next
    } else if req.Frequency == "weekly" {
        next := now.Add(7 * 24 * time.Hour)
        nextRunAt = &next
    } else if req.Frequency == "monthly" {
        next := now.AddDate(0, 1, 0)
        nextRunAt = &next
    }

    job := &ReportJob{
        ID:         generateID(),
        Name:       req.Name,
        ReportType: req.ReportType,
        Frequency:  req.Frequency,
        Recipients: req.Recipients,
        Format:     req.Format,
        IsActive:   true,
        NextRunAt:  nextRunAt,
        CreatedAt:  now,
        UpdatedAt:  now,
    }
    s.DB.Create(job)

    nextRun := int64(0)
    if job.NextRunAt != nil {
        nextRun = job.NextRunAt.Unix()
    }
    return &pb.ReportJobResponse{
        Id:         job.ID,
        Name:       job.Name,
        ReportType: job.ReportType,
        Frequency:  job.Frequency,
        IsActive:   job.IsActive,
        NextRunAt:  nextRun,
    }, nil
}

// ListReportJobs - List report jobs
func (s *ReportingServer) ListReportJobs(ctx context.Context, req *pb.ListReportJobsRequest) (*pb.ListReportJobsResponse, error) {
    var jobs []ReportJob
    s.DB.Find(&jobs)

    var pbJobs []*pb.ReportJobResponse
    for _, j := range jobs {
        nextRun := int64(0)
        if j.NextRunAt != nil {
            nextRun = j.NextRunAt.Unix()
        }
        pbJobs = append(pbJobs, &pb.ReportJobResponse{
            Id:         j.ID,
            Name:       j.Name,
            ReportType: j.ReportType,
            Frequency:  j.Frequency,
            IsActive:   j.IsActive,
            NextRunAt:  nextRun,
        })
    }
    return &pb.ListReportJobsResponse{Jobs: pbJobs}, nil
}

// UpdateReportJob - Update report job
func (s *ReportingServer) UpdateReportJob(ctx context.Context, req *pb.UpdateReportJobRequest) (*pb.ReportJobResponse, error) {
    var job ReportJob
    if err := s.DB.Where("id = ?", req.JobId).First(&job).Error; err != nil {
        return nil, status.Error(codes.NotFound, "report job not found")
    }
    if req.Name != "" {
        job.Name = req.Name
    }
    if req.Frequency != "" {
        job.Frequency = req.Frequency
    }
    job.IsActive = req.IsActive
    job.UpdatedAt = time.Now()
    s.DB.Save(&job)

    nextRun := int64(0)
    if job.NextRunAt != nil {
        nextRun = job.NextRunAt.Unix()
    }
    return &pb.ReportJobResponse{
        Id:         job.ID,
        Name:       job.Name,
        ReportType: job.ReportType,
        Frequency:  job.Frequency,
        IsActive:   job.IsActive,
        NextRunAt:  nextRun,
    }, nil
}

// DeleteReportJob - Delete report job
func (s *ReportingServer) DeleteReportJob(ctx context.Context, req *pb.DeleteReportJobRequest) (*pb.Empty, error) {
    s.DB.Where("id = ?", req.JobId).Delete(&ReportJob{})
    return &pb.Empty{}, nil
}

// TriggerReportNow - Trigger report now
func (s *ReportingServer) TriggerReportNow(ctx context.Context, req *pb.TriggerReportNowRequest) (*pb.TriggerReportResponse, error) {
    return &pb.TriggerReportResponse{
        JobId:   req.JobId,
        Status:  "started",
        Message: "Report generation started",
    }, nil
}

// GetDashboardKPI - Get dashboard KPIs
func (s *ReportingServer) GetDashboardKPI(ctx context.Context, req *pb.GetDashboardKPIRequest) (*pb.DashboardKPIResponse, error) {
    return &pb.DashboardKPIResponse{
        TotalRevenue:       125000.50,
        TotalRides:         12500,
        TotalUsers:         8500,
        TotalDrivers:       1200,
        AvgRating:          4.85,
        RideCompletionRate: 96.5,
        ActiveDrivers:      450,
        PendingDrivers:     35,
    }, nil
}

func generateID() string {
    return "rpt_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=reportingdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&ReportJob{}, &ReportData{})

    grpcServer := grpc.NewServer()
    pb.RegisterReportingServiceServer(grpcServer, &ReportingServer{DB: db})

    lis, err := net.Listen("tcp", ":50083")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Reporting Service running on port 50083")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}