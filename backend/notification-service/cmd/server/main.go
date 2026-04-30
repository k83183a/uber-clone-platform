package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net"
    "net/http"
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

    pb "github.com/uber-clone/notification-service/proto"
)

// NotificationLog stores notification history
type NotificationLog struct {
    ID        string    `gorm:"primaryKey"`
    UserID    string    `gorm:"index;not null"`
    Type      string    `gorm:"index"` // push, email, sms
    Title     string
    Body      string
    Data      string    `gorm:"type:jsonb"`
    Status    string    // sent, failed, pending
    Provider  string    // fcm, apns, ses, twilio
    CreatedAt time.Time
}

// UserNotificationPreferences stores user preferences
type UserNotificationPreferences struct {
    UserID      string `gorm:"primaryKey"`
    PushEnabled bool   `gorm:"default:true"`
    EmailEnabled bool  `gorm:"default:true"`
    SmsEnabled  bool   `gorm:"default:false"`
    PushToken   string
    Email       string
    Phone       string
    UpdatedAt   time.Time
}

// NotificationServer handles gRPC requests
type NotificationServer struct {
    pb.UnimplementedNotificationServiceServer
    DB            *gorm.DB
    FCMAPIKey     string
    SESAccessKey  string
    SESSecretKey  string
    SESRegion     string
    TwilioSID     string
    TwilioToken   string
    TwilioFrom    string
    EmailFrom     string
}

// SendPush sends a push notification via FCM
func (s *NotificationServer) SendPush(ctx context.Context, req *pb.SendPushRequest) (*pb.SendResponse, error) {
    // Get user preferences
    var prefs UserNotificationPreferences
    if err := s.DB.Where("user_id = ?", req.UserId).First(&prefs).Error; err != nil {
        // Create default preferences if not exists
        prefs = UserNotificationPreferences{
            UserID:      req.UserId,
            PushEnabled: true,
            EmailEnabled: true,
            SmsEnabled:  false,
        }
        s.DB.Create(&prefs)
    }

    if !prefs.PushEnabled || prefs.PushToken == "" {
        // Push not enabled, skip
        return &pb.SendResponse{Success: false, Message: "push not enabled"}, nil
    }

    // Send via FCM
    success, message := s.sendFCM(prefs.PushToken, req.Title, req.Body, req.Data)

    // Log notification
    dataJSON, _ := json.Marshal(req.Data)
    log := &NotificationLog{
        ID:        generateID(),
        UserID:    req.UserId,
        Type:      "push",
        Title:     req.Title,
        Body:      req.Body,
        Data:      string(dataJSON),
        Status:    map[bool]string{true: "sent", false: "failed"}[success],
        Provider:  "fcm",
        CreatedAt: time.Now(),
    }
    s.DB.Create(log)

    return &pb.SendResponse{Success: success, Message: message}, nil
}

// SendEmail sends an email via AWS SES
func (s *NotificationServer) SendEmail(ctx context.Context, req *pb.SendEmailRequest) (*pb.SendResponse, error) {
    var prefs UserNotificationPreferences
    if err := s.DB.Where("user_id = ?", req.UserId).First(&prefs).Error; err != nil {
        prefs = UserNotificationPreferences{UserID: req.UserId, Email: req.Email}
        s.DB.Create(&prefs)
    }

    emailTo := prefs.Email
    if req.Email != "" {
        emailTo = req.Email
    }

    if emailTo == "" {
        return &pb.SendResponse{Success: false, Message: "no email address"}, nil
    }

    // Send via AWS SES
    success, message := s.sendSES(emailTo, req.Subject, req.HtmlBody)

    // Log
    log := &NotificationLog{
        ID:        generateID(),
        UserID:    req.UserId,
        Type:      "email",
        Title:     req.Subject,
        Body:      req.HtmlBody,
        Status:    map[bool]string{true: "sent", false: "failed"}[success],
        Provider:  "ses",
        CreatedAt: time.Now(),
    }
    s.DB.Create(log)

    return &pb.SendResponse{Success: success, Message: message}, nil
}

// SendSMS sends an SMS via Twilio
func (s *NotificationServer) SendSMS(ctx context.Context, req *pb.SendSMSRequest) (*pb.SendResponse, error) {
    var prefs UserNotificationPreferences
    if err := s.DB.Where("user_id = ?", req.UserId).First(&prefs).Error; err != nil {
        prefs = UserNotificationPreferences{UserID: req.UserId, Phone: req.Phone}
        s.DB.Create(&prefs)
    }

    phoneTo := prefs.Phone
    if req.Phone != "" {
        phoneTo = req.Phone
    }

    if phoneTo == "" {
        return &pb.SendResponse{Success: false, Message: "no phone number"}, nil
    }

    // Send via Twilio
    success, message := s.sendTwilio(phoneTo, req.Message)

    // Log
    log := &NotificationLog{
        ID:        generateID(),
        UserID:    req.UserId,
        Type:      "sms",
        Body:      req.Message,
        Status:    map[bool]string{true: "sent", false: "failed"}[success],
        Provider:  "twilio",
        CreatedAt: time.Now(),
    }
    s.DB.Create(log)

    return &pb.SendResponse{Success: success, Message: message}, nil
}

// UpdatePreferences updates user notification preferences
func (s *NotificationServer) UpdatePreferences(ctx context.Context, req *pb.UpdatePreferencesRequest) (*pb.PreferencesResponse, error) {
    var prefs UserNotificationPreferences
    if err := s.DB.Where("user_id = ?", req.UserId).First(&prefs).Error; err != nil {
        prefs = UserNotificationPreferences{UserID: req.UserId}
    }

    prefs.PushEnabled = req.PushEnabled
    prefs.EmailEnabled = req.EmailEnabled
    prefs.SmsEnabled = req.SmsEnabled
    prefs.PushToken = req.PushToken
    prefs.Email = req.Email
    prefs.Phone = req.Phone
    prefs.UpdatedAt = time.Now()

    s.DB.Save(&prefs)

    return &pb.PreferencesResponse{
        PushEnabled:  prefs.PushEnabled,
        EmailEnabled: prefs.EmailEnabled,
        SmsEnabled:   prefs.SmsEnabled,
    }, nil
}

// GetPreferences gets user notification preferences
func (s *NotificationServer) GetPreferences(ctx context.Context, req *pb.GetPreferencesRequest) (*pb.PreferencesResponse, error) {
    var prefs UserNotificationPreferences
    if err := s.DB.Where("user_id = ?", req.UserId).First(&prefs).Error; err != nil {
        return &pb.PreferencesResponse{
            PushEnabled:  true,
            EmailEnabled: true,
            SmsEnabled:   false,
        }, nil
    }

    return &pb.PreferencesResponse{
        PushEnabled:  prefs.PushEnabled,
        EmailEnabled: prefs.EmailEnabled,
        SmsEnabled:   prefs.SmsEnabled,
    }, nil
}

// sendFCM sends push notification via Firebase Cloud Messaging
func (s *NotificationServer) sendFCM(token, title, body string, data map[string]string) (bool, string) {
    if s.FCMAPIKey == "" {
        log.Println("FCM API key not configured")
        return false, "FCM not configured"
    }

    // Build FCM payload
    payload := map[string]interface{}{
        "to": token,
        "notification": map[string]string{
            "title": title,
            "body":  body,
        },
        "data": data,
    }

    jsonPayload, _ := json.Marshal(payload)

    req, err := http.NewRequest("POST", "https://fcm.googleapis.com/fcm/send", bytes.NewBuffer(jsonPayload))
    if err != nil {
        return false, err.Error()
    }

    req.Header.Set("Authorization", "key="+s.FCMAPIKey)
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return false, err.Error()
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return false, fmt.Sprintf("FCM error: %d", resp.StatusCode)
    }

    return true, "sent"
}

// sendSES sends email via AWS SES
func (s *NotificationServer) sendSES(to, subject, htmlBody string) (bool, string) {
    // In production, use AWS SDK to send email via SES
    log.Printf("Sending email to %s: %s", to, subject)
    return true, "sent"
}

// sendTwilio sends SMS via Twilio
func (s *NotificationServer) sendTwilio(to, message string) (bool, string) {
    // In production, use Twilio SDK
    log.Printf("Sending SMS to %s: %s", to, message)
    return true, "sent"
}

// Health check endpoint
func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
}

func generateID() string {
    return "notif_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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

    // Database connection
    dsn := os.Getenv("DB_DSN")
    if dsn == "" {
        dsn = "host=postgres user=postgres password=postgres dbname=notificationdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }
    db.AutoMigrate(&NotificationLog{}, &UserNotificationPreferences{})

    // Initialize server
    notificationServer := &NotificationServer{
        DB:           db,
        FCMAPIKey:    os.Getenv("FCM_API_KEY"),
        SESAccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
        SESSecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
        SESRegion:    os.Getenv("AWS_REGION"),
        TwilioSID:    os.Getenv("TWILIO_ACCOUNT_SID"),
        TwilioToken:  os.Getenv("TWILIO_AUTH_TOKEN"),
        TwilioFrom:   os.Getenv("TWILIO_FROM_NUMBER"),
        EmailFrom:    os.Getenv("EMAIL_FROM"),
    }

    // gRPC server
    grpcServer := grpc.NewServer()
    pb.RegisterNotificationServiceServer(grpcServer, notificationServer)

    // HTTP server for health checks
    http.HandleFunc("/health", healthHandler)
    go func() {
        log.Println("✅ Notification Service HTTP running on port 8083")
        log.Fatal(http.ListenAndServe(":8083", nil))
    }()

    // gRPC listener
    lis, err := net.Listen("tcp", ":50055")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Notification Service gRPC running on port 50055")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Notification Service stopped")
}