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

    pb "github.com/uber-clone/grocery-service/proto"
)

// Store represents a grocery store (including dark stores)
type Store struct {
    ID          string    `gorm:"primaryKey"`
    Name        string    `gorm:"not null"`
    Description string
    Address     string    `gorm:"not null"`
    Latitude    float64   `gorm:"not null"`
    Longitude   float64   `gorm:"not null"`
    Phone       string
    Email       string
    LogoURL     string
    Rating      float64   `gorm:"default:0"`
    RatingCount int       `gorm:"default:0"`
    IsActive    bool      `gorm:"default:true"`
    MinOrder    float64   `gorm:"default:0"`
    DeliveryFee float64   `gorm:"default:0"`
    DeliveryTimeMin int   `gorm:"default:30"` // Estimated delivery time in minutes
    IsDarkStore bool      `gorm:"default:false"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// Category represents a product category (e.g., Fruits, Vegetables, Dairy)
type Category struct {
    ID          string `gorm:"primaryKey"`
    StoreID     string `gorm:"index;not null"`
    Name        string `gorm:"not null"`
    Description string
    ImageURL    string
    SortOrder   int    `gorm:"default:0"`
    CreatedAt   time.Time
}

// Product represents a grocery product
type Product struct {
    ID          string    `gorm:"primaryKey"`
    StoreID     string    `gorm:"index;not null"`
    CategoryID  string    `gorm:"index"`
    Name        string    `gorm:"not null"`
    Description string
    Price       float64   `gorm:"not null"`
    DiscountPrice float64
    Stock       int       `gorm:"not null;default:0"`
    Unit        string    // kg, g, ml, L, piece, pack
    ImageURL    string
    IsAvailable bool      `gorm:"default:true"`
    IsFeatured  bool      `gorm:"default:false"`
    Barcode     string
    Weight      float64   // in grams or kg
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// Order represents a grocery order
type Order struct {
    ID               string     `gorm:"primaryKey"`
    OrderNumber      string     `gorm:"uniqueIndex"`
    UserID           string     `gorm:"index;not null"`
    StoreID          string     `gorm:"index;not null"`
    Status           string     `gorm:"default:'pending'"` // pending, confirmed, picking, ready, out_for_delivery, delivered, cancelled
    Subtotal         float64    `gorm:"not null"`
    DeliveryFee      float64    `gorm:"not null"`
    Tax              float64    `gorm:"not null"`
    Total            float64    `gorm:"not null"`
    PaymentMethod    string
    DeliveryAddress  string
    DeliveryLat      float64
    DeliveryLng      float64
    DeliveryInstructions string
    DriverID         string     `gorm:"index"`
    EstimatedTime    int        // minutes
    CreatedAt        time.Time
    UpdatedAt        time.Time
    CompletedAt      *time.Time
    CancelledAt      *time.Time
    CancelledReason  string
}

// OrderItem represents an item in a grocery order
type OrderItem struct {
    ID          string  `gorm:"primaryKey"`
    OrderID     string  `gorm:"index;not null"`
    ProductID   string  `gorm:"index"`
    Name        string  `gorm:"not null"`
    Quantity    int     `gorm:"not null"`
    UnitPrice   float64 `gorm:"not null"`
    Subtotal    float64 `gorm:"not null"`
    CreatedAt   time.Time
}

// GroceryServer handles gRPC requests
type GroceryServer struct {
    pb.UnimplementedGroceryServiceServer
    DB *gorm.DB
}

// ListStores returns stores near a location
func (s *GroceryServer) ListStores(ctx context.Context, req *pb.ListStoresRequest) (*pb.ListStoresResponse, error) {
    var stores []Store
    query := s.DB.Where("is_active = ?", true)

    if req.Latitude != 0 && req.Longitude != 0 {
        // In production, use PostGIS for distance-based sorting
        query = query.Order("latitude")
    }

    if req.Query != "" {
        query = query.Where("name ILIKE ?", "%"+req.Query+"%")
    }

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&stores).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list stores")
    }

    var total int64
    s.DB.Model(&Store{}).Where("is_active = ?", true).Count(&total)

    var pbStores []*pb.Store
    for _, s := range stores {
        pbStores = append(pbStores, &pb.Store{
            Id:          s.ID,
            Name:        s.Name,
            Description: s.Description,
            Address:     s.Address,
            Latitude:    s.Latitude,
            Longitude:   s.Longitude,
            Phone:       s.Phone,
            LogoUrl:     s.LogoURL,
            Rating:      s.Rating,
            MinOrder:    s.MinOrder,
            DeliveryFee: s.DeliveryFee,
            DeliveryTimeMin: int32(s.DeliveryTimeMin),
        })
    }

    return &pb.ListStoresResponse{
        Stores: pbStores,
        Total:  int32(total),
    }, nil
}

// GetStore returns store details
func (s *GroceryServer) GetStore(ctx context.Context, req *pb.GetStoreRequest) (*pb.Store, error) {
    var store Store
    if err := s.DB.Where("id = ? AND is_active = ?", req.Id, true).First(&store).Error; err != nil {
        return nil, status.Error(codes.NotFound, "store not found")
    }

    return &pb.Store{
        Id:          store.ID,
        Name:        store.Name,
        Description: store.Description,
        Address:     store.Address,
        Latitude:    store.Latitude,
        Longitude:   store.Longitude,
        Phone:       store.Phone,
        LogoUrl:     store.LogoURL,
        Rating:      store.Rating,
        MinOrder:    store.MinOrder,
        DeliveryFee: store.DeliveryFee,
        DeliveryTimeMin: int32(store.DeliveryTimeMin),
    }, nil
}

// ListProducts returns products for a store
func (s *GroceryServer) ListProducts(ctx context.Context, req *pb.ListProductsRequest) (*pb.ListProductsResponse, error) {
    var products []Product
    query := s.DB.Where("store_id = ? AND is_available = ?", req.StoreId, true)

    if req.CategoryId != "" {
        query = query.Where("category_id = ?", req.CategoryId)
    }

    if req.Query != "" {
        query = query.Where("name ILIKE ?", "%"+req.Query+"%")
    }

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&products).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list products")
    }

    var total int64
    s.DB.Model(&Product{}).Where("store_id = ? AND is_available = ?", req.StoreId, true).Count(&total)

    var pbProducts []*pb.Product
    for _, p := range products {
        pbProducts = append(pbProducts, &pb.Product{
            Id:            p.ID,
            Name:          p.Name,
            Description:   p.Description,
            Price:         p.Price,
            DiscountPrice: p.DiscountPrice,
            Stock:         int32(p.Stock),
            Unit:          p.Unit,
            ImageUrl:      p.ImageURL,
            IsAvailable:   p.IsAvailable,
            IsFeatured:    p.IsFeatured,
        })
    }

    return &pb.ListProductsResponse{
        Products: pbProducts,
        Total:    int32(total),
    }, nil
}

// GetProduct returns product details
func (s *GroceryServer) GetProduct(ctx context.Context, req *pb.GetProductRequest) (*pb.Product, error) {
    var product Product
    if err := s.DB.Where("id = ?", req.Id).First(&product).Error; err != nil {
        return nil, status.Error(codes.NotFound, "product not found")
    }

    return &pb.Product{
        Id:            product.ID,
        Name:          product.Name,
        Description:   product.Description,
        Price:         product.Price,
        DiscountPrice: product.DiscountPrice,
        Stock:         int32(product.Stock),
        Unit:          product.Unit,
        ImageUrl:      product.ImageURL,
        IsAvailable:   product.IsAvailable,
        IsFeatured:    product.IsFeatured,
    }, nil
}

// PlaceOrder creates a new grocery order
func (s *GroceryServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.OrderResponse, error) {
    // Verify store exists
    var store Store
    if err := s.DB.Where("id = ?", req.StoreId).First(&store).Error; err != nil {
        return nil, status.Error(codes.NotFound, "store not found")
    }

    // Calculate totals and check stock
    var subtotal float64
    var orderItems []OrderItem

    for _, item := range req.Items {
        var product Product
        if err := s.DB.Where("id = ?", item.ProductId).First(&product).Error; err != nil {
            return nil, status.Error(codes.NotFound, "product not found")
        }

        if product.Stock < int(item.Quantity) {
            return nil, status.Error(codes.ResourceExhausted, "insufficient stock for product: "+product.Name)
        }

        itemTotal := product.Price * float64(item.Quantity)
        subtotal += itemTotal

        orderItems = append(orderItems, OrderItem{
            ID:        generateID(),
            ProductID: product.ID,
            Name:      product.Name,
            Quantity:  int(item.Quantity),
            UnitPrice: product.Price,
            Subtotal:  itemTotal,
            CreatedAt: time.Now(),
        })

        // Reserve stock (decrement)
        product.Stock -= int(item.Quantity)
        s.DB.Save(&product)
    }

    tax := subtotal * 0.20 // 20% VAT
    total := subtotal + store.DeliveryFee + tax

    order := &Order{
        ID:               generateID(),
        OrderNumber:      generateOrderNumber(),
        UserID:           req.UserId,
        StoreID:          req.StoreId,
        Status:           "pending",
        Subtotal:         subtotal,
        DeliveryFee:      store.DeliveryFee,
        Tax:              tax,
        Total:            total,
        PaymentMethod:    req.PaymentMethod,
        DeliveryAddress:  req.DeliveryAddress,
        DeliveryLat:      req.DeliveryLat,
        DeliveryLng:      req.DeliveryLng,
        DeliveryInstructions: req.DeliveryInstructions,
        EstimatedTime:    store.DeliveryTimeMin,
        CreatedAt:        time.Now(),
        UpdatedAt:        time.Now(),
    }

    // Create order in transaction
    err := s.DB.Transaction(func(tx *gorm.DB) error {
        if err := tx.Create(order).Error; err != nil {
            return err
        }
        for _, item := range orderItems {
            item.OrderID = order.ID
            if err := tx.Create(&item).Error; err != nil {
                return err
            }
        }
        return nil
    })

    if err != nil {
        return nil, status.Error(codes.Internal, "failed to place order")
    }

    return &pb.OrderResponse{
        Id:         order.ID,
        OrderNumber: order.OrderNumber,
        Status:     order.Status,
        Total:      order.Total,
        CreatedAt:  order.CreatedAt.String(),
    }, nil
}

// GetOrder returns order details
func (s *GroceryServer) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.OrderDetailResponse, error) {
    var order Order
    if err := s.DB.Where("id = ?", req.OrderId).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    var items []OrderItem
    s.DB.Where("order_id = ?", order.ID).Find(&items)

    var pbItems []*pb.OrderItem
    for _, i := range items {
        pbItems = append(pbItems, &pb.OrderItem{
            Id:        i.ID,
            Name:      i.Name,
            Quantity:  int32(i.Quantity),
            UnitPrice: i.UnitPrice,
            Subtotal:  i.Subtotal,
        })
    }

    return &pb.OrderDetailResponse{
        Id:          order.ID,
        OrderNumber: order.OrderNumber,
        Status:      order.Status,
        Subtotal:    order.Subtotal,
        DeliveryFee: order.DeliveryFee,
        Tax:         order.Tax,
        Total:       order.Total,
        Items:       pbItems,
        DeliveryAddress: order.DeliveryAddress,
        CreatedAt:   order.CreatedAt.String(),
    }, nil
}

// UpdateOrderStatus updates order status
func (s *GroceryServer) UpdateOrderStatus(ctx context.Context, req *pb.UpdateOrderStatusRequest) (*pb.Empty, error) {
    var order Order
    if err := s.DB.Where("id = ?", req.OrderId).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    updates := map[string]interface{}{
        "status":     req.Status,
        "updated_at": time.Now(),
    }

    if req.Status == "delivered" {
        now := time.Now()
        updates["completed_at"] = now
    } else if req.Status == "cancelled" {
        now := time.Now()
        updates["cancelled_at"] = now
        updates["cancelled_reason"] = req.Reason
    }

    if req.DriverId != "" {
        updates["driver_id"] = req.DriverId
    }

    if err := s.DB.Model(&order).Updates(updates).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update order status")
    }

    return &pb.Empty{}, nil
}

// ListUserOrders lists orders for a user
func (s *GroceryServer) ListUserOrders(ctx context.Context, req *pb.ListUserOrdersRequest) (*pb.ListOrdersResponse, error) {
    var orders []Order
    query := s.DB.Where("user_id = ?", req.UserId).Order("created_at DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&orders).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list orders")
    }

    var pbOrders []*pb.OrderResponse
    for _, o := range orders {
        pbOrders = append(pbOrders, &pb.OrderResponse{
            Id:         o.ID,
            OrderNumber: o.OrderNumber,
            Status:     o.Status,
            Total:      o.Total,
            CreatedAt:  o.CreatedAt.String(),
        })
    }

    return &pb.ListOrdersResponse{Orders: pbOrders}, nil
}

// UpdateStock updates product stock (for store owners)
func (s *GroceryServer) UpdateStock(ctx context.Context, req *pb.UpdateStockRequest) (*pb.Empty, error) {
    var product Product
    if err := s.DB.Where("id = ?", req.ProductId).First(&product).Error; err != nil {
        return nil, status.Error(codes.NotFound, "product not found")
    }

    product.Stock = int(req.NewStock)
    product.UpdatedAt = time.Now()
    product.IsAvailable = req.NewStock > 0

    if err := s.DB.Save(&product).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update stock")
    }

    return &pb.Empty{}, nil
}

func generateID() string {
    return "grocery_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateOrderNumber() string {
    return "GRC" + time.Now().Format("20060102") + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=grocerydb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Store{}, &Category{}, &Product{}, &Order{}, &OrderItem{})

    grpcServer := grpc.NewServer()
    pb.RegisterGroceryServiceServer(grpcServer, &GroceryServer{DB: db})

    lis, err := net.Listen("tcp", ":50057")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Grocery Service running on port 50057")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Grocery Service stopped")
}