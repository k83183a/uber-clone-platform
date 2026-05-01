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

    pb "github.com/uber-clone/onboarding-service/proto"
)

// OnboardingSession represents a driver's onboarding journey
type OnboardingSession struct {
    ID              string    `gorm:"primaryKey"`
    DriverID        string    `gorm:"uniqueIndex;not null"`
    UserID          string    `gorm:"index;not null"`
    Email           string
    Phone           string
    ReferralCode    string    `gorm:"index"`
    ReferredBy      string
    CurrentStep     int       `gorm:"default:1"`
    StepsCompleted  string    `gorm:"type:text"` // JSON array of completed steps
    Status          string    `gorm:"default:'in_progress'"` // in_progress, completed, abandoned
    DocumentStatus  string    `gorm:"default:'pending'"`
    BackgroundStatus string   `gorm:"default:'pending'"`
    TrainingStatus  string    `gorm:"default:'pending'"`
    PaymentStatus   string    `gorm:"default:'pending'"`
    AdminApproved   bool      `gorm:"default:false"`
    AdminReviewedBy string
    AdminReviewedAt *time.Time
    CompletedAt     *time.Time
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

// OnboardingStep tracks step completion
type OnboardingStep struct {
    ID          string    `gorm:"primaryKey"`
    SessionID   string    `gorm:"index;not null"`
    StepNumber  int       `gorm:"not null"`
    StepName    string    `gorm:"not null"`
    Completed   bool      `gorm:"default:false"`
    CompletedAt *time.Time
    Data        string    `gorm:"type:text"` // JSON step data
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// OnboardingServer handles gRPC requests
type OnboardingServer struct {
    pb.UnimplementedOnboardingServiceServer
    DB *gorm.DB
}

// StartOnboarding initiates the onboarding process for a driver
func (s *OnboardingServer) StartOnboarding(ctx context.Context, req *pb.StartOnboardingRequest) (*pb.StartOnboardingResponse, error) {
    // Check if session already exists
    var existing OnboardingSession
    if err := s.DB.Where("user_id = ?", req.UserId).First(&existing).Error; err == nil {
        return &pb.StartOnboardingResponse{
            SessionId:     existing.ID,
            CurrentStep:   int32(existing.CurrentStep),
            Status:        existing.Status,
            ReferralCode:  existing.ReferralCode,
        }, nil
    }

    // Generate referral code
    referralCode := generateReferralCode(req.FullName)

    session := &OnboardingSession{
        ID:             generateID(),
        DriverID:       generateDriverID(),
        UserID:         req.UserId,
        Email:          req.Email,
        Phone:          req.Phone,
        ReferralCode:   referralCode,
        ReferredBy:     req.ReferredBy,
        CurrentStep:    1,
        StepsCompleted: "[]",
        Status:         "in_progress",
        DocumentStatus: "pending",
        BackgroundStatus: "pending",
        TrainingStatus: "pending",
        PaymentStatus:  "pending",
        AdminApproved:  false,
        CreatedAt:      time.Now(),
        UpdatedAt:      time.Now(),
    }

    if err := s.DB.Create(session).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to start onboarding")
    }

    // Create first step
    step := &OnboardingStep{
        ID:         generateID(),
        SessionID:  session.ID,
        StepNumber: 1,
        StepName:   "account_creation",
        Completed:  true,
        CompletedAt: &time.Now{},
        CreatedAt:  time.Now(),
        UpdatedAt:  time.Now(),
    }
    s.DB.Create(step)

    return &pb.StartOnboardingResponse{
        SessionId:    session.ID,
        CurrentStep:  int32(session.CurrentStep),
        Status:       session.Status,
        ReferralCode: session.ReferralCode,
    }, nil
}

// GetOnboardingStatus returns the current onboarding status
func (s *OnboardingServer) GetOnboardingStatus(ctx context.Context, req *pb.GetOnboardingStatusRequest) (*pb.OnboardingStatusResponse, error) {
    var session OnboardingSession
    query := s.DB
    if req.SessionId != "" {
        query = query.Where("id = ?", req.SessionId)
    } else if req.UserId != "" {
        query = query.Where("user_id = ?", req.UserId)
    } else {
        return nil, status.Error(codes.InvalidArgument, "session_id or user_id required")
    }

    if err := query.First(&session).Error; err != nil {
        return nil, status.Error(codes.NotFound, "onboarding session not found")
    }

    var steps []OnboardingStep
    s.DB.Where("session_id = ?", session.ID).Order("step_number ASC").Find(&steps)

    var stepStatuses []*pb.StepStatus
    for _, st := range steps {
        stepStatuses = append(stepStatuses, &pb.StepStatus{
            StepNumber: int32(st.StepNumber),
            StepName:   st.StepName,
            Completed:  st.Completed,
            CompletedAt: st.CompletedAt.Unix(),
        })
    }

    return &pb.OnboardingStatusResponse{
        SessionId:       session.ID,
        DriverId:        session.DriverID,
        CurrentStep:     int32(session.CurrentStep),
        Status:          session.Status,
        Steps:           stepStatuses,
        DocumentStatus:  session.DocumentStatus,
        BackgroundStatus: session.BackgroundStatus,
        TrainingStatus:  session.TrainingStatus,
        PaymentStatus:   session.PaymentStatus,
        AdminApproved:   session.AdminApproved,
    }, nil
}

// CompleteStep marks a step as completed
func (s *OnboardingServer) CompleteStep(ctx context.Context, req *pb.CompleteStepRequest) (*pb.Empty, error) {
    var session OnboardingSession
    if err := s.DB.Where("id = ?", req.SessionId).First(&session).Error; err != nil {
        return nil, status.Error(codes.NotFound, "session not found")
    }

    // Check if step already completed
    var existingStep OnboardingStep
    if err := s.DB.Where("session_id = ? AND step_number = ?", req.SessionId, req.StepNumber).First(&existingStep).Error; err == nil {
        if existingStep.Completed {
            return &pb.Empty{}, nil
        }
    }

    now := time.Now()
    step := &OnboardingStep{
        ID:          generateID(),
        SessionID:   req.SessionId,
        StepNumber:  int(req.StepNumber),
        StepName:    req.StepName,
        Completed:   true,
        CompletedAt: &now,
        Data:        req.StepData,
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
    }
    s.DB.Create(step)

    // Update session current step
    if int(req.StepNumber) >= session.CurrentStep {
        session.CurrentStep = int(req.StepNumber) + 1
        session.UpdatedAt = now
    }

    s.DB.Save(&session)

    // Check if all steps are completed
    var totalSteps int64
    var completedSteps int64
    s.DB.Model(&OnboardingStep{}).Where("session_id = ?", req.SessionId).Count(&totalSteps)
    s.DB.Model(&OnboardingStep{}).Where("session_id = ? AND completed = ?", req.SessionId, true).Count(&completedSteps)

    if totalSteps >= 8 && completedSteps >= 8 {
        session.Status = "completed"
        session.CompletedAt = &now
        s.DB.Save(&session)
    }

    return &pb.Empty{}, nil
}

// UpdateDocumentStatus updates document verification status
func (s *OnboardingServer) UpdateDocumentStatus(ctx context.Context, req *pb.UpdateDocumentStatusRequest) (*pb.Empty, error) {
    var session OnboardingSession
    if err := s.DB.Where("id = ?", req.SessionId).First(&session).Error; err != nil {
        return nil, status.Error(codes.NotFound, "session not found")
    }

    session.DocumentStatus = req.Status
    session.UpdatedAt = time.Now()
    s.DB.Save(&session)

    return &pb.Empty{}, nil
}

// UpdateBackgroundStatus updates background check status
func (s *OnboardingServer) UpdateBackgroundStatus(ctx context.Context, req *pb.UpdateBackgroundStatusRequest) (*pb.Empty, error) {
    var session OnboardingSession
    if err := s.DB.Where("id = ?", req.SessionId).First(&session).Error; err != nil {
        return nil, status.Error(codes.NotFound, "session not found")
    }

    session.BackgroundStatus = req.Status
    session.UpdatedAt = time.Now()
    s.DB.Save(&session)

    return &pb.Empty{}, nil
}

// UpdateTrainingStatus updates training completion status
func (s *OnboardingServer) UpdateTrainingStatus(ctx context.Context, req *pb.UpdateTrainingStatusRequest) (*pb.Empty, error) {
    var session OnboardingSession
    if err := s.DB.Where("id = ?", req.SessionId).First(&session).Error; err != nil {
        return nil, status.Error(codes.NotFound, "session not found")
    }

    session.TrainingStatus = req.Status
    session.UpdatedAt = time.Now()
    s.DB.Save(&session)

    return &pb.Empty{}, nil
}

// UpdatePaymentStatus updates Stripe Connect status
func (s *OnboardingServer) UpdatePaymentStatus(ctx context.Context, req *pb.UpdatePaymentStatusRequest) (*pb.Empty, error) {
    var session OnboardingSession
    if err := s.DB.Where("id = ?", req.SessionId).First(&session).Error; err != nil {
        return nil, status.Error(codes.NotFound, "session not found")
    }

    session.PaymentStatus = req.Status
    session.UpdatedAt = time.Now()
    s.DB.Save(&session)

    return &pb.Empty{}, nil
}

// AdminApprove approves a driver's onboarding application
func (s *OnboardingServer) AdminApprove(ctx context.Context, req *pb.AdminApproveRequest) (*pb.Empty, error) {
    var session OnboardingSession
    if err := s.DB.Where("id = ?", req.SessionId).First(&session).Error; err != nil {
        return nil, status.Error(codes.NotFound, "session not found")
    }

    now := time.Now()
    session.AdminApproved = true
    session.AdminReviewedBy = req.AdminId
    session.AdminReviewedAt = &now
    session.Status = "completed"
    session.CompletedAt = &now
    session.UpdatedAt = now

    if err := s.DB.Save(&session).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to approve onboarding")
    }

    return &pb.Empty{}, nil
}

// AdminReject rejects a driver's onboarding application
func (s *OnboardingServer) AdminReject(ctx context.Context, req *pb.AdminRejectRequest) (*pb.Empty, error) {
    var session OnboardingSession
    if err := s.DB.Where("id = ?", req.SessionId).First(&session).Error; err != nil {
        return nil, status.Error(codes.NotFound, "session not found")
    }

    session.Status = "rejected"
    session.AdminReviewedBy = req.AdminId
    session.AdminReviewedAt = &time.Now{}
    session.UpdatedAt = time.Now()

    if err := s.DB.Save(&session).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to reject onboarding")
    }

    return &pb.Empty{}, nil
}

// ListPendingApplications lists onboarding applications pending admin review
func (s *OnboardingServer) ListPendingApplications(ctx context.Context, req *pb.ListPendingApplicationsRequest) (*pb.ListPendingApplicationsResponse, error) {
    var sessions []OnboardingSession
    query := s.DB.Where("status = ? AND admin_approved = ?", "in_progress", false)

    if err := query.Find(&sessions).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list pending applications")
    }

    var pbSessions []*pb.OnboardingSummary
    for _, sess := range sessions {
        pbSessions = append(pbSessions, &pb.OnboardingSummary{
            SessionId:      sess.ID,
            DriverId:       sess.DriverID,
            Email:          sess.Email,
            Phone:          sess.Phone,
            CurrentStep:    int32(sess.CurrentStep),
            DocumentStatus: sess.DocumentStatus,
            BackgroundStatus: sess.BackgroundStatus,
            CreatedAt:      sess.CreatedAt.Unix(),
        })
    }

    return &pb.ListPendingApplicationsResponse{Applications: pbSessions}, nil
}

func generateID() string {
    return "onb_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateDriverID() string {
    return "DRV" + time.Now().Format("20060102") + randomString(6)
}

func generateReferralCode(name string) string {
    code := name[:min(5, len(name))] + randomString(5)
    return strings.ToUpper(code)
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
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
        dsn = "host=postgres user=postgres password=postgres dbname=onboardingdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&OnboardingSession{}, &OnboardingStep{})

    grpcServer := grpc.NewServer()
    pb.RegisterOnboardingServiceServer(grpcServer, &OnboardingServer{DB: db})

    lis, err := net.Listen("tcp", ":50076")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Onboarding Service running on port 50076")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Onboarding Service stopped")
}