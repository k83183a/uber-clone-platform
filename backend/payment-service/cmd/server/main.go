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

    "github.com/joho/godotenv"
    "github.com/stripe/stripe-go/v76"
    "github.com/stripe/stripe-go/v76/paymentintent"
    "github.com/stripe/stripe-go/v76/webhook"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/uber-clone/payment-service/proto"
)

// Transaction represents a payment transaction
type Transaction struct {
    ID              string    `gorm:"primaryKey"`
    UserID          string    `gorm:"index;not null"`
    DriverID        string    `gorm:"index"`
    ServiceType     string    `gorm:"not null"` // ride, food, grocery, courier
    ServiceID       string    `gorm:"index;not null"`
    Amount          float64   `gorm:"not null"`
    Currency        string    `gorm:"default:'GBP'"`
    Status          string    `gorm:"default:'pending'"` // pending, succeeded, failed, refunded
    PaymentMethod   string    // card, apple_pay, google_pay, paypal, klarna, open_banking
    StripePaymentID string    `gorm:"index"`
    StripeIntentID  string
    PlatformFee     float64
    DriverPayout    float64
    Metadata        string    `gorm:"type:jsonb"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

// PaymentServer handles gRPC requests
type PaymentServer struct {
    pb.UnimplementedPaymentServiceServer
    DB           *gorm.DB
    StripeKey    string
    WebhookSecret string
}

// CreatePaymentIntent creates a Stripe PaymentIntent
func (s *PaymentServer) CreatePaymentIntent(ctx context.Context, req *pb.CreatePaymentIntentRequest) (*pb.PaymentIntentResponse, error) {
    stripe.Key = s.StripeKey

    // Calculate platform fee (20% commission)
    platformFeePercent := 0.20
    platformFee := int64(float64(req.AmountCents) * platformFeePercent)
    driverAmount := req.AmountCents - platformFee

    params := &stripe.PaymentIntentParams{
        Amount:             stripe.Int64(req.AmountCents),
        Currency:           stripe.String(req.Currency),
        PaymentMethodTypes: stripe.StringSlice([]string{"card", "apple_pay", "google_pay"}),
        Metadata: map[string]string{
            "user_id":      req.UserId,
            "service_type": req.ServiceType,
            "service_id":   req.ServiceId,
        },
        TransferData: &stripe.PaymentIntentTransferDataParams{
            Amount:      stripe.Int64(driverAmount),
            Destination: stripe.String(req.ConnectAccountId),
        },
        ApplicationFeeAmount: stripe.Int64(platformFee),
    }

    pi, err := paymentintent.New(params)
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to create payment intent: "+err.Error())
    }

    // Store transaction in database
    transaction := &Transaction{
        ID:              generateTransactionID(),
        UserID:          req.UserId,
        ServiceType:     req.ServiceType,
        ServiceID:       req.ServiceId,
        Amount:          float64(req.AmountCents) / 100,
        Currency:        req.Currency,
        Status:          "pending",
        StripeIntentID:  pi.ID,
        PlatformFee:     float64(platformFee) / 100,
        DriverPayout:    float64(driverAmount) / 100,
        Metadata:        "{}",
        CreatedAt:       time.Now(),
        UpdatedAt:       time.Now(),
    }

    if err := s.DB.Create(transaction).Error; err != nil {
        log.Printf("Failed to store transaction: %v", err)
    }

    return &pb.PaymentIntentResponse{
        ClientSecret: pi.ClientSecret,
        IntentId:     pi.ID,
    }, nil
}

// ConfirmPayment confirms a payment intent
func (s *PaymentServer) ConfirmPayment(ctx context.Context, req *pb.ConfirmPaymentRequest) (*pb.ConfirmPaymentResponse, error) {
    stripe.Key = s.StripeKey

    pi, err := paymentintent.Confirm(req.IntentId, &stripe.PaymentIntentConfirmParams{
        PaymentMethod: stripe.String(req.PaymentMethodId),
    })
    if err != nil {
        // Update transaction status to failed
        s.DB.Model(&Transaction{}).Where("stripe_intent_id = ?", req.IntentId).
            Update("status", "failed")
        return nil, status.Error(codes.Internal, "payment confirmation failed: "+err.Error())
    }

    // Update transaction status
    status := "succeeded"
    if pi.Status == "failed" {
        status = "failed"
    }

    s.DB.Model(&Transaction{}).Where("stripe_intent_id = ?", req.IntentId).Updates(map[string]interface{}{
        "status":           status,
        "stripe_payment_id": pi.LatestChargeID(),
        "updated_at":       time.Now(),
    })

    return &pb.ConfirmPaymentResponse{
        Success:    pi.Status == "succeeded",
        PaymentId:  pi.LatestChargeID(),
        Status:     string(pi.Status),
    }, nil
}

// GetTransaction retrieves a transaction by service ID
func (s *PaymentServer) GetTransaction(ctx context.Context, req *pb.GetTransactionRequest) (*pb.TransactionResponse, error) {
    var transaction Transaction
    query := s.DB.Where("service_id = ? AND service_type = ?", req.ServiceId, req.ServiceType)
    
    if err := query.First(&transaction).Error; err != nil {
        return nil, status.Error(codes.NotFound, "transaction not found")
    }

    return &pb.TransactionResponse{
        Id:          transaction.ID,
        UserId:      transaction.UserID,
        ServiceType: transaction.ServiceType,
        ServiceId:   transaction.ServiceID,
        Amount:      transaction.Amount,
        Status:      transaction.Status,
        CreatedAt:   transaction.CreatedAt.String(),
    }, nil
}

// RefundPayment refunds a payment
func (s *PaymentServer) RefundPayment(ctx context.Context, req *pb.RefundPaymentRequest) (*pb.RefundResponse, error) {
    stripe.Key = s.StripeKey

    var transaction Transaction
    if err := s.DB.Where("id = ?", req.TransactionId).First(&transaction).Error; err != nil {
        return nil, status.Error(codes.NotFound, "transaction not found")
    }

    // In production: create Stripe refund
    // refundParams := &stripe.RefundParams{
    //     PaymentIntent: stripe.String(transaction.StripeIntentID),
    // }
    // refund, err := refund.New(refundParams)

    // Update transaction status
    s.DB.Model(&transaction).Update("status", "refunded")

    return &pb.RefundResponse{
        Success:     true,
        RefundId:    "ref_" + time.Now().Format("20060102150405"),
        Amount:      transaction.Amount,
    }, nil
}

// Webhook handler for Stripe events
func (s *PaymentServer) stripeWebhookHandler(w http.ResponseWriter, r *http.Request) {
    const MaxBodyBytes = int64(65536)
    r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
    payload, err := json.Marshal(r.Body)
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    // Verify webhook signature
    signature := r.Header.Get("Stripe-Signature")
    event, err := webhook.ConstructEvent(payload, signature, s.WebhookSecret)
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    // Handle event
    switch event.Type {
    case "payment_intent.succeeded":
        var pi stripe.PaymentIntent
        if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
            w.WriteHeader(http.StatusBadRequest)
            return
        }
        // Update transaction status
        s.DB.Model(&Transaction{}).Where("stripe_intent_id = ?", pi.ID).Updates(map[string]interface{}{
            "status":           "succeeded",
            "stripe_payment_id": pi.LatestChargeID(),
            "updated_at":       time.Now(),
        })
        log.Printf("✅ Payment succeeded for intent: %s", pi.ID)
    case "payment_intent.payment_failed":
        var pi stripe.PaymentIntent
        if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
            w.WriteHeader(http.StatusBadRequest)
            return
        }
        s.DB.Model(&Transaction{}).Where("stripe_intent_id = ?", pi.ID).Update("status", "failed")
        log.Printf("❌ Payment failed for intent: %s", pi.ID)
    }

    w.WriteHeader(http.StatusOK)
}

// Health check endpoint
func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
}

func generateTransactionID() string {
    return "txn_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=paymentdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }
    db.AutoMigrate(&Transaction{})

    // Stripe configuration
    stripeKey := os.Getenv("STRIPE_SECRET_KEY")
    webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
    if stripeKey == "" {
        log.Println("⚠️ Warning: STRIPE_SECRET_KEY not set, payments will fail")
    }

    // Create gRPC server
    grpcServer := grpc.NewServer()
    paymentServer := &PaymentServer{
        DB:            db,
        StripeKey:     stripeKey,
        WebhookSecret: webhookSecret,
    }
    pb.RegisterPaymentServiceServer(grpcServer, paymentServer)

    // HTTP server for webhooks
    http.HandleFunc("/webhook/stripe", paymentServer.stripeWebhookHandler)
    http.HandleFunc("/health", healthHandler)

    go func() {
        log.Println("✅ Payment Service HTTP (webhook) running on port 8082")
        log.Fatal(http.ListenAndServe(":8082", nil))
    }()

    // gRPC server
    lis, err := net.Listen("tcp", ":50054")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Payment Service gRPC running on port 50054")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Payment Service stopped")
}