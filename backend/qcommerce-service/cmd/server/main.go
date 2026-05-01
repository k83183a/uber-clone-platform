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

    pb "github.com/uber-clone/qcommerce-service/proto"
)

// DarkStore represents a dark store (dedicated fulfillment center)
type DarkStore struct {
    ID              string    `gorm:"primaryKey"`
    Name            string    `gorm:"not null"`
    Address         string    `gorm:"not null"`
    Latitude        float64   `gorm:"not null"`
    Longitude       float64   `gorm:"not null"`
    DeliveryTimeMin int       `gorm:"default:15"` // promised delivery time
    MinOrder        float64   `gorm:"default:0"`
    DeliveryFee     float64   `gorm:"default:0"`
    IsActive        bool      `gorm:"default:true"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

// Product represents an item in a dark store
type Product struct {
    ID            string    `gorm:"primaryKey"`
    StoreID       string    `gorm:"index;not null"`
    Name          string    `gorm:"not null"`
    Description   string
    Price         float64   `gorm:"not null"`
    DiscountPrice float64
    Stock         int       `gorm:"not null;default:0"`
    Unit          string    // kg, g, ml, L, piece
    ImageURL      string
    Category      string    `gorm:"index"`
    IsAvailable   bool      `gorm:"default:true"`
    IsPopular     bool      `gorm:"default:false"`
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

// QCOrder represents an express order
type QCOrder struct {
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

// QCOrderItem represents an item in an express order
type QCOrderItem struct {
    ID          string  `gorm:"primaryKey"`
    OrderID     string  `gorm:"index;not null"`
    ProductID   string  `gorm:"index"`
    Name        string  `gorm:"not null"`
    Quantity    int     `gorm:"not null"`
    UnitPrice   float64 `gorm:"not null"`
    Subtotal    float64 `gorm:"not null"`
    CreatedAt   time.Time
}

// QCommerceServer handles gRPC requests
type QCommerceServer struct {
    pb.UnimplementedQCommerceServiceServer
    DB *gorm.DB
}

// ListStores returns dark stores near a location
func (s *QCommerceServer) ListStores(ctx context.Context, req *pb.ListStoresRequest) (*pb.ListStoresResponse, error) {
    var stores []DarkStore
    query := s.DB.Where("is_active = ?", true)

    if req.Latitude != 0 && req.Longitude != 0 {
        // In production: use PostGIS for distance-based sorting
        query = query.Order("latitude")
    }

    if err := query.Find(&stores).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list stores")
    }

    var pbStores []*pb.Store
    for _, s := range stores {
        pbStores = append(pbStores, &pb.Store{
            Id:              s.ID,
            Name:            s.Name,
            Address:         s.Address,
            Latitude:        s.Latitude,
            Longitude:       s.Longitude,
            DeliveryTimeMin: int32(s.DeliveryTimeMin),
            MinOrder:        s.MinOrder,
            DeliveryFee:     s.DeliveryFee,
        })
    }

    return &pb.ListStoresResponse{Stores: pbStores}, nil
}

// ListProducts returns products available in a dark store
func (s *QCommerceServer) ListProducts(ctx context.Context, req *pb.ListProductsRequest) (*pb.ListProductsResponse, error) {
    var products []Product
    query := s.DB.Where("store_id = ? AND is_available = ? AND stock > ?", req.StoreId, true, 0)

    if req.Category != "" {
        query = query.Where("category = ?", req.Category)
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
            Category:      p.Category,
        })
    }

    return &pb.ListProductsResponse{Products: pbProducts, Total: int32(total)}, nil
}

// PlaceOrder creates a new express order
func (s *QCommerceServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.OrderResponse, error) {
    var store DarkStore
    if err := s.DB.Where("id = ?", req.StoreId).First(&store).Error; err != nil {
        return nil, status.Error(codes.NotFound, "store not found")
    }

    // Calculate totals and check stock
    var subtotal float64
    var orderItems []QCOrderItem

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

        orderItems = append(orderItems, QCOrderItem{
            ID:        generateID(),
            ProductID: product.ID,
            Name:      product.Name,
            Quantity:  int(item.Quantity),
            UnitPrice: product.Price,
            Subtotal:  itemTotal,
            CreatedAt: time.Now(),
        })

        // Reserve stock
        product.Stock -= int(item.Quantity)
        s.DB.Save(&product)
    }

    tax := subtotal * 0.20 // 20% VAT
    total := subtotal + store.DeliveryFee + tax

    order := &QCOrder{
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
func (s *QCommerceServer) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.OrderDetailResponse, error) {
    var order QCOrder
    if err := s.DB.Where("id = ?", req.OrderId).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    var items []QCOrderItem
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
func (s *QCommerceServer) UpdateOrderStatus(ctx context.Context, req *pb.UpdateOrderStatusRequest) (*pb.Empty, error) {
    var order QCOrder
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
func (s *QCommerceServer) ListUserOrders(ctx context.Context, req *pb.ListUserOrdersRequest) (*pb.ListOrdersResponse, error) {
    var orders []QCOrder
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

// UpdateStock updates product stock (admin/store manager)
func (s *QCommerceServer) UpdateStock(ctx context.Context, req *pb.UpdateStockRequest) (*pb.Empty, error) {
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
    return "qc_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateOrderNumber() string {
    return "QCO" + time.Now().Format("20060102") + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=qcommercedb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&DarkStore{}, &Product{}, &QCOrder{}, &QCOrderItem{})

    // Seed sample dark store
    var count int64
    db.Model(&DarkStore{}).Count(&count)
    if count == 0 {
        store := &DarkStore{
            ID:              generateID(),
            Name:            "Express Hub London",
            Address:         "123 Delivery Road, London",
            Latitude:        51.5074,
            Longitude:       -0.1278,
            DeliveryTimeMin: 15,
            MinOrder:        5.0,
            DeliveryFee:     1.99,
            IsActive:        true,
            CreatedAt:       time.Now(),
            UpdatedAt:       time.Now(),
        }
        db.Create(store)

        // Seed some products
        products := []Product{
            {ID: generateID(), StoreID: store.ID, Name: "Milk 1L", Price: 1.20, Stock: 100, Unit: "bottle", Category: "Dairy", IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), StoreID: store.ID, Name: "Bread", Price: 1.50, Stock: 50, Unit: "loaf", Category: "Bakery", IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), StoreID: store.ID, Name: "Eggs (6 pack)", Price: 1.80, Stock: 80, Unit: "pack", Category: "Dairy", IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
            {ID: generateID(), StoreID: store.ID, Name: "Coca Cola 500ml", Price: 1.50, Stock: 200, Unit: "bottle", Category: "Beverages", IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        }
        db.Create(&products)
        log.Println("Seeded dark store and products")
    }

    grpcServer := grpc.NewServer()
    pb.RegisterQCommerceServiceServer(grpcServer, &QCommerceServer{DB: db})

    lis, err := net.Listen("tcp", ":50068")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Q‑Commerce Service running on port 50068")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Q‑Commerce Service stopped")
}