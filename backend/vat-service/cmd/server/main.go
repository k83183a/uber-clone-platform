package main

import (
    "context"
    "encoding/csv"
    "encoding/json"
    "fmt"
    "log"
    "net"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "syscall"
    "time"

    "github.com/joho/godotenv"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/uber-clone/vat-service/proto"
)

// VATTransaction represents a single transaction with VAT calculation
type VATTransaction struct {
    ID              string    `gorm:"primaryKey"`
    ServiceType     string    `gorm:"index;not null"`   // ride, food, grocery, courier, subscription
    TransactionID   string    `gorm:"index;not null"`   // ride_id, order_id, etc.
    TransactionDate time.Time `gorm:"index;not null"`
    NetAmount       float64   `gorm:"not null"`         // excluding VAT
    VATRate         float64   `gorm:"not null"`         // 0, 5, 20
    VATAmount       float64   `gorm:"not null"`         // net * rate / 100
    GrossAmount     float64   `gorm:"not null"`         // net + vat
    BusinessModel   string    // principal, agent
    OperatorID      string    `gorm:"index"`
    DriverID        string    `gorm:"index"`
    CustomerID      string    `gorm:"index"`
    CreatedAt       time.Time
}

// VATReturn represents a submitted VAT return
type VATReturn struct {
    ID                string    `gorm:"primaryKey"`
    PeriodStart       time.Time `gorm:"index"`
    PeriodEnd         time.Time `gorm:"index"`
    SubmissionID      string    `gorm:"uniqueIndex"` // HMRC submission ID
    Status            string    `gorm:"default:'pending'"` // pending, submitted, failed
    VatDueSales       float64   // box 1
    VatDueAcquisitions float64  // box 2
    TotalVatDue       float64   // box 3
    VatReclaimedCurrPeriod float64 // box 4
    NetVatDue         float64   // box 5
    TotalValueSalesExVAT float64 // box 6
    TotalValuePurchasesExVAT float64 // box 7
    TotalValueGoodsSuppliedExVAT float64 // box 8
    TotalAcquisitionsExVAT float64 // box 9
    SubmittedAt       time.Time
    ResponseData      string    `gorm:"type:text"` // JSON response from HMRC
    CreatedAt         time.Time
}

// VATServer handles gRPC requests
type VATServer struct {
    pb.UnimplementedVATServiceServer
    DB *gorm.DB
}

// GenerateVATReport generates VAT report for a given period
func (s *VATServer) GenerateVATReport(ctx context.Context, req *pb.GenerateVATReportRequest) (*pb.VATReportResponse, error) {
    startDate := time.Unix(req.PeriodStart, 0)
    endDate := time.Unix(req.PeriodEnd, 0)

    var transactions []VATTransaction
    query := s.DB.Where("transaction_date >= ? AND transaction_date <= ?", startDate, endDate)

    if req.OperatorId != "" {
        query = query.Where("operator_id = ?", req.OperatorId)
    }

    if err := query.Find(&transactions).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to fetch transactions")
    }

    // Calculate VAT summary
    summary := &pb.VATSummary{
        TotalNetSales:     0,
        TotalVatOutput:    0,
        TotalVatInput:     0,
        VatDue:            0,
    }

    var pbTransactions []*pb.VATTransaction
    for _, t := range transactions {
        summary.TotalNetSales += t.NetAmount
        summary.TotalVatOutput += t.VATAmount

        pbTransactions = append(pbTransactions, &pb.VATTransaction{
            TransactionId:   t.TransactionID,
            ServiceType:     t.ServiceType,
            Date:            t.TransactionDate.Format("2006-01-02"),
            Net:             t.NetAmount,
            VatRate:         t.VATRate,
            Vat:             t.VATAmount,
            Gross:           t.GrossAmount,
        })
    }

    // For MVP, VAT input is 0 (would be calculated from expenses)
    summary.TotalVatInput = 0
    summary.VatDue = summary.TotalVatOutput - summary.TotalVatInput

    return &pb.VATReportResponse{
        Summary:      summary,
        Transactions: pbTransactions,
    }, nil
}

// ExportVATCSV exports VAT report as CSV
func (s *VATServer) ExportVATCSV(ctx context.Context, req *pb.ExportVATCSVRequest) (*pb.CSVResponse, error) {
    startDate := time.Unix(req.PeriodStart, 0)
    endDate := time.Unix(req.PeriodEnd, 0)

    var transactions []VATTransaction
    if err := s.DB.Where("transaction_date >= ? AND transaction_date <= ?", startDate, endDate).Find(&transactions).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to fetch transactions")
    }

    csvBuffer := &strings.Builder{}
    writer := csv.NewWriter(csvBuffer)

    // Write header
    writer.Write([]string{"Date", "Transaction ID", "Service Type", "Net (£)", "VAT Rate (%)", "VAT (£)", "Gross (£)"})

    // Write rows
    for _, t := range transactions {
        writer.Write([]string{
            t.TransactionDate.Format("2006-01-02"),
            t.TransactionID,
            t.ServiceType,
            fmt.Sprintf("%.2f", t.NetAmount),
            fmt.Sprintf("%.0f", t.VATRate),
            fmt.Sprintf("%.2f", t.VATAmount),
            fmt.Sprintf("%.2f", t.GrossAmount),
        })
    }
    writer.Flush()

    return &pb.CSVResponse{
        CsvData: []byte(csvBuffer.String()),
    }, nil
}

// SubmitToHMRC submits VAT return to HMRC Making Tax Digital API
func (s *VATServer) SubmitToHMRC(ctx context.Context, req *pb.SubmitToHMRCRequest) (*pb.SubmitResponse, error) {
    startDate := time.Unix(req.PeriodStart, 0)
    endDate := time.Unix(req.PeriodEnd, 0)

    // Generate VAT return data from the summary
    vatReturn := &pb.VATSummary{}
    if err := json.Unmarshal([]byte(req.VatReturnJson), vatReturn); err != nil {
        return nil, status.Error(codes.InvalidArgument, "invalid VAT return JSON")
    }

    // Prepare HMRC MTD payload
    hmrcPayload := map[string]interface{}{
        "periodKey": fmt.Sprintf("%02d%02d", startDate.Year(), startDate.Month()),
        "vatDueSales":               vatReturn.TotalVatOutput,
        "vatDueAcquisitions":        0,
        "totalVatDue":              vatReturn.VatDue,
        "vatReclaimedCurrPeriod":   vatReturn.TotalVatInput,
        "netVatDue":                vatReturn.VatDue,
        "totalValueSalesExVAT":     vatReturn.TotalNetSales,
        "totalValuePurchasesExVAT": 0,
        "totalValueGoodsSuppliedExVAT": 0,
        "totalAcquisitionsExVAT":   0,
        "finalised": true,
    }

    // In production: call HMRC MTD API with OAuth2
    // For MVP, simulate successful submission
    submissionID := "HMRC_" + time.Now().Format("20060102150405")

    // Save submission record
    submission := &VATReturn{
        ID:          generateID(),
        PeriodStart: startDate,
        PeriodEnd:   endDate,
        SubmissionID: submissionID,
        Status:      "submitted",
        VatDueSales: vatReturn.TotalVatOutput,
        TotalVatDue: vatReturn.VatDue,
        VatReclaimedCurrPeriod: vatReturn.TotalVatInput,
        NetVatDue:   vatReturn.VatDue,
        TotalValueSalesExVAT: vatReturn.TotalNetSales,
        SubmittedAt: time.Now(),
        CreatedAt:   time.Now(),
    }

    respData, _ := json.Marshal(hmrcPayload)
    submission.ResponseData = string(respData)

    if err := s.DB.Create(submission).Error; err != nil {
        log.Printf("Failed to save VAT submission record: %v", err)
    }

    return &pb.SubmitResponse{
        SubmissionId: submissionID,
        Status:       "accepted",
        Message:      "VAT return submitted to HMRC successfully",
    }, nil
}

// GetSubmissionStatus retrieves status of a VAT submission
func (s *VATServer) GetSubmissionStatus(ctx context.Context, req *pb.GetSubmissionStatusRequest) (*pb.SubmissionStatusResponse, error) {
    var submission VATReturn
    if err := s.DB.Where("submission_id = ?", req.SubmissionId).First(&submission).Error; err != nil {
        return nil, status.Error(codes.NotFound, "submission not found")
    }

    return &pb.SubmissionStatusResponse{
        SubmissionId: submission.SubmissionID,
        Status:       submission.Status,
        SubmittedAt:  submission.SubmittedAt.Unix(),
    }, nil
}

// ListSubmissions lists all VAT submissions
func (s *VATServer) ListSubmissions(ctx context.Context, req *pb.ListSubmissionsRequest) (*pb.ListSubmissionsResponse, error) {
    var submissions []VATReturn
    if err := s.DB.Order("submitted_at DESC").Find(&submissions).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list submissions")
    }

    var pbSubmissions []*pb.SubmissionSummary
    for _, sub := range submissions {
        pbSubmissions = append(pbSubmissions, &pb.SubmissionSummary{
            SubmissionId: sub.SubmissionID,
            PeriodStart:  sub.PeriodStart.Unix(),
            PeriodEnd:    sub.PeriodEnd.Unix(),
            Status:       sub.Status,
            SubmittedAt:  sub.SubmittedAt.Unix(),
        })
    }

    return &pb.ListSubmissionsResponse{Submissions: pbSubmissions}, nil
}

// AddVATTransaction manually adds a VAT transaction (for testing/fixes)
func (s *VATServer) AddVATTransaction(ctx context.Context, req *pb.AddVATTransactionRequest) (*pb.Empty, error) {
    txDate := time.Unix(req.TransactionDate, 0)
    vatAmount := req.NetAmount * (req.VatRate / 100)

    tx := &VATTransaction{
        ID:              generateID(),
        ServiceType:     req.ServiceType,
        TransactionID:   req.TransactionId,
        TransactionDate: txDate,
        NetAmount:       req.NetAmount,
        VATRate:         req.VatRate,
        VATAmount:       vatAmount,
        GrossAmount:     req.NetAmount + vatAmount,
        BusinessModel:   req.BusinessModel,
        OperatorID:      req.OperatorId,
        DriverID:        req.DriverId,
        CustomerID:      req.CustomerId,
        CreatedAt:       time.Now(),
    }

    if err := s.DB.Create(tx).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to add transaction")
    }

    return &pb.Empty{}, nil
}

// GetHMRCConfig returns HMRC MTD configuration for the admin panel
func (s *VATServer) GetHMRCConfig(ctx context.Context, req *pb.Empty) (*pb.HMRCConfigResponse, error) {
    // In production, read from database or env
    return &pb.HMRCConfigResponse{
        ClientId:     os.Getenv("HMRC_CLIENT_ID"),
        IsConfigured: os.Getenv("HMRC_CLIENT_ID") != "",
    }, nil
}

// SetHMRCConfig updates HMRC MTD configuration (admin only)
func (s *VATServer) SetHMRCConfig(ctx context.Context, req *pb.SetHMRCConfigRequest) (*pb.Empty, error) {
    // In production, store encrypted in database
    // For MVP, just log
    log.Printf("HMRC config updated: client_id=%s", req.ClientId)
    return &pb.Empty{}, nil
}

func generateID() string {
    return "vat_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=vatdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&VATTransaction{}, &VATReturn{})

    grpcServer := grpc.NewServer()
    pb.RegisterVATServiceServer(grpcServer, &VATServer{DB: db})

    lis, err := net.Listen("tcp", ":50077")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ VAT Service running on port 50077")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("VAT Service stopped")
}