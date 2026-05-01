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

    pb "github.com/uber-clone/appointment-service/proto"
)

// ServiceProvider represents a salon, clinic, or healthcare provider
type ServiceProvider struct {
    ID           string    `gorm:"primaryKey"`
    Name         string    `gorm:"not null"`
    Type         string    `gorm:"not null"` // salon, clinic, dentist, physio, spa
    Description  string
    Address      string    `gorm:"not null"`
    Latitude     float64
    Longitude    float64
    Phone        string
    Email        string
    ImageURL     string
    Rating       float64   `gorm:"default:0"`
    RatingCount  int       `gorm:"default:0"`
    IsActive     bool      `gorm:"default:true"`
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

// Service represents a specific service offered (e.g., haircut, massage, dental cleaning)
type Service struct {
    ID             string    `gorm:"primaryKey"`
    ProviderID     string    `gorm:"index;not null"`
    Name           string    `gorm:"not null"`
    Description    string
    DurationMin    int       `gorm:"not null"` // minutes
    Price          float64   `gorm:"not null"`
    DiscountPrice  float64
    IsActive       bool      `gorm:"default:true"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

// Staff represents a provider's employee (e.g., stylist, therapist)
type Staff struct {
    ID          string    `gorm:"primaryKey"`
    ProviderID  string    `gorm:"index;not null"`
    Name        string    `gorm:"not null"`
    Role        string
    Bio         string
    ImageURL    string
    IsActive    bool      `gorm:"default:true"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// TimeSlot represents an available time slot for booking
type TimeSlot struct {
    ID          string    `gorm:"primaryKey"`
    ProviderID  string    `gorm:"index;not null"`
    StaffID     string    `gorm:"index"`
    Date        time.Time `gorm:"index"`
    StartTime   time.Time
    EndTime     time.Time
    IsAvailable bool      `gorm:"default:true"`
    CreatedAt   time.Time
}

// Appointment represents a booking
type Appointment struct {
    ID           string     `gorm:"primaryKey"`
    BookingRef   string     `gorm:"uniqueIndex"`
    UserID       string     `gorm:"index;not null"`
    ProviderID   string     `gorm:"index;not null"`
    StaffID      string     `gorm:"index"`
    ServiceID    string     `gorm:"index;not null"`
    ServiceName  string
    ServicePrice float64
    Date         time.Time
    StartTime    time.Time
    EndTime      time.Time
    Status       string     `gorm:"default:'pending'"` // pending, confirmed, completed, cancelled, no_show
    CancelledAt  *time.Time
    CancelledBy  string
    PaymentID    string
    Notes        string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

// AppointmentServer handles gRPC requests
type AppointmentServer struct {
    pb.UnimplementedAppointmentServiceServer
    DB *gorm.DB
}

// ListProviders returns service providers near a location
func (s *AppointmentServer) ListProviders(ctx context.Context, req *pb.ListProvidersRequest) (*pb.ListProvidersResponse, error) {
    var providers []ServiceProvider
    query := s.DB.Where("is_active = ?", true)

    if req.Type != "" {
        query = query.Where("type = ?", req.Type)
    }

    if req.Latitude != 0 && req.Longitude != 0 {
        query = query.Order("latitude")
    }

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&providers).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list providers")
    }

    var total int64
    s.DB.Model(&ServiceProvider{}).Where("is_active = ?", true).Count(&total)

    var pbProviders []*pb.ServiceProvider
    for _, p := range providers {
        pbProviders = append(pbProviders, &pb.ServiceProvider{
            Id:          p.ID,
            Name:        p.Name,
            Type:        p.Type,
            Description: p.Description,
            Address:     p.Address,
            Latitude:    p.Latitude,
            Longitude:   p.Longitude,
            Phone:       p.Phone,
            ImageUrl:    p.ImageURL,
            Rating:      p.Rating,
        })
    }

    return &pb.ListProvidersResponse{Providers: pbProviders, Total: int32(total)}, nil
}

// GetProvider returns provider details
func (s *AppointmentServer) GetProvider(ctx context.Context, req *pb.GetProviderRequest) (*pb.ServiceProvider, error) {
    var provider ServiceProvider
    if err := s.DB.Where("id = ? AND is_active = ?", req.Id, true).First(&provider).Error; err != nil {
        return nil, status.Error(codes.NotFound, "provider not found")
    }

    return &pb.ServiceProvider{
        Id:          provider.ID,
        Name:        provider.Name,
        Type:        provider.Type,
        Description: provider.Description,
        Address:     provider.Address,
        Latitude:    provider.Latitude,
        Longitude:   provider.Longitude,
        Phone:       provider.Phone,
        ImageUrl:    provider.ImageURL,
        Rating:      provider.Rating,
    }, nil
}

// ListServices returns services offered by a provider
func (s *AppointmentServer) ListServices(ctx context.Context, req *pb.ListServicesRequest) (*pb.ListServicesResponse, error) {
    var services []Service
    if err := s.DB.Where("provider_id = ? AND is_active = ?", req.ProviderId, true).Find(&services).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list services")
    }

    var pbServices []*pb.Service
    for _, svc := range services {
        pbServices = append(pbServices, &pb.Service{
            Id:          svc.ID,
            Name:        svc.Name,
            Description: svc.Description,
            DurationMin: int32(svc.DurationMin),
            Price:       svc.Price,
            DiscountPrice: svc.DiscountPrice,
        })
    }

    return &pb.ListServicesResponse{Services: pbServices}, nil
}

// GetAvailableSlots returns available time slots for a provider/staff
func (s *AppointmentServer) GetAvailableSlots(ctx context.Context, req *pb.GetAvailableSlotsRequest) (*pb.AvailableSlotsResponse, error) {
    date := time.Unix(req.Date, 0)
    startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
    endOfDay := startOfDay.Add(24 * time.Hour)

    var slots []TimeSlot
    query := s.DB.Where("provider_id = ? AND date = ? AND is_available = ?", req.ProviderId, startOfDay, true)

    if req.StaffId != "" {
        query = query.Where("staff_id = ?", req.StaffId)
    }

    if err := query.Find(&slots).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get available slots")
    }

    var pbSlots []*pb.TimeSlot
    for _, s := range slots {
        pbSlots = append(pbSlots, &pb.TimeSlot{
            Id:        s.ID,
            StartTime: s.StartTime.Unix(),
            EndTime:   s.EndTime.Unix(),
        })
    }

    return &pb.AvailableSlotsResponse{Slots: pbSlots}, nil
}

// CreateAppointment books an appointment
func (s *AppointmentServer) CreateAppointment(ctx context.Context, req *pb.CreateAppointmentRequest) (*pb.AppointmentResponse, error) {
    // Get service details
    var service Service
    if err := s.DB.Where("id = ?", req.ServiceId).First(&service).Error; err != nil {
        return nil, status.Error(codes.NotFound, "service not found")
    }

    // Get staff details if specified
    var staffName string
    if req.StaffId != "" {
        var staff Staff
        if err := s.DB.Where("id = ?", req.StaffId).First(&staff).Error; err == nil {
            staffName = staff.Name
        }
    }

    startTime := time.Unix(req.StartTime, 0)
    endTime := startTime.Add(time.Duration(service.DurationMin) * time.Minute)

    appointment := &Appointment{
        ID:           generateID(),
        BookingRef:   generateBookingRef(),
        UserID:       req.UserId,
        ProviderID:   req.ProviderId,
        StaffID:      req.StaffId,
        ServiceID:    req.ServiceId,
        ServiceName:  service.Name,
        ServicePrice: service.Price,
        Date:         time.Unix(req.Date, 0),
        StartTime:    startTime,
        EndTime:      endTime,
        Status:       "confirmed",
        Notes:        req.Notes,
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
    }

    // Mark slot as unavailable
    if req.SlotId != "" {
        s.DB.Model(&TimeSlot{}).Where("id = ?", req.SlotId).Update("is_available", false)
    }

    if err := s.DB.Create(appointment).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create appointment")
    }

    return &pb.AppointmentResponse{
        Id:         appointment.ID,
        BookingRef: appointment.BookingRef,
        Status:     appointment.Status,
        StartTime:  appointment.StartTime.Unix(),
        EndTime:    appointment.EndTime.Unix(),
    }, nil
}

// GetAppointment returns appointment details
func (s *AppointmentServer) GetAppointment(ctx context.Context, req *pb.GetAppointmentRequest) (*pb.AppointmentDetailResponse, error) {
    var appt Appointment
    if err := s.DB.Where("id = ? OR booking_ref = ?", req.Id, req.BookingRef).First(&appt).Error; err != nil {
        return nil, status.Error(codes.NotFound, "appointment not found")
    }

    return &pb.AppointmentDetailResponse{
        Id:         appt.ID,
        BookingRef: appt.BookingRef,
        UserId:     appt.UserID,
        ProviderId: appt.ProviderID,
        StaffId:    appt.StaffID,
        ServiceId:  appt.ServiceID,
        ServiceName: appt.ServiceName,
        Price:      appt.ServicePrice,
        Date:       appt.Date.Unix(),
        StartTime:  appt.StartTime.Unix(),
        EndTime:    appt.EndTime.Unix(),
        Status:     appt.Status,
        Notes:      appt.Notes,
        CreatedAt:  appt.CreatedAt.Unix(),
    }, nil
}

// CancelAppointment cancels an appointment
func (s *AppointmentServer) CancelAppointment(ctx context.Context, req *pb.CancelAppointmentRequest) (*pb.Empty, error) {
    var appt Appointment
    if err := s.DB.Where("id = ?", req.AppointmentId).First(&appt).Error; err != nil {
        return nil, status.Error(codes.NotFound, "appointment not found")
    }

    if appt.Status == "cancelled" || appt.Status == "completed" {
        return nil, status.Error(codes.FailedPrecondition, "appointment cannot be cancelled")
    }

    now := time.Now()
    appt.Status = "cancelled"
    appt.CancelledAt = &now
    appt.CancelledBy = req.CancelledBy

    if err := s.DB.Save(&appt).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to cancel appointment")
    }

    // Release time slot if exists
    // In production: mark slot as available again

    return &pb.Empty{}, nil
}

// ListUserAppointments lists appointments for a user
func (s *AppointmentServer) ListUserAppointments(ctx context.Context, req *pb.ListUserAppointmentsRequest) (*pb.ListAppointmentsResponse, error) {
    var appointments []Appointment
    query := s.DB.Where("user_id = ?", req.UserId).Order("start_time DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&appointments).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list appointments")
    }

    var pbAppointments []*pb.AppointmentResponse
    for _, a := range appointments {
        pbAppointments = append(pbAppointments, &pb.AppointmentResponse{
            Id:         a.ID,
            BookingRef: a.BookingRef,
            Status:     a.Status,
            StartTime:  a.StartTime.Unix(),
            EndTime:    a.EndTime.Unix(),
        })
    }

    return &pb.ListAppointmentsResponse{Appointments: pbAppointments}, nil
}

func generateID() string {
    return "apt_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateBookingRef() string {
    return "BOOK" + time.Now().Format("20060102") + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=appointmentdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&ServiceProvider{}, &Service{}, &Staff{}, &TimeSlot{}, &Appointment{})

    // Seed sample provider
    var count int64
    db.Model(&ServiceProvider{}).Count(&count)
    if count == 0 {
        provider := &ServiceProvider{
            ID:          generateID(),
            Name:        "London Wellness Spa",
            Type:        "spa",
            Description: "Luxury spa and wellness center in central London",
            Address:     "123 Regent Street, London",
            Latitude:    51.5099,
            Longitude:   -0.1337,
            Phone:       "+442012345678",
            IsActive:    true,
            CreatedAt:   time.Now(),
            UpdatedAt:   time.Now(),
        }
        db.Create(provider)

        // Seed some services
        services := []Service{
            {ID: generateID(), ProviderID: provider.ID, Name: "Swedish Massage", DurationMin: 60, Price: 65.00, IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), ProviderID: provider.ID, Name: "Deep Tissue Massage", DurationMin: 90, Price: 85.00, IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), ProviderID: provider.ID, Name: "Facial Treatment", DurationMin: 45, Price: 55.00, IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        }
        db.Create(&services)
        log.Println("Seeded appointment service provider and services")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterAppointmentServiceServer(grpcServer, &AppointmentServer{DB: db})

    lis, err := net.Listen("tcp", ":50070")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Appointment Service running on port 50070")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Appointment Service stopped")
}