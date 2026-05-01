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

    pb "github.com/uber-clone/driver-service/proto"
)

type Driver struct {
    ID           string     `gorm:"primaryKey"`
    UserID       string     `gorm:"uniqueIndex;not null"`
    FullName     string     `gorm:"not null"`
    Phone        string     `gorm:"not null"`
    Email        string
    DateOfBirth  string
    Address      string
    City         string
    Postcode     string
    Status       string     `gorm:"default:'pending'"`
    Rating       float64    `gorm:"default:0"`
    TotalTrips   int        `gorm:"default:0"`
    TotalEarnings float64   `gorm:"default:0"`
    StripeAccountID string
    OnlineStatus bool       `gorm:"default:false"`
    OnboardingStep int      `gorm:"default:1"`
    CreatedAt    time.Time
    UpdatedAt    time.Time
    ApprovedAt   *time.Time
    SuspendedAt  *time.Time
    SuspensionReason string
}

type Vehicle struct {
    ID                   string    `gorm:"primaryKey"`
    DriverID             string    `gorm:"index;not null"`
    Make                 string    `gorm:"not null"`
    Model                string    `gorm:"not null"`
    Year                 int
    Color                string
    LicensePlate         string    `gorm:"uniqueIndex;not null"`
    VehicleType          string    `gorm:"default:'standard'"`
    IsElectric           bool      `gorm:"default:false"`
    PetFriendly          bool      `gorm:"default:false"`
    WheelchairAccessible bool      `gorm:"default:false"`
    IsActive             bool      `gorm:"default:true"`
    CreatedAt            time.Time
    UpdatedAt            time.Time
}

type DriverDocument struct {
    ID             string     `gorm:"primaryKey"`
    DriverID       string     `gorm:"index;not null"`
    DocumentType   string     `gorm:"not null"`
    FileURL        string     `gorm:"not null"`
    Status         string     `gorm:"default:'pending'"`
    RejectionReason string
    VerifiedAt     *time.Time
    ExpiresAt      *time.Time
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type DriverServer struct {
    pb.UnimplementedDriverServiceServer
    DB *gorm.DB
}

// CreateDriverProfile - Create driver profile
func (s *DriverServer) CreateDriverProfile(ctx context.Context, req *pb.CreateDriverProfileRequest) (*pb.DriverResponse, error) {
    var existing Driver
    if err := s.DB.Where("user_id = ?", req.UserId).First(&existing).Error; err == nil {
        return nil, status.Error(codes.AlreadyExists, "driver profile already exists")
    }

    driver := &Driver{
        ID:             generateID(),
        UserID:         req.UserId,
        FullName:       req.FullName,
        Phone:          req.Phone,
        Email:          req.Email,
        DateOfBirth:    req.DateOfBirth,
        Address:        req.Address,
        City:           req.City,
        Postcode:       req.Postcode,
        Status:         "pending",
        OnboardingStep: 1,
        CreatedAt:      time.Now(),
        UpdatedAt:      time.Now(),
    }
    if err := s.DB.Create(driver).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create driver profile")
    }

    return &pb.DriverResponse{
        Id:          driver.ID,
        UserId:      driver.UserID,
        FullName:    driver.FullName,
        Phone:       driver.Phone,
        Status:      driver.Status,
        Rating:      driver.Rating,
        TotalTrips:  int32(driver.TotalTrips),
        CreatedAt:   driver.CreatedAt.Unix(),
    }, nil
}

// GetDriverProfile - Get driver profile
func (s *DriverServer) GetDriverProfile(ctx context.Context, req *pb.GetDriverProfileRequest) (*pb.DriverResponse, error) {
    var driver Driver
    if req.DriverId != "" {
        s.DB.Where("id = ?", req.DriverId).First(&driver)
    } else if req.UserId != "" {
        s.DB.Where("user_id = ?", req.UserId).First(&driver)
    } else {
        return nil, status.Error(codes.InvalidArgument, "driver_id or user_id required")
    }

    return &pb.DriverResponse{
        Id:          driver.ID,
        UserId:      driver.UserID,
        FullName:    driver.FullName,
        Phone:       driver.Phone,
        Email:       driver.Email,
        Status:      driver.Status,
        Rating:      driver.Rating,
        TotalTrips:  int32(driver.TotalTrips),
        TotalEarnings: driver.TotalEarnings,
        OnlineStatus: driver.OnlineStatus,
        CreatedAt:   driver.CreatedAt.Unix(),
    }, nil
}

// UpdateDriverStatus - Update driver status (admin)
func (s *DriverServer) UpdateDriverStatus(ctx context.Context, req *pb.UpdateDriverStatusRequest) (*pb.DriverResponse, error) {
    var driver Driver
    if err := s.DB.Where("id = ?", req.DriverId).First(&driver).Error; err != nil {
        return nil, status.Error(codes.NotFound, "driver not found")
    }

    driver.Status = req.Status
    driver.UpdatedAt = time.Now()
    if req.Status == "approved" {
        now := time.Now()
        driver.ApprovedAt = &now
    } else if req.Status == "suspended" {
        now := time.Now()
        driver.SuspendedAt = &now
        driver.SuspensionReason = req.SuspensionReason
    }
    s.DB.Save(&driver)

    return s.GetDriverProfile(ctx, &pb.GetDriverProfileRequest{DriverId: driver.ID})
}

// UpdateOnlineStatus - Update driver online status
func (s *DriverServer) UpdateOnlineStatus(ctx context.Context, req *pb.UpdateOnlineStatusRequest) (*pb.Empty, error) {
    var driver Driver
    if err := s.DB.Where("id = ?", req.DriverId).First(&driver).Error; err != nil {
        return nil, status.Error(codes.NotFound, "driver not found")
    }
    if driver.Status != "approved" {
        return nil, status.Error(codes.FailedPrecondition, "driver not approved")
    }
    driver.OnlineStatus = req.Online
    driver.UpdatedAt = time.Now()
    s.DB.Save(&driver)

    return &pb.Empty{}, nil
}

// AddVehicle - Add vehicle to driver profile
func (s *DriverServer) AddVehicle(ctx context.Context, req *pb.AddVehicleRequest) (*pb.VehicleResponse, error) {
    vehicle := &Vehicle{
        ID:                   generateID(),
        DriverID:             req.DriverId,
        Make:                 req.Make,
        Model:                req.Model,
        Year:                 int(req.Year),
        Color:                req.Color,
        LicensePlate:         req.LicensePlate,
        VehicleType:          req.VehicleType,
        IsElectric:           req.IsElectric,
        PetFriendly:          req.PetFriendly,
        WheelchairAccessible: req.WheelchairAccessible,
        IsActive:             true,
        CreatedAt:            time.Now(),
        UpdatedAt:            time.Now(),
    }
    if err := s.DB.Create(vehicle).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to add vehicle")
    }

    return &pb.VehicleResponse{
        Id:                   vehicle.ID,
        DriverId:             vehicle.DriverID,
        Make:                 vehicle.Make,
        Model:                vehicle.Model,
        Year:                 int32(vehicle.Year),
        Color:                vehicle.Color,
        LicensePlate:         vehicle.LicensePlate,
        VehicleType:          vehicle.VehicleType,
        IsElectric:           vehicle.IsElectric,
        PetFriendly:          vehicle.PetFriendly,
        WheelchairAccessible: vehicle.WheelchairAccessible,
    }, nil
}

// GetVehicles - Get driver's vehicles
func (s *DriverServer) GetVehicles(ctx context.Context, req *pb.GetVehiclesRequest) (*pb.VehiclesResponse, error) {
    var vehicles []Vehicle
    s.DB.Where("driver_id = ? AND is_active = ?", req.DriverId, true).Find(&vehicles)

    var pbVehicles []*pb.VehicleResponse
    for _, v := range vehicles {
        pbVehicles = append(pbVehicles, &pb.VehicleResponse{
            Id:                   v.ID,
            DriverId:             v.DriverID,
            Make:                 v.Make,
            Model:                v.Model,
            Year:                 int32(v.Year),
            Color:                v.Color,
            LicensePlate:         v.LicensePlate,
            VehicleType:          v.VehicleType,
            IsElectric:           v.IsElectric,
            PetFriendly:          v.PetFriendly,
            WheelchairAccessible: v.WheelchairAccessible,
        })
    }

    return &pb.VehiclesResponse{Vehicles: pbVehicles}, nil
}

// UploadDocument - Upload driver document
func (s *DriverServer) UploadDocument(ctx context.Context, req *pb.UploadDocumentRequest) (*pb.DocumentResponse, error) {
    doc := &DriverDocument{
        ID:           generateID(),
        DriverID:     req.DriverId,
        DocumentType: req.DocumentType,
        FileURL:      req.FileUrl,
        Status:       "pending",
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
    }
    s.DB.Create(doc)

    var totalDocs int64
    s.DB.Model(&DriverDocument{}).Where("driver_id = ? AND document_type IN (?)", req.DriverId, []string{"license_front", "license_back", "phv_license"}).Count(&totalDocs)
    if totalDocs >= 3 {
        s.DB.Model(&Driver{}).Where("id = ?", req.DriverId).Update("onboarding_step", 2)
    }

    return &pb.DocumentResponse{
        Id:           doc.ID,
        DocumentType: doc.DocumentType,
        Status:       doc.Status,
    }, nil
}

// GetDocuments - Get driver documents
func (s *DriverServer) GetDocuments(ctx context.Context, req *pb.GetDocumentsRequest) (*pb.DocumentsResponse, error) {
    var docs []DriverDocument
    s.DB.Where("driver_id = ?", req.DriverId).Find(&docs)

    var pbDocs []*pb.DocumentResponse
    for _, d := range docs {
        pbDocs = append(pbDocs, &pb.DocumentResponse{
            Id:           d.ID,
            DocumentType: d.DocumentType,
            FileUrl:      d.FileURL,
            Status:       d.Status,
        })
    }

    return &pb.DocumentsResponse{Documents: pbDocs}, nil
}

// ListPendingDrivers - List drivers pending approval
func (s *DriverServer) ListPendingDrivers(ctx context.Context, req *pb.ListPendingDriversRequest) (*pb.ListDriversResponse, error) {
    var drivers []Driver
    s.DB.Where("status = ?", "pending").Find(&drivers)

    var pbDrivers []*pb.DriverResponse
    for _, d := range drivers {
        pbDrivers = append(pbDrivers, &pb.DriverResponse{
            Id:        d.ID,
            UserId:    d.UserID,
            FullName:  d.FullName,
            Phone:     d.Phone,
            Status:    d.Status,
            CreatedAt: d.CreatedAt.Unix(),
        })
    }

    return &pb.ListDriversResponse{Drivers: pbDrivers}, nil
}

func generateID() string {
    return "drv_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=driverdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Driver{}, &Vehicle{}, &DriverDocument{})

    grpcServer := grpc.NewServer()
    pb.RegisterDriverServiceServer(grpcServer, &DriverServer{DB: db})

    lis, err := net.Listen("tcp", ":50073")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Driver Service running on port 50073")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}