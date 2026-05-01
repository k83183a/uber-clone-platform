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

// ============================================================
// MODELS
// ============================================================

type Transaction struct {
    ID               string    `gorm:"primaryKey"`
    UserID           string    `gorm:"index;not null"`
    DriverID         string    `gorm:"index"`
    ServiceType      string    `gorm:"index;not null"` // ride, food, grocery, courier
    ServiceID        string    `gorm:"index;not null"`
    Amount           float64   `gorm:"not null"`
    Currency         string    `gorm:"default:'GBP'"`
    Status           string    `gorm:"default:'pending'"`
    PaymentMethod    string
    StripeIntentID   string    `gorm:"index"`
    StripePaymentID  string
    PlatformFee      float64
    DriverPayout     float64
    OperatorID       string
    BusinessModel    string
    Metadata         string    `gorm:"type:jsonb"`
    CreatedAt        time.Time
    UpdatedAt        time.Time
    CompletedAt      *time.Time
    RefundedAt       *time.Time
}

type PaymentMethod struct {
    ID                   string    `gorm:"primaryKey"`
    UserID               string    `gorm:"index;not null"`
    MethodType           string    `gorm:"not null"` // card, apple_pay, google_pay, paypal
    StripePaymentMethodID string   `gorm:"uniqueIndex"`
    LastFour             string
    CardBrand            string
    ExpiryMonth          int
    ExpiryYear           int
    IsDefault            bool      `gorm:"default:false"`
    IsActive             bool      `gorm:"default:true"`
    CreatedAt            time.Time
    UpdatedAt            time.Time
}

type PaymentServer struct {
    pb.UnimplementedPaymentServiceServer
    DB            *gorm.DB
    StripeKey     string
    WebhookSecret string
}

// CreatePaymentIntent - Create Stripe PaymentIntent
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
            "operator_id":  req.OperatorId,
            "business_model": req.BusinessModel,
        },
    }

    // For agent model, split payment to driver
    if req.BusinessModel == "agent" && req.DriverId != "" && req.DriverStripeAccountId != "" {
        params.TransferData = &stripe.PaymentIntentTransferDataParams{
            Amount:      stripe.Int64(driverAmount),
            Destination: stripe.String(req.DriverStripeAccountId),
        }
        params.ApplicationFeeAmount = stripe.Int64(platformFee)
    } else if req.OperatorStripeAccountId != "" {
        // Principal model: full amount to operator
        params.TransferData = &stripe.PaymentIntentTransferDataParams{
            Destination: stripe.String(req.OperatorStripeAccountId),
        }
    }

    pi, err := paymentintent.New(params)
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to create payment intent: "+err.Error())
    }

    // Store transaction
    tx := &Transaction{
        ID:             generateID(),
        UserID:         req.UserId,
        DriverID:       req.DriverId,
        ServiceType:    req.ServiceType,
        ServiceID:      req.ServiceId,
        Amount:         float64(req.AmountCents) / 100,
        Currency:       req.Currency,
        Status:         "pending",
        StripeIntentID: pi.ID,
        PlatformFee:    float64(platformFee) / 100,
        DriverPayout:   float64(driverAmount) / 100,
        OperatorID:     req.OperatorId,
        BusinessModel:  req.BusinessModel,
        CreatedAt:      time.Now(),
        UpdatedAt:      time.Now(),
    }
    s.DB.Create(tx)

    return &pb.PaymentIntentResponse{
        ClientSecret: pi.ClientSecret,
        IntentId:     pi.ID,
    }, nil
}

// ConfirmPayment - Confirm a payment intent
func (s *PaymentServer) ConfirmPayment(ctx context.Context, req *pb.ConfirmPaymentRequest) (*pb.ConfirmPaymentResponse, error) {
    stripe.Key = s.StripeKey

    pi, err := paymentintent.Confirm(req.IntentId, &stripe.PaymentIntentConfirmParams{
        PaymentMethod: stripe.String(req.PaymentMethodId),
    })
    if err != nil {
        s.DB.Model(&Transaction{}).Where("stripe_intent_id = ?", req.IntentId).Update("status", "failed")
        return nil, status.Error(codes.Internal, "payment confirmation failed: "+err.Error())
    }

    status := "succeeded"
    if pi.Status == "failed" {
        status = "failed"
    }

    now := time.Now()
    updates := map[string]interface{}{
        "status":           status,
        "stripe_payment_id": pi.LatestChargeID(),
        "updated_at":       now,
    }
    if status == "succeeded" {
        updates["completed_at"] = now
    }
    s.DB.Model(&Transaction{}).Where("stripe_intent_id = ?", req.IntentId).Updates(updates)

    return &pb.ConfirmPaymentResponse{
        Success:   pi.Status == "succeeded",
        PaymentId: pi.LatestChargeID(),
        Status:    string(pi.Status),
    }, nil
}

// GetTransaction - Get transaction by service ID
func (s *PaymentServer) GetTransaction(ctx context.Context, req *pb.GetTransactionRequest) (*pb.TransactionResponse, error) {
    var tx Transaction
    if err := s.DB.Where("service_id = ? AND service_type = ?", req.ServiceId, req.ServiceType).First(&tx).Error; err != nil {
        return nil, status.Error(codes.NotFound, "transaction not found")
    }

    return &pb.TransactionResponse{
        Id:          tx.ID,
        UserId:      tx.UserID,
        ServiceType: tx.ServiceType,
        ServiceId:   tx.ServiceID,
        Amount:      tx.Amount,
        Status:      tx.Status,
        CreatedAt:   tx.CreatedAt.Unix(),
    }, nil
}

// RefundPayment - Refund a payment
func (s *PaymentServer) RefundPayment(ctx context.Context, req *pb.RefundPaymentRequest) (*pb.RefundResponse, error) {
    var tx Transaction
    if err := s.DB.Where("id = ?", req.TransactionId).First(&tx).Error; err != nil {
        return nil, status.Error(codes.NotFound, "transaction not found")
    }

    stripe.Key = s.StripeKey
    // In production: create Stripe refund
    // refundParams := &stripe.RefundParams{
    //     PaymentIntent: stripe.String(tx.StripeIntentID),
    // }

    tx.Status = "refunded"
    now := time.Now()
    tx.RefundedAt = &now
    s.DB.Save(&tx)

    return &pb.RefundResponse{
        Success:   true,
        RefundId:  "ref_" + time.Now().Format("20060102150405"),
        Amount:    tx.Amount,
    }, nil
}

// AddPaymentMethod - Save a payment method for a user
func (s *PaymentServer) AddPaymentMethod(ctx context.Context, req *pb.AddPaymentMethodRequest) (*pb.PaymentMethodResponse, error) {
    stripe.Key = s.StripeKey

    // Verify payment method exists
    pm, err := stripe.paymentmethod.Get(req.StripePaymentMethodId, nil)
    if err != nil {
        return nil, status.Error(codes.InvalidArgument, "invalid payment method")
    }

    pmRecord := &PaymentMethod{
        ID:                   generateID(),
        UserID:               req.UserId,
        MethodType:           req.MethodType,
        StripePaymentMethodID: req.StripePaymentMethodId,
        IsActive:             true,
        CreatedAt:            time.Now(),
        UpdatedAt:            time.Now(),
    }

    if pm.Card != nil {
        pmRecord.LastFour = pm.Card.Last4
        pmRecord.CardBrand = string(pm.Card.Brand)
        pmRecord.ExpiryMonth = int(pm.Card.ExpMonth)
        pmRecord.ExpiryYear = int(pm.Card.ExpYear)
    }

    if req.SetDefault {
        s.DB.Model(&PaymentMethod{}).Where("user_id = ?", req.UserId).Update("is_default", false)
        pmRecord.IsDefault = true
    }

    if err := s.DB.Create(pmRecord).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to save payment method")
    }

    return &pb.PaymentMethodResponse{
        Id:         pmRecord.ID,
        MethodType: pmRecord.MethodType,
        LastFour:   pmRecord.LastFour,
        CardBrand:  pmRecord.CardBrand,
        IsDefault:  pmRecord.IsDefault,
    }, nil
}

// ListPaymentMethods - List user's payment methods
func (s *PaymentServer) ListPaymentMethods(ctx context.Context, req *pb.ListPaymentMethodsRequest) (*pb.ListPaymentMethodsResponse, error) {
    var methods []PaymentMethod
    if err := s.DB.Where("user_id = ? AND is_active = ?", req.UserId, true).Find(&methods).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list payment methods")
    }

    var pbMethods []*pb.PaymentMethodResponse
    for _, m := range methods {
        pbMethods = append(pbMethods, &pb.PaymentMethodResponse{
            Id:         m.ID,
            MethodType: m.MethodType,
            LastFour:   m.LastFour,
            CardBrand:  m.CardBrand,
            IsDefault:  m.IsDefault,
        })
    }

    return &pb.ListPaymentMethodsResponse{Methods: pbMethods}, nil
}

// SetDefaultPaymentMethod - Set a payment method as default
func (s *PaymentServer) SetDefaultPaymentMethod(ctx context.Context, req *pb.SetDefaultPaymentMethodRequest) (*pb.Empty, error) {
    s.DB.Model(&PaymentMethod{}).Where("user_id = ?", req.UserId).Update("is_default", false)
    s.DB.Model(&PaymentMethod{}).Where("id = ?", req.PaymentMethodId).Update("is_default", true)
    return &pb.Empty{}, nil
}

// DeletePaymentMethod - Delete a payment method
func (s *PaymentServer) DeletePaymentMethod(ctx context.Context, req *pb.DeletePaymentMethodRequest) (*pb.Empty, error) {
    s.DB.Model(&PaymentMethod{}).Where("id = ?", req.PaymentMethodId).Update("is_active", false)
    return &pb.Empty{}, nil
}

// Webhook handler for Stripe events
func (s *PaymentServer) stripeWebhookHandler(w http.ResponseWriter, r *http.Request) {
    const MaxBodyBytes = int64(65536)
    r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
    payload, err := io.ReadAll(r.Body)
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    signature := r.Header.Get("Stripe-Signature")
    event, err := webhook.ConstructEvent(payload, signature, s.WebhookSecret)
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    switch event.Type {
    case "payment_intent.succeeded":
        var pi stripe.PaymentIntent
        json.Unmarshal(event.Data.Raw, &pi)
        s.DB.Model(&Transaction{}).Where("stripe_intent_id = ?", pi.ID).Updates(map[string]interface{}{
            "status":          "succeeded",
            "stripe_payment_id": pi.LatestChargeID(),
            "completed_at":    time.Now(),
        })
        log.Printf("✅ Payment succeeded: %s", pi.ID)

    case "payment_intent.payment_failed":
        var pi stripe.PaymentIntent
        json.Unmarshal(event.Data.Raw, &pi)
        s.DB.Model(&Transaction{}).Where("stripe_intent_id = ?", pi.ID).Update("status", "failed")
        log.Printf("❌ Payment failed: %s", pi.ID)
    }

    w.WriteHeader(http.StatusOK)
}

func generateID() string {
    return "pay_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=paymentdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }
    db.AutoMigrate(&Transaction{}, &PaymentMethod{})

    stripeKey := os.Getenv("STRIPE_SECRET_KEY")
    webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
    if stripeKey == "" {
        log.Println("⚠️ Warning: STRIPE_SECRET_KEY not set")
    }

    server := &PaymentServer{
        DB:            db,
        StripeKey:     stripeKey,
        WebhookSecret: webhookSecret,
    }

    // HTTP webhook endpoint
    http.HandleFunc("/webhook/stripe", server.stripeWebhookHandler)
    go func() {
        log.Println("✅ Payment webhook listening on :8082")
        log.Fatal(http.ListenAndServe(":8082", nil))
    }()

    grpcServer := grpc.NewServer()
    pb.RegisterPaymentServiceServer(grpcServer, server)

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
}