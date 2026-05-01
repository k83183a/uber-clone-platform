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

    pb "github.com/uber-clone/compliance-service/proto"
)

type DBCheck struct {
    ID               string     `gorm:"primaryKey"`
    DriverID         string     `gorm:"index;not null"`
    FullName         string     `gorm:"not null"`
    DateOfBirth      string     `gorm:"not null"`
    Address          string     `gorm:"not null"`
    ConsentToken     string     `gorm:"not null"`
    ExternalReference string    `gorm:"index"`
    Status           string     `gorm:"default:'pending'"`
    CertificateURL   string
    ResultData       string     `gorm:"type:text"`
    RequestedAt      time.Time
    CompletedAt      *time.Time
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type DVLACheck struct {
    ID               string     `gorm:"primaryKey"`
    DriverID         string     `gorm:"index;not null"`
    LicenceNumber    string     `gorm:"not null"`
    Postcode         string     `gorm:"not null"`
    ExternalReference string    `gorm:"index"`
    Status           string     `gorm:"default:'pending'"`
    LicenceValid     bool
    Entitlements     string     `gorm:"type:text"`
    PenaltyPoints    int
    ExpiryDate       *time.Time
    ResultData       string     `gorm:"type:text"`
    RequestedAt      time.Time
    CompletedAt      *time.Time
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type RightToWorkCheck struct {
    ID               string     `gorm:"primaryKey"`
    DriverID         string     `gorm:"index;not null"`
    PassportNumber   string
    ShareCode        string     `gorm:"not null"`
    Status           string     `gorm:"default:'pending'"`
    ResultData       string     `gorm:"type:text"`
    RequestedAt      time.Time
    CompletedAt      *time.Time
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type ComplianceServer struct {
    pb.UnimplementedComplianceServiceServer
    DB *gorm.DB
}

// TriggerDBCheck - Initiate DBS check
func (s *ComplianceServer) TriggerDBCheck(ctx context.Context, req *pb.DBCheckRequest) (*pb.CheckResponse, error) {
    check := &DBCheck{
        ID:           generateID(),
        DriverID:     req.DriverId,
        FullName:     req.FullName,
        DateOfBirth:  req.DateOfBirth,
        Address:      req.Address,
        ConsentToken: req.ConsentToken,
        Status:       "pending",
        RequestedAt:  time.Now(),
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
    }
    s.DB.Create(check)

    go s.simulateDBCheck(check.ID)

    return &pb.CheckResponse{
        CheckId: check.ID,
        Status:  check.Status,
        Message: "DBS check initiated. Results will be available in 24-48 hours.",
    }, nil
}

// TriggerDVLACheck - Initiate DVLA check
func (s *ComplianceServer) TriggerDVLACheck(ctx context.Context, req *pb.DVLACheckRequest) (*pb.CheckResponse, error) {
    check := &DVLACheck{
        ID:            generateID(),
        DriverID:      req.DriverId,
        LicenceNumber: req.LicenceNumber,
        Postcode:      req.Postcode,
        Status:        "pending",
        RequestedAt:   time.Now(),
        CreatedAt:     time.Now(),
        UpdatedAt:     time.Now(),
    }
    s.DB.Create(check)

    go s.simulateDVLACheck(check.ID)

    return &pb.CheckResponse{
        CheckId: check.ID,
        Status:  check.Status,
        Message: "DVLA check initiated.",
    }, nil
}

// TriggerRightToWorkCheck - Initiate right-to-work check
func (s *ComplianceServer) TriggerRightToWorkCheck(ctx context.Context, req *pb.RightToWorkCheckRequest) (*pb.CheckResponse, error) {
    check := &RightToWorkCheck{
        ID:           generateID(),
        DriverID:     req.DriverId,
        PassportNumber: req.PassportNumber,
        ShareCode:    req.ShareCode,
        Status:       "pending",
        RequestedAt:  time.Now(),
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
    }
    s.DB.Create(check)

    go s.simulateRightToWorkCheck(check.ID)

    return &pb.CheckResponse{
        CheckId: check.ID,
        Status:  check.Status,
        Message: "Right-to-work check initiated.",
    }, nil
}

// GetCheckStatus - Get check status
func (s *ComplianceServer) GetCheckStatus(ctx context.Context, req *pb.GetCheckStatusRequest) (*pb.CheckStatusResponse, error) {
    switch req.CheckType {
    case "dbs":
        var check DBCheck
        if err := s.DB.Where("id = ?", req.CheckId).First(&check).Error; err != nil {
            return nil, status.Error(codes.NotFound, "check not found")
        }
        return &pb.CheckStatusResponse{
            Status:      check.Status,
            CompletedAt: check.CompletedAt.Unix(),
            ResultData:  check.ResultData,
        }, nil
    case "dvla":
        var check DVLACheck
        if err := s.DB.Where("id = ?", req.CheckId).First(&check).Error; err != nil {
            return nil, status.Error(codes.NotFound, "check not found")
        }
        return &pb.CheckStatusResponse{
            Status:      check.Status,
            CompletedAt: check.CompletedAt.Unix(),
            ResultData:  check.ResultData,
        }, nil
    case "right_to_work":
        var check RightToWorkCheck
        if err := s.DB.Where("id = ?", req.CheckId).First(&check).Error; err != nil {
            return nil, status.Error(codes.NotFound, "check not found")
        }
        return &pb.CheckStatusResponse{
            Status:      check.Status,
            CompletedAt: check.CompletedAt.Unix(),
            ResultData:  check.ResultData,
        }, nil
    default:
        return nil, status.Error(codes.InvalidArgument, "invalid check type")
    }
}

// GetDriverComplianceStatus - Get overall compliance status
func (s *ComplianceServer) GetDriverComplianceStatus(ctx context.Context, req *pb.GetDriverComplianceStatusRequest) (*pb.DriverComplianceStatusResponse, error) {
    var dbsCheck DBCheck
    var dvlaCheck DVLACheck
    var rtwCheck RightToWorkCheck

    s.DB.Where("driver_id = ?", req.DriverId).Order("created_at DESC").First(&dbsCheck)
    s.DB.Where("driver_id = ?", req.DriverId).Order("created_at DESC").First(&dvlaCheck)
    s.DB.Where("driver_id = ?", req.DriverId).Order("created_at DESC").First(&rtwCheck)

    dbsStatus := "not_started"
    if dbsCheck.ID != "" {
        dbsStatus = dbsCheck.Status
    }
    dvlaStatus := "not_started"
    if dvlaCheck.ID != "" {
        dvlaStatus = dvlaCheck.Status
    }
    rtwStatus := "not_started"
    if rtwCheck.ID != "" {
        rtwStatus = rtwCheck.Status
    }
    allPassed := dbsStatus == "passed" && dvlaStatus == "passed" && rtwStatus == "passed"

    return &pb.DriverComplianceStatusResponse{
        DbsStatus:         dbsStatus,
        DvlaStatus:        dvlaStatus,
        RightToWorkStatus: rtwStatus,
        AllPassed:         allPassed,
    }, nil
}

func (s *ComplianceServer) simulateDBCheck(checkID string) {
    time.Sleep(5 * time.Second)
    var check DBCheck
    s.DB.Where("id = ?", checkID).First(&check)
    now := time.Now()
    check.Status = "passed"
    check.CompletedAt = &now
    check.ResultData = `{"certificate_issued":true}`
    s.DB.Save(&check)
    log.Printf("DBS check %s completed", checkID)
}

func (s *ComplianceServer) simulateDVLACheck(checkID string) {
    time.Sleep(3 * time.Second)
    var check DVLACheck
    s.DB.Where("id = ?", checkID).First(&check)
    now := time.Now()
    check.Status = "passed"
    check.LicenceValid = true
    check.PenaltyPoints = 0
    check.CompletedAt = &now
    check.ResultData = `{"licence_valid":true,"penalty_points":0}`
    s.DB.Save(&check)
    log.Printf("DVLA check %s completed", checkID)
}

func (s *ComplianceServer) simulateRightToWorkCheck(checkID string) {
    time.Sleep(4 * time.Second)
    var check RightToWorkCheck
    s.DB.Where("id = ?", checkID).First(&check)
    now := time.Now()
    check.Status = "passed"
    check.CompletedAt = &now
    check.ResultData = `{"right_to_work":true,"share_code_valid":true}`
    s.DB.Save(&check)
    log.Printf("Right-to-work check %s completed", checkID)
}

func generateID() string {
    return "cmp_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=compliancedb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&DBCheck{}, &DVLACheck{}, &RightToWorkCheck{})

    grpcServer := grpc.NewServer()
    pb.RegisterComplianceServiceServer(grpcServer, &ComplianceServer{DB: db})

    lis, err := net.Listen("tcp", ":50075")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Compliance Service running on port 50075")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}