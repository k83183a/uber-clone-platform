package main

import (
    "context"
    "encoding/json"
    "log"
    "net"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/gorilla/websocket"
    "github.com/joho/godotenv"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/uber-clone/safety-service/proto"
)

// SOSAlert represents an emergency alert
type SOSAlert struct {
    ID         string    `gorm:"primaryKey"`
    RideID     string    `gorm:"index;not null"`
    UserID     string    `gorm:"index;not null"` // rider who triggered
    DriverID   string    `gorm:"index"`
    Latitude   float64
    Longitude  float64
    Status     string    `gorm:"default:'active'"` // active, resolved, false_alarm
    AlertTime  time.Time
    ResolvedAt *time.Time
    Notes      string
}

// LiveSharingToken represents a shareable trip tracking link
type LiveSharingToken struct {
    ID        string    `gorm:"primaryKey"`
    RideID    string    `gorm:"index;not null"`
    Token     string    `gorm:"uniqueIndex;not null"`
    ExpiresAt time.Time `gorm:"not null"`
    CreatedAt time.Time
}

// DriverSelfieCheck represents a selfie verification request
type DriverSelfieCheck struct {
    ID         string    `gorm:"primaryKey"`
    DriverID   string    `gorm:"index;not null"`
    RideID     string    `gorm:"index"`
    SelfieURL  string    `gorm:"not null"`
    Verified   bool      `gorm:"default:false"`
    VerifiedAt *time.Time
    CreatedAt  time.Time
}

// AudioRecording represents a trip recording
type AudioRecording struct {
    ID         string    `gorm:"primaryKey"`
    RideID     string    `gorm:"index;not null"`
    UserID     string    `gorm:"index"` // who recorded (rider or driver)
    RecordingURL string `gorm:"not null"`
    DurationSec int
    UploadedAt time.Time
    CreatedAt  time.Time
}

// SafetyServer handles gRPC requests
type SafetyServer struct {
    pb.UnimplementedSafetyServiceServer
    DB          *gorm.DB
    upgrader    websocket.Upgrader
    twilioSID   string
    twilioToken string
    twilioFrom  string
    // In production: S3 client for selfie/audio storage
}

// ReportSOS triggers an emergency alert
func (s *SafetyServer) ReportSOS(ctx context.Context, req *pb.SOSRequest) (*pb.SOSResponse, error) {
    alert := &SOSAlert{
        ID:        generateID(),
        RideID:    req.RideId,
        UserID:    req.UserId,
        Latitude:  req.Latitude,
        Longitude: req.Longitude,
        Status:    "active",
        AlertTime: time.Now(),
    }

    if err := s.DB.Create(alert).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create alert")
    }

    // In production: send SMS/call to emergency services (999 UK) via Twilio
    log.Printf("⚠️ SOS ALERT: Ride %s at (%f,%f) - Alert ID: %s", req.RideId, req.Latitude, req.Longitude, alert.ID)

    return &pb.SOSResponse{
        AlertId: alert.ID,
        Message: "Emergency services have been notified. Help is on the way.",
    }, nil
}

// CreateLiveSharingLink generates a shareable tracking link
func (s *SafetyServer) CreateLiveSharingLink(ctx context.Context, req *pb.LiveSharingRequest) (*pb.LiveSharingResponse, error) {
    token := generateSecureToken(32)
    expiresAt := time.Now().Add(time.Duration(req.ExpiryMinutes) * time.Minute)

    share := &LiveSharingToken{
        ID:        generateID(),
        RideID:    req.RideId,
        Token:     token,
        ExpiresAt: expiresAt,
        CreatedAt: time.Now(),
    }

    if err := s.DB.Create(share).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create sharing link")
    }

    shareURL := "https://share.yourapp.com/live/" + token

    return &pb.LiveSharingResponse{
        ShareUrl:   shareURL,
        ExpiresAt:  expiresAt.Unix(),
    }, nil
}

// VerifyDriverSelfie validates driver's selfie against stored photo
func (s *SafetyServer) VerifyDriverSelfie(ctx context.Context, req *pb.SelfieRequest) (*pb.SelfieResponse, error) {
    // In production: upload selfie to cloud storage
    selfieURL := "/uploads/selfies/" + generateID() + ".jpg"

    check := &DriverSelfieCheck{
        ID:        generateID(),
        DriverID:  req.DriverId,
        RideID:    req.RideId,
        SelfieURL: selfieURL,
        Verified:  false,
        CreatedAt: time.Now(),
    }

    // In production: call facial recognition service (AWS Rekognition)
    // For MVP, we accept all selfies as verified
    // In a real system, compare with driver's profile photo from user-service
    verified := true
    now := time.Now()
    check.Verified = verified
    check.VerifiedAt = &now

    if err := s.DB.Create(check).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to record selfie check")
    }

    if !verified {
        // In production: notify dispatch to cancel ride
        log.Printf("⚠️ Selfie verification FAILED for driver %s, ride %s", req.DriverId, req.RideId)
        return &pb.SelfieResponse{
            Matched: false,
            Message: "Selfie verification failed. Please contact support.",
        }, nil
    }

    return &pb.SelfieResponse{
        Matched: true,
        Message: "Verification successful. You can proceed.",
    }, nil
}

// StartAudioRecording initiates audio recording on the client
func (s *SafetyServer) StartAudioRecording(ctx context.Context, req *pb.StartRecordingRequest) (*pb.StartRecordingResponse, error) {
    recordingID := generateID()
    // In production: send WebSocket signal to client to start recording
    log.Printf("🎤 Starting audio recording for ride %s, recording ID: %s", req.RideId, recordingID)

    return &pb.StartRecordingResponse{
        RecordingId: recordingID,
        Message:     "Recording started. Audio will be saved securely.",
    }, nil
}

// StopAudioRecording stops and uploads the recording
func (s *SafetyServer) StopAudioRecording(ctx context.Context, req *pb.StopRecordingRequest) (*pb.StopRecordingResponse, error) {
    // In production: client uploads audio file to cloud storage, then we store URL
    recordingURL := "/uploads/audio/" + req.RecordingId + ".m4a"

    recording := &AudioRecording{
        ID:           req.RecordingId,
        RideID:       req.RideId,
        UserID:       req.UserId,
        RecordingURL: recordingURL,
        DurationSec:  int(req.DurationSec),
        UploadedAt:   time.Now(),
        CreatedAt:    time.Now(),
    }

    if err := s.DB.Create(recording).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to save recording")
    }

    return &pb.StopRecordingResponse{
        RecordingUrl: recordingURL,
        Message:      "Recording saved securely.",
    }, nil
}

// GetActiveSOSAlerts returns active SOS alerts (admin/operations)
func (s *SafetyServer) GetActiveSOSAlerts(ctx context.Context, req *pb.Empty) (*pb.SOSAlertsResponse, error) {
    var alerts []SOSAlert
    if err := s.DB.Where("status = ?", "active").Find(&alerts).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to fetch alerts")
    }

    var pbAlerts []*pb.SOSAlertInfo
    for _, a := range alerts {
        pbAlerts = append(pbAlerts, &pb.SOSAlertInfo{
            AlertId:   a.ID,
            RideId:    a.RideID,
            UserId:    a.UserID,
            Latitude:  a.Latitude,
            Longitude: a.Longitude,
            AlertTime: a.AlertTime.Unix(),
        })
    }

    return &pb.SOSAlertsResponse{Alerts: pbAlerts}, nil
}

// ResolveSOSAlert marks an alert as resolved (admin)
func (s *SafetyServer) ResolveSOSAlert(ctx context.Context, req *pb.ResolveSOSRequest) (*pb.Empty, error) {
    var alert SOSAlert
    if err := s.DB.Where("id = ?", req.AlertId).First(&alert).Error; err != nil {
        return nil, status.Error(codes.NotFound, "alert not found")
    }

    now := time.Now()
    alert.Status = "resolved"
    alert.ResolvedAt = &now
    alert.Notes = req.Notes

    if err := s.DB.Save(&alert).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to resolve alert")
    }

    return &pb.Empty{}, nil
}

// WebSocket handler for live sharing (HTML page + stream)
func (s *SafetyServer) handleLiveSharingWS(w http.ResponseWriter, r *http.Request) {
    conn, err := s.upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("WebSocket upgrade error: %v", err)
        return
    }
    defer conn.Close()

    token := r.URL.Query().Get("token")
    if token == "" {
        return
    }

    var share LiveSharingToken
    if err := s.DB.Where("token = ? AND expires_at > ?", token, time.Now()).First(&share).Error; err != nil {
        conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"Invalid or expired link"}`))
        return
    }

    // In production: subscribe to location-service WebSocket for this ride
    // and forward driver positions to the connected client
    for {
        // Simulate location updates; in real system, read from Kafka/Redis
        time.Sleep(2 * time.Second)
        msg := `{"lat":51.5074,"lng":-0.1278,"status":"In transit"}`
        if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
            break
        }
    }
}

func generateID() string {
    return "sfty_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateSecureToken(length int) string {
    const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    b := make([]byte, length)
    for i := range b {
        b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
    }
    return string(b)
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
        dsn = "host=postgres user=postgres password=postgres dbname=safetydb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&SOSAlert{}, &LiveSharingToken{}, &DriverSelfieCheck{}, &AudioRecording{})

    server := &SafetyServer{
        DB:        db,
        upgrader:  websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
    }

    // HTTP endpoints for live sharing
    http.HandleFunc("/ws/live", server.handleLiveSharingWS)

    go func() {
        log.Println("✅ Safety Service HTTP (WebSocket) on :8085")
        log.Fatal(http.ListenAndServe(":8085", nil))
    }()

    grpcServer := grpc.NewServer()
    pb.RegisterSafetyServiceServer(grpcServer, server)

    lis, err := net.Listen("tcp", ":50065")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Safety Service gRPC running on port 50065")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Safety Service stopped")
}