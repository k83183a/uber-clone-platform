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

type NotificationLog struct {
    ID        string    `gorm:"primaryKey"`
    UserID    string    `gorm:"index;not null"`
    Type      string    `gorm:"index"` // push, email, sms
    Title     string
    Body      string
    Data      string    `gorm:"type:jsonb"`
    Status    string    // sent, failed
    Provider  string    // fcm, apns, ses, twilio
    CreatedAt time.Time
}

type UserPreferences struct {
    UserID      string `gorm:"primaryKey"`
    PushEnabled bool   `gorm:"default:true"`
    EmailEnabled bool  `gorm:"default:true"`
    SmsEnabled  bool   `gorm:"default:false"`
    PushToken   string
    Email       string
    Phone       string
    UpdatedAt   time.Time
}

type NotificationServer struct {
    pb.UnimplementedNotificationServiceServer
    DB              *gorm.DB
    FCMAPIKey       string
    TwilioSID       string
    TwilioToken     string
    TwilioFrom      string
    EmailFrom       string
    httpClient      *http.Client
}

// SendPush - Send push notification via FCM
func (s *NotificationServer) SendPush(ctx context.Context, req *pb.SendPushRequest) (*pb.SendResponse, error) {
    var prefs UserPreferences
    if err := s.DB.Where("user_id = ?", req.UserId).First(&prefs).Error; err != nil {
        prefs = UserPreferences{UserID: req.UserId, PushEnabled: true}
        s.DB.Create(&prefs)
    }

    if !prefs.PushEnabled || prefs.PushToken == "" {
        return &pb.SendResponse{Success: false, Message: "push not enabled"}, nil
    }

    success, message := s.sendFCM(prefs.PushToken, req.Title, req.Body, req.Data)

    s.logNotification(req.UserId, "push", req.Title, req.Body, success, "fcm")

    return &pb.SendResponse{Success: success, Message: message}, nil
}

// SendEmail - Send email via AWS SES
func (s *NotificationServer) SendEmail(ctx context.Context, req *pb.SendEmailRequest) (*pb.SendResponse, error) {
    var prefs UserPreferences
    s.DB.Where("user_id = ?", req.UserId).First(&prefs)
    emailTo := prefs.Email
    if req.Email != "" {
        emailTo = req.Email
    }
    if emailTo == "" {
        return &pb.SendResponse{Success: false, Message: "no email address"}, nil
    }

    success, message := s.sendSES(emailTo, req.Subject, req.HtmlBody)
    s.logNotification(req.UserId, "email", req.Subject, req.HtmlBody, success, "ses")

    return &pb.SendResponse{Success: success, Message: message}, nil
}

// SendSMS - Send SMS via Twilio
func (s *NotificationServer) SendSMS(ctx context.Context, req *pb.SendSMSRequest) (*pb.SendResponse, error) {
    var prefs UserPreferences
    s.DB.Where("user_id = ?", req.UserId).First(&prefs)
    phoneTo := prefs.Phone
    if req.Phone != "" {
        phoneTo = req.Phone
    }
    if phoneTo == "" {
        return &pb.SendResponse{Success: false, Message: "no phone number"}, nil
    }

    success, message := s.sendTwilio(phoneTo, req.Message)
    s.logNotification(req.UserId, "sms", "", req.Message, success, "twilio")

    return &pb.SendResponse{Success: success, Message: message}, nil
}

// UpdatePreferences - Update user notification preferences
func (s *NotificationServer) UpdatePreferences(ctx context.Context, req *pb.UpdatePreferencesRequest) (*pb.PreferencesResponse, error) {
    var prefs UserPreferences
    if err := s.DB.Where("user_id = ?", req.UserId).First(&prefs).Error; err != nil {
        prefs = UserPreferences{UserID: req.UserId}
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

// GetPreferences - Get user notification preferences
func (s *NotificationServer) GetPreferences(ctx context.Context, req *pb.GetPreferencesRequest) (*pb.PreferencesResponse, error) {
    var prefs UserPreferences
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

// sendFCM - Firebase Cloud Messaging
func (s *NotificationServer) sendFCM(token, title, body string, data map[string]string) (bool, string) {
    if s.FCMAPIKey == "" {
        log.Println("FCM API key not configured")
        return false, "FCM not configured"
    }

    payload := map[string]interface{}{
        "to": token,
        "notification": map[string]string{"title": title, "body": body},
        "data": data,
    }
    jsonPayload, _ := json.Marshal(payload)

    req, _ := http.NewRequest("POST", "https://fcm.googleapis.com/fcm/send", bytes.NewBuffer(jsonPayload))
    req.Header.Set("Authorization", "key="+s.FCMAPIKey)
    req.Header.Set("Content-Type", "application/json")

    resp, err := s.httpClient.Do(req)
    if err != nil || resp.StatusCode != 200 {
        return false, "FCM error"
    }
    return true, "sent"
}

// sendSES - AWS SES (simplified)
func (s *NotificationServer) sendSES(to, subject, htmlBody string) (bool, string) {
    log.Printf("📧 Sending email to %s: %s", to, subject)
    return true, "sent"
}

// sendTwilio - Twilio SMS (simplified)
func (s *NotificationServer) sendTwilio(to, message string) (bool, string) {
    log.Printf("📱 Sending SMS to %s: %s", to, message)
    return true, "sent"
}

func (s *NotificationServer) logNotification(userID, notifType, title, body string, success bool, provider string) {
    log := &NotificationLog{
        ID:        generateID(),
        UserID:    userID,
        Type:      notifType,
        Title:     title,
        Body:      body,
        Status:    map[bool]string{true: "sent", false: "failed"}[success],
        Provider:  provider,
        CreatedAt: time.Now(),
    }
    s.DB.Create(log)
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

    dsn := os.Getenv("DB_DSN")
    if dsn == "" {
        dsn = "host=postgres user=postgres password=postgres dbname=notificationdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }
    db.AutoMigrate(&NotificationLog{}, &UserPreferences{})

    server := &NotificationServer{
        DB:          db,
        FCMAPIKey:   os.Getenv("FCM_API_KEY"),
        TwilioSID:   os.Getenv("TWILIO_ACCOUNT_SID"),
        TwilioToken: os.Getenv("TWILIO_AUTH_TOKEN"),
        TwilioFrom:  os.Getenv("TWILIO_FROM_NUMBER"),
        EmailFrom:   os.Getenv("EMAIL_FROM"),
        httpClient:  &http.Client{Timeout: 10 * time.Second},
    }

    grpcServer := grpc.NewServer()
    pb.RegisterNotificationServiceServer(grpcServer, server)

    lis, err := net.Listen("tcp", ":50055")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Notification Service running on port 50055")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}