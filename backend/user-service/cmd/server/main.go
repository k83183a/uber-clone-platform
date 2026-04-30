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

    pb "github.com/uber-clone/user-service/proto"
)

type UserProfile struct {
    ID        string `gorm:"primaryKey"`
    UserID    string `gorm:"uniqueIndex;not null"`
    Email     string `gorm:"uniqueIndex;not null"`
    FullName  string `gorm:"not null"`
    Phone     string `gorm:"not null"`
    AvatarURL string
    DateOfBirth string
    Address   string
    City      string
    Postcode  string
    Country   string `gorm:"default:'UK'"`
    Role      string `gorm:"default:'rider'"`
    IsActive  bool   `gorm:"default:true"`
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt `gorm:"index"`
}

type UserServer struct {
    pb.UnimplementedUserServiceServer
    DB *gorm.DB
}

func (s *UserServer) CreateProfile(ctx context.Context, req *pb.CreateProfileRequest) (*pb.UserResponse, error) {
    // Check if profile already exists
    var existing UserProfile
    if err := s.DB.Where("user_id = ? OR email = ?", req.UserId, req.Email).First(&existing).Error; err == nil {
        return nil, status.Error(codes.AlreadyExists, "profile already exists")
    }

    profile := UserProfile{
        ID:          generateID(),
        UserID:      req.UserId,
        Email:       req.Email,
        FullName:    req.FullName,
        Phone:       req.Phone,
        AvatarURL:   req.AvatarUrl,
        DateOfBirth: req.DateOfBirth,
        Address:     req.Address,
        City:        req.City,
        Postcode:    req.Postcode,
        Country:     req.Country,
        Role:        req.Role,
        IsActive:    true,
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
    }

    if err := s.DB.Create(&profile).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create profile")
    }

    return &pb.UserResponse{
        Id:          profile.ID,
        UserId:      profile.UserID,
        Email:       profile.Email,
        FullName:    profile.FullName,
        Phone:       profile.Phone,
        AvatarUrl:   profile.AvatarURL,
        DateOfBirth: profile.DateOfBirth,
        Address:     profile.Address,
        City:        profile.City,
        Postcode:    profile.Postcode,
        Country:     profile.Country,
        Role:        profile.Role,
        IsActive:    profile.IsActive,
        CreatedAt:   profile.CreatedAt.String(),
    }, nil
}

func (s *UserServer) GetProfile(ctx context.Context, req *pb.GetProfileRequest) (*pb.UserResponse, error) {
    var profile UserProfile
    query := s.DB.Where("is_active = ?", true)

    if req.UserId != "" {
        query = query.Where("user_id = ?", req.UserId)
    } else if req.Email != "" {
        query = query.Where("email = ?", req.Email)
    } else {
        return nil, status.Error(codes.InvalidArgument, "user_id or email required")
    }

    if err := query.First(&profile).Error; err != nil {
        return nil, status.Error(codes.NotFound, "profile not found")
    }

    return &pb.UserResponse{
        Id:          profile.ID,
        UserId:      profile.UserID,
        Email:       profile.Email,
        FullName:    profile.FullName,
        Phone:       profile.Phone,
        AvatarUrl:   profile.AvatarURL,
        DateOfBirth: profile.DateOfBirth,
        Address:     profile.Address,
        City:        profile.City,
        Postcode:    profile.Postcode,
        Country:     profile.Country,
        Role:        profile.Role,
        IsActive:    profile.IsActive,
        CreatedAt:   profile.CreatedAt.String(),
    }, nil
}

func (s *UserServer) UpdateProfile(ctx context.Context, req *pb.UpdateProfileRequest) (*pb.UserResponse, error) {
    var profile UserProfile
    if err := s.DB.Where("user_id = ?", req.UserId).First(&profile).Error; err != nil {
        return nil, status.Error(codes.NotFound, "profile not found")
    }

    if req.FullName != "" {
        profile.FullName = req.FullName
    }
    if req.Phone != "" {
        profile.Phone = req.Phone
    }
    if req.AvatarUrl != "" {
        profile.AvatarURL = req.AvatarUrl
    }
    if req.Address != "" {
        profile.Address = req.Address
    }
    if req.City != "" {
        profile.City = req.City
    }
    if req.Postcode != "" {
        profile.Postcode = req.Postcode
    }
    profile.UpdatedAt = time.Now()

    if err := s.DB.Save(&profile).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update profile")
    }

    return s.GetProfile(ctx, &pb.GetProfileRequest{UserId: req.UserId})
}

func (s *UserServer) DeleteProfile(ctx context.Context, req *pb.DeleteProfileRequest) (*pb.Empty, error) {
    // Soft delete for GDPR compliance
    if err := s.DB.Where("user_id = ?", req.UserId).Delete(&UserProfile{}).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to delete profile")
    }
    return &pb.Empty{}, nil
}

func (s *UserServer) ListProfiles(ctx context.Context, req *pb.ListProfilesRequest) (*pb.ListProfilesResponse, error) {
    var profiles []UserProfile
    query := s.DB.Where("is_active = ?", true)

    if req.Role != "" {
        query = query.Where("role = ?", req.Role)
    }

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&profiles).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list profiles")
    }

    var total int64
    s.DB.Model(&UserProfile{}).Where("is_active = ?", true).Count(&total)

    var responses []*pb.UserResponse
    for _, p := range profiles {
        responses = append(responses, &pb.UserResponse{
            Id:        p.ID,
            UserId:    p.UserID,
            Email:     p.Email,
            FullName:  p.FullName,
            Phone:     p.Phone,
            Role:      p.Role,
            IsActive:  p.IsActive,
            CreatedAt: p.CreatedAt.String(),
        })
    }

    return &pb.ListProfilesResponse{
        Users: responses,
        Total: int32(total),
    }, nil
}

func generateID() string {
    return "prof_" + time.Now().Format("20060102150405") + "_" + randomString(8)
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
        dsn = "host=postgres user=postgres password=postgres dbname=userdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&UserProfile{})

    grpcServer := grpc.NewServer()
    pb.RegisterUserServiceServer(grpcServer, &UserServer{DB: db})

    lis, err := net.Listen("tcp", ":50052")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ User Service running on port 50052")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("User Service stopped")
}