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

    pb "github.com/uber-clone/admin-service/proto"
)

type AdminUser struct {
    ID        string     `gorm:"primaryKey"`
    Email     string     `gorm:"uniqueIndex;not null"`
    Password  string     `gorm:"not null"`
    FullName  string
    Role      string     `gorm:"default:'viewer'"`
    IsActive  bool       `gorm:"default:true"`
    LastLogin *time.Time
    CreatedAt time.Time
    UpdatedAt time.Time
}

type AdminAuditLog struct {
    ID          string    `gorm:"primaryKey"`
    AdminID     string    `gorm:"index"`
    Action      string    `gorm:"not null"`
    Resource    string
    ResourceID  string
    Details     string    `gorm:"type:text"`
    IPAddress   string
    CreatedAt   time.Time
}

type AdminServer struct {
    pb.UnimplementedAdminServiceServer
    DB *gorm.DB
}

// Login - Admin login
func (s *AdminServer) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
    var admin AdminUser
    if err := s.DB.Where("email = ? AND is_active = ?", req.Email, true).First(&admin).Error; err != nil {
        return nil, status.Error(codes.Unauthenticated, "invalid credentials")
    }
    if req.Password != admin.Password {
        return nil, status.Error(codes.Unauthenticated, "invalid credentials")
    }

    now := time.Now()
    admin.LastLogin = &now
    s.DB.Save(&admin)

    token := generateAdminToken(admin.ID, admin.Role)
    s.logAction(admin.ID, "login", "admin", admin.ID, "Admin logged in", req.IpAddress)

    return &pb.LoginResponse{
        Token:     token,
        AdminId:   admin.ID,
        FullName:  admin.FullName,
        Role:      admin.Role,
        ExpiresIn: 86400,
    }, nil
}

// ListUsers - List platform users
func (s *AdminServer) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
    // In production: call user-service
    return &pb.ListUsersResponse{Users: []*pb.UserInfo{}, Total: 0}, nil
}

// GetUserDetails - Get user details
func (s *AdminServer) GetUserDetails(ctx context.Context, req *pb.GetUserDetailsRequest) (*pb.UserDetailsResponse, error) {
    return &pb.UserDetailsResponse{UserId: req.UserId}, nil
}

// SuspendUser - Suspend user
func (s *AdminServer) SuspendUser(ctx context.Context, req *pb.SuspendUserRequest) (*pb.Empty, error) {
    s.logAction(req.AdminId, "suspend_user", "user", req.UserId, req.Reason, "")
    return &pb.Empty{}, nil
}

// ListDrivers - List drivers
func (s *AdminServer) ListDrivers(ctx context.Context, req *pb.ListDriversRequest) (*pb.ListDriversResponse, error) {
    return &pb.ListDriversResponse{Drivers: []*pb.DriverInfo{}, Total: 0}, nil
}

// ApproveDriver - Approve driver
func (s *AdminServer) ApproveDriver(ctx context.Context, req *pb.ApproveDriverRequest) (*pb.Empty, error) {
    s.logAction(req.AdminId, "approve_driver", "driver", req.DriverId, "", "")
    return &pb.Empty{}, nil
}

// RejectDriver - Reject driver
func (s *AdminServer) RejectDriver(ctx context.Context, req *pb.RejectDriverRequest) (*pb.Empty, error) {
    s.logAction(req.AdminId, "reject_driver", "driver", req.DriverId, req.Reason, "")
    return &pb.Empty{}, nil
}

// BulkSuspendDrivers - Bulk suspend drivers
func (s *AdminServer) BulkSuspendDrivers(ctx context.Context, req *pb.BulkSuspendDriversRequest) (*pb.BulkOperationResponse, error) {
    for _, driverId := range req.DriverIds {
        s.logAction(req.AdminId, "bulk_suspend_driver", "driver", driverId, req.Reason, "")
    }
    return &pb.BulkOperationResponse{SuccessCount: int32(len(req.DriverIds)), FailedCount: 0}, nil
}

// ListStores - List stores
func (s *AdminServer) ListStores(ctx context.Context, req *pb.ListStoresRequest) (*pb.ListStoresResponse, error) {
    return &pb.ListStoresResponse{Stores: []*pb.StoreInfo{}, Total: 0}, nil
}

// GetDashboardStats - Get dashboard stats
func (s *AdminServer) GetDashboardStats(ctx context.Context, req *pb.Empty) (*pb.DashboardStatsResponse, error) {
    return &pb.DashboardStatsResponse{
        TotalUsers:       12500,
        TotalDrivers:     3200,
        TotalRidesToday:  1450,
        TotalRevenueToday: 28750.50,
        ActiveDrivers:    185,
        PendingDrivers:   42,
    }, nil
}

// GetRevenueReport - Get revenue report
func (s *AdminServer) GetRevenueReport(ctx context.Context, req *pb.GetRevenueReportRequest) (*pb.RevenueReportResponse, error) {
    return &pb.RevenueReportResponse{DailyRevenue: []*pb.DailyRevenue{}, TotalRevenue: 0}, nil
}

// GetRideStats - Get ride stats
func (s *AdminServer) GetRideStats(ctx context.Context, req *pb.GetRideStatsRequest) (*pb.RideStatsResponse, error) {
    return &pb.RideStatsResponse{
        TotalRides:      50000,
        CompletedRides:  48000,
        CancelledRides:  2000,
        AverageRating:   4.8,
    }, nil
}

// CreateAdmin - Create admin user
func (s *AdminServer) CreateAdmin(ctx context.Context, req *pb.CreateAdminRequest) (*pb.AdminResponse, error) {
    admin := &AdminUser{
        ID:        generateID(),
        Email:     req.Email,
        Password:  req.Password,
        FullName:  req.FullName,
        Role:      req.Role,
        IsActive:  true,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }
    s.DB.Create(admin)
    s.logAction(req.CreatedBy, "create_admin", "admin", admin.ID, "Created admin "+admin.Email, "")
    return &pb.AdminResponse{Id: admin.ID, Email: admin.Email, FullName: admin.FullName, Role: admin.Role, IsActive: admin.IsActive}, nil
}

// ListAdmins - List admin users
func (s *AdminServer) ListAdmins(ctx context.Context, req *pb.ListAdminsRequest) (*pb.ListAdminsResponse, error) {
    var admins []AdminUser
    s.DB.Find(&admins)
    var pbAdmins []*pb.AdminResponse
    for _, a := range admins {
        pbAdmins = append(pbAdmins, &pb.AdminResponse{
            Id:       a.ID,
            Email:    a.Email,
            FullName: a.FullName,
            Role:     a.Role,
            IsActive: a.IsActive,
        })
    }
    return &pb.ListAdminsResponse{Admins: pbAdmins}, nil
}

// UpdateAdmin - Update admin
func (s *AdminServer) UpdateAdmin(ctx context.Context, req *pb.UpdateAdminRequest) (*pb.AdminResponse, error) {
    var admin AdminUser
    if err := s.DB.Where("id = ?", req.AdminId).First(&admin).Error; err != nil {
        return nil, status.Error(codes.NotFound, "admin not found")
    }
    if req.FullName != "" {
        admin.FullName = req.FullName
    }
    if req.Role != "" {
        admin.Role = req.Role
    }
    if req.Password != "" {
        admin.Password = req.Password
    }
    admin.UpdatedAt = time.Now()
    s.DB.Save(&admin)
    return &pb.AdminResponse{Id: admin.ID, Email: admin.Email, FullName: admin.FullName, Role: admin.Role, IsActive: admin.IsActive}, nil
}

// DeleteAdmin - Delete admin
func (s *AdminServer) DeleteAdmin(ctx context.Context, req *pb.DeleteAdminRequest) (*pb.Empty, error) {
    s.DB.Where("id = ?", req.AdminId).Delete(&AdminUser{})
    return &pb.Empty{}, nil
}

// GetAuditLogs - Get audit logs
func (s *AdminServer) GetAuditLogs(ctx context.Context, req *pb.GetAuditLogsRequest) (*pb.AuditLogsResponse, error) {
    var logs []AdminAuditLog
    query := s.DB.Order("created_at DESC")
    if req.AdminId != "" {
        query = query.Where("admin_id = ?", req.AdminId)
    }
    if req.Action != "" {
        query = query.Where("action = ?", req.Action)
    }
    offset := (req.Page - 1) * req.PageSize
    query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&logs)

    var total int64
    s.DB.Model(&AdminAuditLog{}).Count(&total)

    var pbLogs []*pb.AuditLog
    for _, l := range logs {
        pbLogs = append(pbLogs, &pb.AuditLog{
            Id:         l.ID,
            AdminId:    l.AdminID,
            Action:     l.Action,
            Resource:   l.Resource,
            ResourceId: l.ResourceID,
            Details:    l.Details,
            CreatedAt:  l.CreatedAt.Unix(),
        })
    }
    return &pb.AuditLogsResponse{Logs: pbLogs, Total: int32(total)}, nil
}

func (s *AdminServer) logAction(adminID, action, resource, resourceID, details, ipAddress string) {
    log := &AdminAuditLog{
        ID:        generateID(),
        AdminID:   adminID,
        Action:    action,
        Resource:  resource,
        ResourceID: resourceID,
        Details:   details,
        IPAddress: ipAddress,
        CreatedAt: time.Now(),
    }
    s.DB.Create(log)
}

func generateID() string {
    return "adm_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateAdminToken(adminID, role string) string {
    return "admin_token_" + adminID + "_" + role
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
        dsn = "host=postgres user=postgres password=postgres dbname=admindb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&AdminUser{}, &AdminAuditLog{})

    // Seed default admin
    var count int64
    db.Model(&AdminUser{}).Count(&count)
    if count == 0 {
        admin := &AdminUser{
            ID:        generateID(),
            Email:     "admin@prosser.com",
            Password:  "admin123",
            FullName:  "Super Admin",
            Role:      "super_admin",
            IsActive:  true,
            CreatedAt: time.Now(),
            UpdatedAt: time.Now(),
        }
        db.Create(admin)
        log.Println("Seeded default admin: admin@prosser.com / admin123")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterAdminServiceServer(grpcServer, &AdminServer{DB: db})

    lis, err := net.Listen("tcp", ":50082")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Admin Service running on port 50082")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}