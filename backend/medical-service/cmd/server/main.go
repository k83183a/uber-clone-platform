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

    pb "github.com/uber-clone/medical-service/proto"
)

type AmbulanceRequest struct {
    ID          string     `gorm:"primaryKey"`
    UserID      string     `gorm:"index;not null"`
    UserName    string
    UserPhone   string
    Latitude    float64    `gorm:"not null"`
    Longitude   float64    `gorm:"not null"`
    Address     string
    Symptoms    string
    Status      string     `gorm:"default:'pending'"`
    AmbulanceID string     `gorm:"index"`
    ETA         int
    RequestedAt time.Time
    AssignedAt  *time.Time
    ArrivedAt   *time.Time
    CompletedAt *time.Time
    CancelledAt *time.Time
    CancelledBy string
    Notes       string
}

type Ambulance struct {
    ID            string    `gorm:"primaryKey"`
    VehicleNumber string    `gorm:"uniqueIndex;not null"`
    DriverName    string
    DriverPhone   string
    Latitude      float64
    Longitude     float64
    Status        string    `gorm:"default:'available'"`
    CurrentTripID string
    LastUpdate    time.Time
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

type MedicalAppointment struct {
    ID              string     `gorm:"primaryKey"`
    UserID          string     `gorm:"index;not null"`
    ServiceType     string     `gorm:"not null"`
    ProviderID      string     `gorm:"index"`
    ProviderName    string
    ProviderAddress string
    AppointmentDate time.Time
    DurationMin     int
    Reason          string
    Status          string     `gorm:"default:'pending'"`
    PaymentID       string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    CancelledAt     *time.Time
    CancelledReason string
}

type MedicalServiceProvider struct {
    ID        string    `gorm:"primaryKey"`
    Name      string    `gorm:"not null"`
    Type      string    `gorm:"not null"`
    Address   string
    Latitude  float64
    Longitude float64
    Phone     string
    Email     string
    Rating    float64
    IsActive  bool      `gorm:"default:true"`
    CreatedAt time.Time
    UpdatedAt time.Time
}

type MedicalServer struct {
    pb.UnimplementedMedicalServiceServer
    DB *gorm.DB
}

// RequestAmbulance - Create ambulance request
func (s *MedicalServer) RequestAmbulance(ctx context.Context, req *pb.AmbulanceRequest) (*pb.AmbulanceResponse, error) {
    ambulanceReq := &AmbulanceRequest{
        ID:          generateID(),
        UserID:      req.UserId,
        UserName:    req.UserName,
        UserPhone:   req.UserPhone,
        Latitude:    req.Latitude,
        Longitude:   req.Longitude,
        Address:     req.Address,
        Symptoms:    req.Symptoms,
        Status:      "pending",
        RequestedAt: time.Now(),
    }
    if err := s.DB.Create(ambulanceReq).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create ambulance request")
    }

    // Find nearest available ambulance
    var ambulance Ambulance
    if err := s.DB.Where("status = ?", "available").First(&ambulance).Error; err == nil {
        now := time.Now()
        ambulanceReq.AmbulanceID = ambulance.ID
        ambulanceReq.Status = "assigned"
        ambulanceReq.AssignedAt = &now
        ambulanceReq.ETA = 10
        ambulance.Status = "on_trip"
        ambulance.CurrentTripID = ambulanceReq.ID
        ambulance.UpdatedAt = now
        s.DB.Save(&ambulanceReq)
        s.DB.Save(&ambulance)
    }

    return &pb.AmbulanceResponse{
        RequestId:   ambulanceReq.ID,
        Status:      ambulanceReq.Status,
        AmbulanceId: ambulanceReq.AmbulanceID,
        EtaMinutes:  int32(ambulanceReq.ETA),
        Message:     "Ambulance dispatched to your location",
    }, nil
}

// GetAmbulanceStatus - Get ambulance request status
func (s *MedicalServer) GetAmbulanceStatus(ctx context.Context, req *pb.GetAmbulanceStatusRequest) (*pb.AmbulanceResponse, error) {
    var ambulanceReq AmbulanceRequest
    if err := s.DB.Where("id = ?", req.RequestId).First(&ambulanceReq).Error; err != nil {
        return nil, status.Error(codes.NotFound, "ambulance request not found")
    }
    return &pb.AmbulanceResponse{
        RequestId:   ambulanceReq.ID,
        Status:      ambulanceReq.Status,
        AmbulanceId: ambulanceReq.AmbulanceID,
        EtaMinutes:  int32(ambulanceReq.ETA),
    }, nil
}

// CancelAmbulance - Cancel ambulance request
func (s *MedicalServer) CancelAmbulance(ctx context.Context, req *pb.CancelAmbulanceRequest) (*pb.Empty, error) {
    var ambulanceReq AmbulanceRequest
    if err := s.DB.Where("id = ?", req.RequestId).First(&ambulanceReq).Error; err != nil {
        return nil, status.Error(codes.NotFound, "ambulance request not found")
    }
    if ambulanceReq.Status != "pending" && ambulanceReq.Status != "assigned" {
        return nil, status.Error(codes.FailedPrecondition, "cannot cancel ambulance at this stage")
    }

    now := time.Now()
    ambulanceReq.Status = "cancelled"
    ambulanceReq.CancelledAt = &now
    ambulanceReq.CancelledBy = req.CancelledBy
    s.DB.Save(&ambulanceReq)

    if ambulanceReq.AmbulanceID != "" {
        s.DB.Model(&Ambulance{}).Where("id = ?", ambulanceReq.AmbulanceID).Updates(map[string]interface{}{
            "status":          "available",
            "current_trip_id": nil,
            "updated_at":      now,
        })
    }
    return &pb.Empty{}, nil
}

// ListMedicalProviders - List healthcare providers
func (s *MedicalServer) ListMedicalProviders(ctx context.Context, req *pb.ListMedicalProvidersRequest) (*pb.ListMedicalProvidersResponse, error) {
    var providers []MedicalServiceProvider
    query := s.DB.Where("is_active = ?", true)
    if req.Type != "" {
        query = query.Where("type = ?", req.Type)
    }
    query.Find(&providers)

    var pbProviders []*pb.MedicalProvider
    for _, p := range providers {
        pbProviders = append(pbProviders, &pb.MedicalProvider{
            Id:      p.ID,
            Name:    p.Name,
            Type:    p.Type,
            Address: p.Address,
            Phone:   p.Phone,
            Rating:  p.Rating,
        })
    }
    return &pb.ListMedicalProvidersResponse{Providers: pbProviders}, nil
}

// BookAppointment - Book medical appointment
func (s *MedicalServer) BookAppointment(ctx context.Context, req *pb.BookAppointmentRequest) (*pb.AppointmentResponse, error) {
    providerName := ""
    providerAddress := ""
    var provider MedicalServiceProvider
    if err := s.DB.Where("id = ?", req.ProviderId).First(&provider).Error; err == nil {
        providerName = provider.Name
        providerAddress = provider.Address
    }

    appointment := &MedicalAppointment{
        ID:              generateID(),
        UserID:          req.UserId,
        ServiceType:     req.ServiceType,
        ProviderID:      req.ProviderId,
        ProviderName:    providerName,
        ProviderAddress: providerAddress,
        AppointmentDate: time.Unix(req.AppointmentDate, 0),
        DurationMin:     int(req.DurationMin),
        Reason:          req.Reason,
        Status:          "pending",
        CreatedAt:       time.Now(),
        UpdatedAt:       time.Now(),
    }
    if err := s.DB.Create(appointment).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to book appointment")
    }
    return &pb.AppointmentResponse{
        Id:             appointment.ID,
        ProviderId:     appointment.ProviderID,
        ProviderName:   appointment.ProviderName,
        AppointmentDate: appointment.AppointmentDate.Unix(),
        Status:         appointment.Status,
        Message:        "Appointment request submitted. Waiting for confirmation.",
    }, nil
}

// GetAppointment - Get appointment details
func (s *MedicalServer) GetAppointment(ctx context.Context, req *pb.GetAppointmentRequest) (*pb.AppointmentDetailResponse, error) {
    var appt MedicalAppointment
    if err := s.DB.Where("id = ?", req.AppointmentId).First(&appt).Error; err != nil {
        return nil, status.Error(codes.NotFound, "appointment not found")
    }

    return &pb.AppointmentDetailResponse{
        Id:              appt.ID,
        UserId:          appt.UserID,
        ServiceType:     appt.ServiceType,
        ProviderId:      appt.ProviderID,
        ProviderName:    appt.ProviderName,
        ProviderAddress: appt.ProviderAddress,
        AppointmentDate: appt.AppointmentDate.Unix(),
        DurationMin:     int32(appt.DurationMin),
        Reason:          appt.Reason,
        Status:          appt.Status,
        CreatedAt:       appt.CreatedAt.Unix(),
    }, nil
}

// CancelAppointment - Cancel appointment
func (s *MedicalServer) CancelAppointment(ctx context.Context, req *pb.CancelAppointmentRequest) (*pb.Empty, error) {
    var appt MedicalAppointment
    if err := s.DB.Where("id = ?", req.AppointmentId).First(&appt).Error; err != nil {
        return nil, status.Error(codes.NotFound, "appointment not found")
    }
    if appt.Status == "cancelled" || appt.Status == "completed" {
        return nil, status.Error(codes.FailedPrecondition, "appointment cannot be cancelled")
    }

    now := time.Now()
    appt.Status = "cancelled"
    appt.CancelledAt = &now
    appt.CancelledReason = req.Reason
    s.DB.Save(&appt)
    return &pb.Empty{}, nil
}

// ListUserAppointments - List user's appointments
func (s *MedicalServer) ListUserAppointments(ctx context.Context, req *pb.ListUserAppointmentsRequest) (*pb.ListAppointmentsResponse, error) {
    var appointments []MedicalAppointment
    query := s.DB.Where("user_id = ?", req.UserId).Order("appointment_date DESC")
    offset := (req.Page - 1) * req.PageSize
    query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&appointments)

    var total int64
    s.DB.Model(&MedicalAppointment{}).Where("user_id = ?", req.UserId).Count(&total)

    var pbAppointments []*pb.AppointmentResponse
    for _, a := range appointments {
        pbAppointments = append(pbAppointments, &pb.AppointmentResponse{
            Id:             a.ID,
            ProviderId:     a.ProviderID,
            ProviderName:   a.ProviderName,
            AppointmentDate: a.AppointmentDate.Unix(),
            Status:         a.Status,
        })
    }
    return &pb.ListAppointmentsResponse{Appointments: pbAppointments, Total: int32(total)}, nil
}

func generateID() string {
    return "med_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=medicaldb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&AmbulanceRequest{}, &Ambulance{}, &MedicalAppointment{}, &MedicalServiceProvider{})

    // Seed sample ambulance
    var count int64
    db.Model(&Ambulance{}).Count(&count)
    if count == 0 {
        ambulance := &Ambulance{
            ID:            generateID(),
            VehicleNumber: "AMB001",
            DriverName:    "John Driver",
            DriverPhone:   "+447700900001",
            Latitude:      51.5074,
            Longitude:     -0.1278,
            Status:        "available",
            CreatedAt:     time.Now(),
            UpdatedAt:     time.Now(),
        }
        db.Create(ambulance)
        log.Println("Seeded sample ambulance")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterMedicalServiceServer(grpcServer, &MedicalServer{DB: db})

    lis, err := net.Listen("tcp", ":50080")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Medical Service running on port 50080")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}