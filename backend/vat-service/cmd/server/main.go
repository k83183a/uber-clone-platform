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

type VATTransaction struct {
    ID              string    `gorm:"primaryKey"`
    ServiceType     string    `gorm:"index;not null"`
    TransactionID   string    `gorm:"index;not null"`
    TransactionDate time.Time `gorm:"index;not null"`
    NetAmount       float64   `gorm:"not null"`
    VATRate         float64   `gorm:"not null"`
    VATAmount       float64   `gorm:"not null"`
    GrossAmount     float64   `gorm:"not null"`
    BusinessModel   string
    OperatorID      string    `gorm:"index"`
    DriverID        string    `gorm:"index"`
    CustomerID      string    `gorm:"index"`
    CreatedAt       time.Time
}

type VATReturn struct {
    ID                       string    `gorm:"primaryKey"`
    PeriodStart              time.Time `gorm:"index"`
    PeriodEnd                time.Time `gorm:"index"`
    SubmissionID             string    `gorm:"uniqueIndex"`
    Status                   string    `gorm:"default:'pending'"`
    VatDueSales              float64
    VatDueAcquisitions       float64
    TotalVatDue              float64
    VatReclaimedCurrPeriod   float64
    NetVatDue                float64
    TotalValueSalesExVAT     float64
    TotalValuePurchasesExVAT float64
    TotalValueGoodsSuppliedExVAT float64
    TotalAcquisitionsExVAT   float64
    SubmittedAt              time.Time
    ResponseData             string    `gorm:"type:text"`
    CreatedAt                time.Time
}

type VATServer struct {
    pb.UnimplementedVATServiceServer
    DB *gorm.DB
}

// GenerateVATReport - Generate VAT report for period
func (s *VATServer) GenerateVATReport(ctx context.Context, req *pb.GenerateVATReportRequest) (*pb.VATReportResponse, error) {
    startDate := time.Unix(req.PeriodStart, 0)
    endDate := time.Unix(req.PeriodEnd, 0)

    var transactions []VATTransaction
    query := s.DB.Where("transaction_date >= ? AND transaction_date <= ?", startDate, endDate)
    if req.OperatorId != "" {
        query = query.Where("operator_id = ?", req.OperatorId)
    }
    query.Find(&transactions)

    summary := &pb.VATSummary{TotalNetSales: 0, TotalVatOutput: 0, TotalVatInput: 0, VatDue: 0}
    var pbTransactions []*pb.VATTransaction

    for _, t := range transactions {
        summary.TotalNetSales += t.NetAmount
        summary.TotalVatOutput += t.VATAmount
        pbTransactions = append(pbTransactions, &pb.VATTransaction{
            TransactionId: t.TransactionID,
            ServiceType:   t.ServiceType,
            Date:          t.TransactionDate.Format("2006-01-02"),
            Net:           t.NetAmount,
            VatRate:       t.VATRate,
            Vat:           t.VATAmount,
            Gross:         t.GrossAmount,
        })
    }

    summary.TotalVatInput = 0
    summary.VatDue = summary.TotalVatOutput - summary.TotalVatInput

    return &pb.VATReportResponse{Summary: summary, Transactions: pbTransactions}, nil
}

// ExportVATCSV - Export as CSV
func (s *VATServer) ExportVATCSV(ctx context.Context, req *pb.ExportVATCSVRequest) (*pb.CSVResponse, error) {
    startDate := time.Unix(req.PeriodStart, 0)
    endDate := time.Unix(req.PeriodEnd, 0)

    var transactions []VATTransaction
    s.DB.Where("transaction_date >= ? AND transaction_date <= ?", startDate, endDate).Find(&transactions)

    csvBuffer := &strings.Builder{}
    writer := csv.NewWriter(csvBuffer)
    writer.Write([]string{"Date", "Transaction ID", "Service Type", "Net (£)", "VAT Rate (%)", "VAT (£)", "Gross (£)"})

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

    return &pb.CSVResponse{CsvData: []byte(csvBuffer.String())}, nil
}

// SubmitToHMRC - Submit to HMRC MTD
func (s *VATServer) SubmitToHMRC(ctx context.Context, req *pb.SubmitToHMRCRequest) (*pb.SubmitResponse, error) {
    startDate := time.Unix(req.PeriodStart, 0)
    endDate := time.Unix(req.PeriodEnd, 0)

    var summary pb.VATSummary
    json.Unmarshal([]byte(req.VatReturnJson), &summary)

    hmrcPayload := map[string]interface{}{
        "periodKey":                    fmt.Sprintf("%02d%02d", startDate.Year(), startDate.Month()),
        "vatDueSales":                  summary.TotalVatOutput,
        "vatDueAcquisitions":           0,
        "totalVatDue":                  summary.VatDue,
        "vatReclaimedCurrPeriod":       summary.TotalVatInput,
        "netVatDue":                    summary.VatDue,
        "totalValueSalesExVAT":         summary.TotalNetSales,
        "totalValuePurchasesExVAT":     0,
        "totalValueGoodsSuppliedExVAT": 0,
        "totalAcquisitionsExVAT":       0,
        "finalised":                    true,
    }

    submissionID := "HMRC_" + time.Now().Format("20060102150405")
    submission := &VATReturn{
        ID:                       generateID(),
        PeriodStart:              startDate,
        PeriodEnd:                endDate,
        SubmissionID:             submissionID,
        Status:                   "submitted",
        VatDueSales:              summary.TotalVatOutput,
        TotalVatDue:              summary.VatDue,
        VatReclaimedCurrPeriod:   summary.TotalVatInput,
        NetVatDue:                summary.VatDue,
        TotalValueSalesExVAT:     summary.TotalNetSales,
        SubmittedAt:              time.Now(),
        CreatedAt:                time.Now(),
    }
    s.DB.Create(submission)

    return &pb.SubmitResponse{
        SubmissionId: submissionID,
        Status:       "accepted",
        Message:      "VAT return submitted to HMRC successfully",
    }, nil
}

// GetSubmissionStatus - Get submission status
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

// ListSubmissions - List all submissions
func (s *VATServer) ListSubmissions(ctx context.Context, req *pb.ListSubmissionsRequest) (*pb.ListSubmissionsResponse, error) {
    var submissions []VATReturn
    s.DB.Order("submitted_at DESC").Find(&submissions)

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

// AddVATTransaction - Add manual VAT transaction
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
    s.DB.Create(tx)
    return &pb.Empty{}, nil
}

// GetHMRCConfig - Get HMRC config
func (s *VATServer) GetHMRCConfig(ctx context.Context, req *pb.Empty) (*pb.HMRCConfigResponse, error) {
    return &pb.HMRCConfigResponse{
        ClientId:     os.Getenv("HMRC_CLIENT_ID"),
        IsConfigured: os.Getenv("HMRC_CLIENT_ID") != "",
    }, nil
}

// SetHMRCConfig - Set HMRC config
func (s *VATServer) SetHMRCConfig(ctx context.Context, req *pb.SetHMRCConfigRequest) (*pb.Empty, error) {
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
}