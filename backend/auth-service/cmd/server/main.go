package main

import (
    "context"
    "log"
    "net"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "github.com/joho/godotenv"
    "golang.org/x/crypto/bcrypt"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/uber-clone/auth-service/proto"
)

type User struct {
    ID        string `gorm:"primaryKey"`
    Email     string `gorm:"uniqueIndex;not null"`
    Phone     string `gorm:"uniqueIndex;not null"`
    Password  string `gorm:"not null"`
    Role      string `gorm:"default:'rider'"`
    CreatedAt time.Time
    UpdatedAt time.Time
}

type AuthServer struct {
    pb.UnimplementedAuthServiceServer
    DB        *gorm.DB
    JWTSecret []byte
}

func (s *AuthServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.AuthResponse, error) {
    // Check if user exists
    var existing User
    if err := s.DB.Where("email = ? OR phone = ?", req.Email, req.Phone).First(&existing).Error; err == nil {
        return nil, status.Error(codes.AlreadyExists, "user already exists")
    }

    // Hash password
    hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to hash password")
    }

    // Create user
    user := User{
        ID:        generateUUID(),
        Email:     req.Email,
        Phone:     req.Phone,
        Password:  string(hashedPassword),
        Role:      req.Role,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }
    if err := s.DB.Create(&user).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create user")
    }

    // Generate JWT
    token, err := s.generateJWT(user.ID, user.Role)
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to generate token")
    }

    return &pb.AuthResponse{
        Token:     token,
        UserId:    user.ID,
        Email:     user.Email,
        Role:      user.Role,
        ExpiresIn: 86400,
    }, nil
}

func (s *AuthServer) Login(ctx context.Context, req *pb.LoginRequest) (*pb.AuthResponse, error) {
    var user User
    if err := s.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
        return nil, status.Error(codes.NotFound, "user not found")
    }

    if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
        return nil, status.Error(codes.Unauthenticated, "invalid credentials")
    }

    token, err := s.generateJWT(user.ID, user.Role)
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to generate token")
    }

    return &pb.AuthResponse{
        Token:     token,
        UserId:    user.ID,
        Email:     user.Email,
        Role:      user.Role,
        ExpiresIn: 86400,
    }, nil
}

func (s *AuthServer) ValidateToken(ctx context.Context, req *pb.ValidateRequest) (*pb.ValidateResponse, error) {
    token, err := jwt.Parse(req.Token, func(token *jwt.Token) (interface{}, error) {
        return s.JWTSecret, nil
    })
    if err != nil || !token.Valid {
        return &pb.ValidateResponse{Valid: false}, nil
    }

    claims, ok := token.Claims.(jwt.MapClaims)
    if !ok {
        return &pb.ValidateResponse{Valid: false}, nil
    }

    return &pb.ValidateResponse{
        Valid:  true,
        UserId: claims["user_id"].(string),
        Role:   claims["role"].(string),
    }, nil
}

func (s *AuthServer) generateJWT(userID, role string) (string, error) {
    claims := jwt.MapClaims{
        "user_id": userID,
        "role":    role,
        "exp":     time.Now().Add(24 * time.Hour).Unix(),
        "iat":     time.Now().Unix(),
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(s.JWTSecret)
}

func generateUUID() string {
    return "user_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func randomString(n int) string {
    const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
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
        dsn = "host=postgres user=postgres password=postgres dbname=authdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&User{})

    jwtSecret := []byte(os.Getenv("JWT_SECRET"))
    if len(jwtSecret) == 0 {
        jwtSecret = []byte("default-super-secret-key-change-in-production")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterAuthServiceServer(grpcServer, &AuthServer{
        DB:        db,
        JWTSecret: jwtSecret,
    })

    lis, err := net.Listen("tcp", ":50051")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Auth Service running on port 50051")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Auth Service stopped")
}