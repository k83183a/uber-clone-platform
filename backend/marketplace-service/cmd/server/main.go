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

    pb "github.com/uber-clone/marketplace-service/proto"
)

// Listing represents an item for sale or rent
type Listing struct {
    ID          string     `gorm:"primaryKey"`
    SellerID    string     `gorm:"index;not null"`
    Title       string     `gorm:"not null"`
    Description string
    Category    string     `gorm:"index"`
    Price       float64    `gorm:"not null"`
    PriceType   string     `gorm:"default:'fixed'"` // fixed, negotiable, auction
    ListingType string     `gorm:"not null"`        // for_sale, for_rent, auction
    Condition   string     `gorm:"default:'used'"`  // new, like_new, used, refurbished
    Images      string     `gorm:"type:text"`       // JSON array of image URLs
    Location    string     // city/area
    Latitude    float64
    Longitude   float64
    Status      string     `gorm:"default:'active'"` // active, sold, rented, cancelled, expired
    ViewCount   int        `gorm:"default:0"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
    ExpiresAt   *time.Time
}

// Transaction represents a completed sale or rental
type MarketplaceTransaction struct {
    ID          string    `gorm:"primaryKey"`
    ListingID   string    `gorm:"index;not null"`
    BuyerID     string    `gorm:"index;not null"`
    SellerID    string    `gorm:"index;not null"`
    Amount      float64   `gorm:"not null"`
    Status      string    `gorm:"default:'pending'"` // pending, completed, refunded
    PaymentID   string
    RentalStart *time.Time
    RentalEnd   *time.Time
    CreatedAt   time.Time
    CompletedAt *time.Time
}

// MarketplaceServer handles gRPC requests
type MarketplaceServer struct {
    pb.UnimplementedMarketplaceServiceServer
    DB *gorm.DB
}

// CreateListing creates a new listing
func (s *MarketplaceServer) CreateListing(ctx context.Context, req *pb.CreateListingRequest) (*pb.ListingResponse, error) {
    imagesJSON := "[]"
    if len(req.Images) > 0 {
        // In production: marshal to JSON
    }

    expiresAt := time.Now().AddDate(0, 1, 0) // 1 month expiry by default

    listing := &Listing{
        ID:          generateID(),
        SellerID:    req.SellerId,
        Title:       req.Title,
        Description: req.Description,
        Category:    req.Category,
        Price:       req.Price,
        PriceType:   req.PriceType,
        ListingType: req.ListingType,
        Condition:   req.Condition,
        Images:      imagesJSON,
        Location:    req.Location,
        Latitude:    req.Latitude,
        Longitude:   req.Longitude,
        Status:      "active",
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
        ExpiresAt:   &expiresAt,
    }

    if err := s.DB.Create(listing).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create listing")
    }

    return &pb.ListingResponse{
        Id:          listing.ID,
        SellerId:    listing.SellerID,
        Title:       listing.Title,
        Description: listing.Description,
        Category:    listing.Category,
        Price:       listing.Price,
        PriceType:   listing.PriceType,
        ListingType: listing.ListingType,
        Condition:   listing.Condition,
        Status:      listing.Status,
        CreatedAt:   listing.CreatedAt.Unix(),
    }, nil
}

// GetListing returns listing details
func (s *MarketplaceServer) GetListing(ctx context.Context, req *pb.GetListingRequest) (*pb.ListingDetailResponse, error) {
    var listing Listing
    if err := s.DB.Where("id = ?", req.ListingId).First(&listing).Error; err != nil {
        return nil, status.Error(codes.NotFound, "listing not found")
    }

    // Increment view count
    s.DB.Model(&listing).Update("view_count", gorm.Expr("view_count + 1"))

    return &pb.ListingDetailResponse{
        Id:          listing.ID,
        SellerId:    listing.SellerID,
        Title:       listing.Title,
        Description: listing.Description,
        Category:    listing.Category,
        Price:       listing.Price,
        PriceType:   listing.PriceType,
        ListingType: listing.ListingType,
        Condition:   listing.Condition,
        Images:      []string{}, // Parse from JSON
        Location:    listing.Location,
        Latitude:    listing.Latitude,
        Longitude:   listing.Longitude,
        Status:      listing.Status,
        ViewCount:   int32(listing.ViewCount),
        CreatedAt:   listing.CreatedAt.Unix(),
    }, nil
}

// ListListings lists listings with filters
func (s *MarketplaceServer) ListListings(ctx context.Context, req *pb.ListListingsRequest) (*pb.ListListingsResponse, error) {
    var listings []Listing
    query := s.DB.Where("status = ?", "active")

    if req.Category != "" {
        query = query.Where("category = ?", req.Category)
    }
    if req.ListingType != "" {
        query = query.Where("listing_type = ?", req.ListingType)
    }
    if req.MinPrice > 0 {
        query = query.Where("price >= ?", req.MinPrice)
    }
    if req.MaxPrice > 0 {
        query = query.Where("price <= ?", req.MaxPrice)
    }
    if req.Query != "" {
        query = query.Where("title ILIKE ? OR description ILIKE ?", "%"+req.Query+"%", "%"+req.Query+"%")
    }

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Order("created_at DESC").Find(&listings).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list listings")
    }

    var total int64
    s.DB.Model(&Listing{}).Where("status = ?", "active").Count(&total)

    var pbListings []*pb.ListingResponse
    for _, l := range listings {
        pbListings = append(pbListings, &pb.ListingResponse{
            Id:          l.ID,
            SellerId:    l.SellerID,
            Title:       l.Title,
            Price:       l.Price,
            ListingType: l.ListingType,
            Status:      l.Status,
            CreatedAt:   l.CreatedAt.Unix(),
        })
    }

    return &pb.ListListingsResponse{Listings: pbListings, Total: int32(total)}, nil
}

// ListUserListings lists listings created by a user
func (s *MarketplaceServer) ListUserListings(ctx context.Context, req *pb.ListUserListingsRequest) (*pb.ListListingsResponse, error) {
    var listings []Listing
    query := s.DB.Where("seller_id = ?", req.UserId).Order("created_at DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&listings).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list user listings")
    }

    var total int64
    s.DB.Model(&Listing{}).Where("seller_id = ?", req.UserId).Count(&total)

    var pbListings []*pb.ListingResponse
    for _, l := range listings {
        pbListings = append(pbListings, &pb.ListingResponse{
            Id:          l.ID,
            Title:       l.Title,
            Price:       l.Price,
            ListingType: l.ListingType,
            Status:      l.Status,
            CreatedAt:   l.CreatedAt.Unix(),
        })
    }

    return &pb.ListListingsResponse{Listings: pbListings, Total: int32(total)}, nil
}

// UpdateListingStatus updates listing status (sold, rented, cancelled)
func (s *MarketplaceServer) UpdateListingStatus(ctx context.Context, req *pb.UpdateListingStatusRequest) (*pb.Empty, error) {
    var listing Listing
    if err := s.DB.Where("id = ? AND seller_id = ?", req.ListingId, req.UserId).First(&listing).Error; err != nil {
        return nil, status.Error(codes.NotFound, "listing not found")
    }

    listing.Status = req.Status
    listing.UpdatedAt = time.Now()

    if err := s.DB.Save(&listing).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update listing status")
    }

    return &pb.Empty{}, nil
}

// DeleteListing deletes a listing (soft delete for GDPR)
func (s *MarketplaceServer) DeleteListing(ctx context.Context, req *pb.DeleteListingRequest) (*pb.Empty, error) {
    var listing Listing
    if err := s.DB.Where("id = ? AND seller_id = ?", req.ListingId, req.UserId).First(&listing).Error; err != nil {
        return nil, status.Error(codes.NotFound, "listing not found")
    }

    if err := s.DB.Delete(&listing).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to delete listing")
    }

    return &pb.Empty{}, nil
}

// CreateTransaction creates a transaction when an item is bought/rented
func (s *MarketplaceServer) CreateTransaction(ctx context.Context, req *pb.CreateTransactionRequest) (*pb.TransactionResponse, error) {
    var listing Listing
    if err := s.DB.Where("id = ? AND status = ?", req.ListingId, "active").First(&listing).Error; err != nil {
        return nil, status.Error(codes.NotFound, "listing not found or not active")
    }

    if listing.SellerID == req.BuyerId {
        return nil, status.Error(codes.InvalidArgument, "cannot buy your own listing")
    }

    tx := &MarketplaceTransaction{
        ID:        generateID(),
        ListingID: req.ListingId,
        BuyerID:   req.BuyerId,
        SellerID:  listing.SellerID,
        Amount:    listing.Price,
        Status:    "pending",
        CreatedAt: time.Now(),
    }

    if req.RentalStart > 0 {
        start := time.Unix(req.RentalStart, 0)
        end := time.Unix(req.RentalEnd, 0)
        tx.RentalStart = &start
        tx.RentalEnd = &end
    }

    if err := s.DB.Create(tx).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create transaction")
    }

    return &pb.TransactionResponse{
        Id:        tx.ID,
        ListingId: tx.ListingID,
        BuyerId:   tx.BuyerID,
        SellerId:  tx.SellerID,
        Amount:    tx.Amount,
        Status:    tx.Status,
        CreatedAt: tx.CreatedAt.Unix(),
    }, nil
}

// CompleteTransaction marks a transaction as completed
func (s *MarketplaceServer) CompleteTransaction(ctx context.Context, req *pb.CompleteTransactionRequest) (*pb.Empty, error) {
    var tx MarketplaceTransaction
    if err := s.DB.Where("id = ?", req.TransactionId).First(&tx).Error; err != nil {
        return nil, status.Error(codes.NotFound, "transaction not found")
    }

    now := time.Now()
    tx.Status = "completed"
    tx.CompletedAt = &now

    if err := s.DB.Save(&tx).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to complete transaction")
    }

    // Update listing status
    s.DB.Model(&Listing{}).Where("id = ?", tx.ListingID).Updates(map[string]interface{}{
        "status":     "sold",
        "updated_at": now,
    })

    return &pb.Empty{}, nil
}

// GetUserTransactions lists transactions for a user (as buyer or seller)
func (s *MarketplaceServer) GetUserTransactions(ctx context.Context, req *pb.GetUserTransactionsRequest) (*pb.TransactionsResponse, error) {
    var transactions []MarketplaceTransaction
    query := s.DB.Where("buyer_id = ? OR seller_id = ?", req.UserId, req.UserId).Order("created_at DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&transactions).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get transactions")
    }

    var pbTransactions []*pb.TransactionResponse
    for _, t := range transactions {
        pbTransactions = append(pbTransactions, &pb.TransactionResponse{
            Id:        t.ID,
            ListingId: t.ListingID,
            BuyerId:   t.BuyerID,
            SellerId:  t.SellerID,
            Amount:    t.Amount,
            Status:    t.Status,
            CreatedAt: t.CreatedAt.Unix(),
        })
    }

    var total int64
    s.DB.Model(&MarketplaceTransaction{}).Where("buyer_id = ? OR seller_id = ?", req.UserId, req.UserId).Count(&total)

    return &pb.TransactionsResponse{Transactions: pbTransactions, Total: int32(total)}, nil
}

func generateID() string {
    return "mkt_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=marketplacedb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Listing{}, &MarketplaceTransaction{})

    grpcServer := grpc.NewServer()
    pb.RegisterMarketplaceServiceServer(grpcServer, &MarketplaceServer{DB: db})

    lis, err := net.Listen("tcp", ":50079")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Marketplace Service running on port 50079")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Marketplace Service stopped")
}