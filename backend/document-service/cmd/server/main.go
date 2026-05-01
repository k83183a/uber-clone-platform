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

    pb "github.com/uber-clone/document-service/proto"
)

// Document represents an uploaded document
type Document struct {
    ID              string     `gorm:"primaryKey"`
    UserID          string     `gorm:"index;not null"`
    UserType        string     `gorm:"not null"` // driver, store, rider
    DocumentType    string     `gorm:"not null"` // license_front, license_back, phv_license, proof_address, profile_photo, passport, insurance, v5c, mot
    FileName        string     `gorm:"not null"`
    FileURL         string     `gorm:"not null"`
    FileSize        int64
    MimeType        string
    OCRData         string     `gorm:"type:text"` // JSON extracted data
    Status          string     `gorm:"default:'pending'"` // pending, verified, rejected, expired
    RejectionReason string
    VerifiedBy      string
    VerifiedAt      *time.Time
    ExpiresAt       *time.Time
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

// DocumentVerificationLog tracks verification attempts
type DocumentVerificationLog struct {
    ID          string    `gorm:"primaryKey"`
    DocumentID  string    `gorm:"index;not null"`
    Status      string    // verified, rejected
    Reason      string
    PerformedBy string
    CreatedAt   time.Time
}

// DocumentServer handles gRPC requests
type DocumentServer struct {
    pb.UnimplementedDocumentServiceServer
    DB *gorm.DB
}

// UploadDocument uploads a document (file URL from client)
func (s *DocumentServer) UploadDocument(ctx context.Context, req *pb.UploadDocumentRequest) (*pb.DocumentResponse, error) {
    // Validate file size (max 10MB)
    if req.FileSize > 10*1024*1024 {
        return nil, status.Error(codes.InvalidArgument, "file size exceeds 10MB limit")
    }

    // Validate document type
    validTypes := map[string]bool{
        "license_front": true, "license_back": true, "phv_license": true,
        "proof_address": true, "profile_photo": true, "passport": true,
        "insurance": true, "v5c": true, "mot": true,
    }
    if !validTypes[req.DocumentType] {
        return nil, status.Error(codes.InvalidArgument, "invalid document type")
    }

    // Set expiry date for certain document types
    var expiresAt *time.Time
    if req.DocumentType == "phv_license" || req.DocumentType == "insurance" {
        // PHV license and insurance typically expire after 1 year
        oneYear := time.Now().AddDate(1, 0, 0)
        expiresAt = &oneYear
    }

    doc := &Document{
        ID:           generateID(),
        UserID:       req.UserId,
        UserType:     req.UserType,
        DocumentType: req.DocumentType,
        FileName:     req.FileName,
        FileURL:      req.FileUrl,
        FileSize:     req.FileSize,
        MimeType:     req.MimeType,
        Status:       "pending",
        ExpiresAt:    expiresAt,
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
    }

    if err := s.DB.Create(doc).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to upload document")
    }

    // In production: trigger OCR processing asynchronously
    // s.processOCR(doc.ID, req.FileUrl)

    return &pb.DocumentResponse{
        Id:           doc.ID,
        UserId:       doc.UserID,
        DocumentType: doc.DocumentType,
        FileUrl:      doc.FileURL,
        Status:       doc.Status,
        CreatedAt:    doc.CreatedAt.Unix(),
        ExpiresAt:    doc.ExpiresAt.Unix(),
    }, nil
}

// GetDocument returns document details
func (s *DocumentServer) GetDocument(ctx context.Context, req *pb.GetDocumentRequest) (*pb.DocumentResponse, error) {
    var doc Document
    if err := s.DB.Where("id = ?", req.DocumentId).First(&doc).Error; err != nil {
        return nil, status.Error(codes.NotFound, "document not found")
    }

    return &pb.DocumentResponse{
        Id:           doc.ID,
        UserId:       doc.UserID,
        UserType:     doc.UserType,
        DocumentType: doc.DocumentType,
        FileName:     doc.FileName,
        FileUrl:      doc.FileURL,
        Status:       doc.Status,
        RejectionReason: doc.RejectionReason,
        VerifiedAt:   doc.VerifiedAt.Unix(),
        ExpiresAt:    doc.ExpiresAt.Unix(),
        CreatedAt:    doc.CreatedAt.Unix(),
    }, nil
}

// ListUserDocuments lists all documents for a user
func (s *DocumentServer) ListUserDocuments(ctx context.Context, req *pb.ListUserDocumentsRequest) (*pb.ListDocumentsResponse, error) {
    var docs []Document
    query := s.DB.Where("user_id = ? AND user_type = ?", req.UserId, req.UserType)

    if req.DocumentType != "" {
        query = query.Where("document_type = ?", req.DocumentType)
    }

    if err := query.Find(&docs).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list documents")
    }

    var pbDocs []*pb.DocumentResponse
    for _, d := range docs {
        pbDocs = append(pbDocs, &pb.DocumentResponse{
            Id:           d.ID,
            UserId:       d.UserID,
            DocumentType: d.DocumentType,
            FileName:     d.FileName,
            FileUrl:      d.FileURL,
            Status:       d.Status,
            CreatedAt:    d.CreatedAt.Unix(),
            ExpiresAt:    d.ExpiresAt.Unix(),
        })
    }

    return &pb.ListDocumentsResponse{Documents: pbDocs}, nil
}

// VerifyDocument verifies a document (admin endpoint)
func (s *DocumentServer) VerifyDocument(ctx context.Context, req *pb.VerifyDocumentRequest) (*pb.DocumentResponse, error) {
    var doc Document
    if err := s.DB.Where("id = ?", req.DocumentId).First(&doc).Error; err != nil {
        return nil, status.Error(codes.NotFound, "document not found")
    }

    now := time.Now()
    doc.Status = req.Status
    doc.VerifiedBy = req.VerifiedBy
    doc.VerifiedAt = &now

    if req.Status == "rejected" {
        doc.RejectionReason = req.RejectionReason
    }

    if err := s.DB.Save(&doc).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to verify document")
    }

    // Log verification
    log := &DocumentVerificationLog{
        ID:          generateID(),
        DocumentID:  doc.ID,
        Status:      req.Status,
        Reason:      req.RejectionReason,
        PerformedBy: req.VerifiedBy,
        CreatedAt:   time.Now(),
    }
    s.DB.Create(log)

    return s.GetDocument(ctx, &pb.GetDocumentRequest{DocumentId: doc.ID})
}

// GetExpiringDocuments returns documents that expire soon
func (s *DocumentServer) GetExpiringDocuments(ctx context.Context, req *pb.GetExpiringDocumentsRequest) (*pb.ListDocumentsResponse, error) {
    threshold := time.Now().AddDate(0, 0, int(req.DaysThreshold))
    var docs []Document

    query := s.DB.Where("expires_at IS NOT NULL AND expires_at <= ?", threshold)
    if req.UserType != "" {
        query = query.Where("user_type = ?", req.UserType)
    }

    if err := query.Find(&docs).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get expiring documents")
    }

    var pbDocs []*pb.DocumentResponse
    for _, d := range docs {
        pbDocs = append(pbDocs, &pb.DocumentResponse{
            Id:           d.ID,
            UserId:       d.UserID,
            DocumentType: d.DocumentType,
            Status:       d.Status,
            ExpiresAt:    d.ExpiresAt.Unix(),
        })
    }

    return &pb.ListDocumentsResponse{Documents: pbDocs}, nil
}

// GetOCRData returns OCR extracted data from a document
func (s *DocumentServer) GetOCRData(ctx context.Context, req *pb.GetOCRDataRequest) (*pb.OCRDataResponse, error) {
    var doc Document
    if err := s.DB.Where("id = ?", req.DocumentId).First(&doc).Error; err != nil {
        return nil, status.Error(codes.NotFound, "document not found")
    }

    return &pb.OCRDataResponse{
        Data: doc.OCRData,
    }, nil
}

// DeleteDocument deletes a document (soft delete for GDPR)
func (s *DocumentServer) DeleteDocument(ctx context.Context, req *pb.DeleteDocumentRequest) (*pb.Empty, error) {
    if err := s.DB.Where("id = ?", req.DocumentId).Delete(&Document{}).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to delete document")
    }
    return &pb.Empty{}, nil
}

func generateID() string {
    return "doc_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=documentdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Document{}, &DocumentVerificationLog{})

    grpcServer := grpc.NewServer()
    pb.RegisterDocumentServiceServer(grpcServer, &DocumentServer{DB: db})

    lis, err := net.Listen("tcp", ":50074")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Document Service running on port 50074")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Document Service stopped")
}