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

// ============================================================
// MODELS
// ============================================================

type Store struct {
    ID              string    `gorm:"primaryKey"`
    Name            string    `gorm:"not null"`
    Description     string
    Address         string    `gorm:"not null"`
    Latitude        float64   `gorm:"not null"`
    Longitude       float64   `gorm:"not null"`
    Phone           string
    LogoURL         string
    CoverImageURL   string
    Rating          float64   `gorm:"default:0"`
    RatingCount     int       `gorm:"default:0"`
    MinOrder        float64   `gorm:"default:0"`
    DeliveryFee     float64   `gorm:"default:0"`
    DeliveryTimeMin int       `gorm:"default:30"`
    IsActive        bool      `gorm:"default:true"`
    IsDarkStore     bool      `gorm:"default:false"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type Category struct {
    ID          string `gorm:"primaryKey"`
    StoreID     string `gorm:"index;not null"`
    Name        string `gorm:"not null"`
    ImageURL    string
    SortOrder   int    `gorm:"default:0"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Product struct {
    ID            string    `gorm:"primaryKey"`
    StoreID       string    `gorm:"index;not null"`
    CategoryID    string    `gorm:"index"`
    Name          string    `gorm:"not null"`
    Description   string
    Price         float64   `gorm:"not null"`
    DiscountPrice float64
    Stock         int       `gorm:"not null;default:0"`
    Unit          string    // kg, g, ml, L, piece, pack
    ImageURL      string
    Barcode       string
    Weight        float64   // in grams
    IsAvailable   bool      `gorm:"default:true"`
    IsFeatured    bool      `gorm:"default:false"`
    Category      string    `gorm:"index"`
    Popularity    int       `gorm:"default:0"`
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

type GroceryOrder struct {
    ID                   string     `gorm:"primaryKey"`
    OrderNumber          string     `gorm:"uniqueIndex"`
    UserID               string     `gorm:"index;not null"`
    StoreID              string     `gorm:"index;not null"`
    Status               string     `gorm:"default:'pending'"`
    Subtotal             float64    `gorm:"not null"`
    DeliveryFee          float64    `gorm:"not null"`
    ServiceFee           float64    `gorm:"default:0"`
    Tax                  float64    `gorm:"not null"`
    Total                float64    `gorm:"not null"`
    Discount             float64    `gorm:"default:0"`
    PromoCode            string
    PaymentMethod        string
    DeliveryAddress      string
    DeliveryLat          float64
    DeliveryLng          float64
    DeliveryInstructions string
    DriverID             string     `gorm:"index"`
    EstimatedTime        int
    ActualTime           int
    CreatedAt            time.Time
    ConfirmedAt          *time.Time
    PickingAt            *time.Time
    ReadyAt              *time.Time
    OutForDeliveryAt     *time.Time
    DeliveredAt          *time.Time
    CancelledAt          *time.Time
    CancelledReason      string
}

type GroceryOrderItem struct {
    ID          string  `gorm:"primaryKey"`
    OrderID     string  `gorm:"index;not null"`
    ProductID   string  `gorm:"index"`
    Name        string  `gorm:"not null"`
    Quantity    int     `gorm:"not null"`
    UnitPrice   float64 `gorm:"not null"`
    Subtotal    float64 `gorm:"not null"`
    CreatedAt   time.Time
}

// ============================================================
// GRPC SERVER
// ============================================================

type GroceryServer struct {
    pb.UnimplementedGroceryServiceServer
    DB *gorm.DB
}

// ListStores - Get stores near a location
func (s *GroceryServer) ListStores(ctx context.Context, req *pb.ListStoresRequest) (*pb.ListStoresResponse, error) {
    var stores []Store
    query := s.DB.Where("is_active = ?", true)

    if req.Query != "" {
        query = query.Where("name ILIKE ?", "%"+req.Query+"%")
    }

    if req.IsDarkStore {
        query = query.Where("is_dark_store = ?", true)
    }

    // Sort by distance (simplified)
    if req.Latitude != 0 && req.Longitude != 0 {
        query = query.Order("ABS(latitude - ?) + ABS(longitude - ?)", req.Latitude, req.Longitude)
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

    return &pb.ListStoresResponse{Stores: pbStores, Total: int32(total)}, nil
}

// GetStore - Get store details
func (s *GroceryServer) GetStore(ctx context.Context, req *pb.GetStoreRequest) (*pb.StoreDetail, error) {
    var store Store
    if err := s.DB.Where("id = ?", req.StoreId).First(&store).Error; err != nil {
        return nil, status.Error(codes.NotFound, "store not found")
    }

    return &pb.StoreDetail{
        Id:            store.ID,
        Name:          store.Name,
        Description:   store.Description,
        Address:       store.Address,
        Latitude:      store.Latitude,
        Longitude:     store.Longitude,
        Phone:         store.Phone,
        LogoUrl:       store.LogoURL,
        CoverImageUrl: store.CoverImageURL,
        Rating:        store.Rating,
        RatingCount:   int32(store.RatingCount),
        MinOrder:      store.MinOrder,
        DeliveryFee:   store.DeliveryFee,
        DeliveryTime:  int32(store.DeliveryTimeMin),
    }, nil
}

// ListProducts - Get products with filters
func (s *GroceryServer) ListProducts(ctx context.Context, req *pb.ListProductsRequest) (*pb.ListProductsResponse, error) {
    var products []Product
    query := s.DB.Where("store_id = ? AND is_available = ? AND stock > ?", req.StoreId, true, 0)

    if req.Category != "" {
        query = query.Where("category = ?", req.Category)
    }

    if req.Query != "" {
        query = query.Where("name ILIKE ?", "%"+req.Query+"%")
    }

    if req.IsFeatured {
        query = query.Where("is_featured = ?", true)
    }

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Order("popularity DESC").Find(&products).Error; err != nil {
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

// GetProduct - Get product details
func (s *GroceryServer) GetProduct(ctx context.Context, req *pb.GetProductRequest) (*pb.Product, error) {
    var product Product
    if err := s.DB.Where("id = ?", req.ProductId).First(&product).Error; err != nil {
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
        Category:      product.Category,
    }, nil
}

// PlaceOrder - Create a new grocery order
func (s *GroceryServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.OrderResponse, error) {
    var store Store
    if err := s.DB.Where("id = ?", req.StoreId).First(&store).Error; err != nil {
        return nil, status.Error(codes.NotFound, "store not found")
    }

    var subtotal float64
    var orderItems []GroceryOrderItem
    var maxPickTime int = 15 // default picking time

    for _, item := range req.Items {
        var product Product
        if err := s.DB.Where("id = ?", item.ProductId).First(&product).Error; err != nil {
            return nil, status.Error(codes.NotFound, "product not found")
        }

        if product.Stock < int(item.Quantity) {
            return nil, status.Error(codes.ResourceExhausted, "insufficient stock for product: "+product.Name)
        }

        // Reserve stock
        product.Stock -= int(item.Quantity)
        s.DB.Save(&product)

        price := product.Price
        if product.DiscountPrice > 0 && product.DiscountPrice < price {
            price = product.DiscountPrice
        }

        itemTotal := price * float64(item.Quantity)
        subtotal += itemTotal

        orderItems = append(orderItems, GroceryOrderItem{
            ID:        generateID(),
            ProductID: product.ID,
            Name:      product.Name,
            Quantity:  int(item.Quantity),
            UnitPrice: price,
            Subtotal:  itemTotal,
            CreatedAt: time.Now(),
        })

        // Update popularity
        s.DB.Model(&Product{}).Where("id = ?", product.ID).Update("popularity", gorm.Expr("popularity + ?", item.Quantity))
    }

    // Apply discount if promo code provided
    discount := 0.0
    if req.PromoCode != "" {
        discount = s.calculateDiscount(req.PromoCode, subtotal)
    }

    serviceFee := subtotal * 0.05 // 5% service fee
    tax := (subtotal - discount) * 0.20 // 20% VAT
    total := subtotal - discount + store.DeliveryFee + serviceFee + tax

    order := &GroceryOrder{
        ID:                   generateID(),
        OrderNumber:          generateOrderNumber(),
        UserID:               req.UserId,
        StoreID:              req.StoreId,
        Status:               "pending",
        Subtotal:             subtotal,
        DeliveryFee:          store.DeliveryFee,
        ServiceFee:           serviceFee,
        Tax:                  tax,
        Total:                total,
        Discount:             discount,
        PromoCode:            req.PromoCode,
        PaymentMethod:        req.PaymentMethod,
        DeliveryAddress:      req.DeliveryAddress,
        DeliveryLat:          req.DeliveryLat,
        DeliveryLng:          req.DeliveryLng,
        DeliveryInstructions: req.DeliveryInstructions,
        EstimatedTime:        store.DeliveryTimeMin + maxPickTime,
        CreatedAt:            time.Now(),
    }

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

    log.Printf("🛒 New grocery order %s: £%.2f from %s", order.OrderNumber, order.Total, store.Name)

    return &pb.OrderResponse{
        Id:         order.ID,
        OrderNumber: order.OrderNumber,
        Status:     order.Status,
        Total:      order.Total,
        EstimatedTime: int32(order.EstimatedTime),
        CreatedAt:  order.CreatedAt.Unix(),
    }, nil
}

// GetOrder - Get order details
func (s *GroceryServer) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.OrderDetailResponse, error) {
    var order GroceryOrder
    if err := s.DB.Where("id = ? OR order_number = ?", req.OrderId, req.OrderNumber).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    var items []GroceryOrderItem
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

    var driverInfo *pb.DriverInfo
    if order.DriverID != "" {
        driverInfo = &pb.DriverInfo{DriverId: order.DriverID}
    }

    return &pb.OrderDetailResponse{
        Id:          order.ID,
        OrderNumber: order.OrderNumber,
        Status:      order.Status,
        Subtotal:    order.Subtotal,
        DeliveryFee: order.DeliveryFee,
        ServiceFee:  order.ServiceFee,
        Tax:         order.Tax,
        Discount:    order.Discount,
        Total:       order.Total,
        Items:       pbItems,
        DeliveryAddress: order.DeliveryAddress,
        EstimatedTime: int32(order.EstimatedTime),
        Driver:      driverInfo,
        CreatedAt:   order.CreatedAt.Unix(),
    }, nil
}

// UpdateOrderStatus - Update order status
func (s *GroceryServer) UpdateOrderStatus(ctx context.Context, req *pb.UpdateOrderStatusRequest) (*pb.Empty, error) {
    var order GroceryOrder
    if err := s.DB.Where("id = ?", req.OrderId).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    now := time.Now()
    updates := map[string]interface{}{"status": req.Status}

    switch req.Status {
    case "confirmed":
        updates["confirmed_at"] = now
    case "picking":
        updates["picking_at"] = now
    case "ready":
        updates["ready_at"] = now
    case "out_for_delivery":
        updates["out_for_delivery_at"] = now
        if req.DriverId != "" {
            updates["driver_id"] = req.DriverId
        }
    case "delivered":
        updates["delivered_at"] = now
        actualTime := int(now.Sub(order.CreatedAt).Minutes())
        updates["actual_time"] = actualTime
    case "cancelled":
        updates["cancelled_at"] = now
        updates["cancelled_reason"] = req.Reason
    }

    if err := s.DB.Model(&order).Updates(updates).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update order status")
    }

    return &pb.Empty{}, nil
}

// CancelOrder - Cancel an order
func (s *GroceryServer) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.Empty, error) {
    var order GroceryOrder
    if err := s.DB.Where("id = ? AND user_id = ?", req.OrderId, req.UserId).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    if order.Status != "pending" && order.Status != "confirmed" {
        return nil, status.Error(codes.FailedPrecondition, "order cannot be cancelled")
    }

    // Restore stock
    var items []GroceryOrderItem
    s.DB.Where("order_id = ?", order.ID).Find(&items)
    for _, item := range items {
        s.DB.Model(&Product{}).Where("id = ?", item.ProductID).Update("stock", gorm.Expr("stock + ?", item.Quantity))
    }

    now := time.Now()
    order.Status = "cancelled"
    order.CancelledAt = &now
    order.CancelledReason = req.Reason
    s.DB.Save(&order)

    return &pb.Empty{}, nil
}

// ListUserOrders - List orders for a user
func (s *GroceryServer) ListUserOrders(ctx context.Context, req *pb.ListUserOrdersRequest) (*pb.ListOrdersResponse, error) {
    var orders []GroceryOrder
    query := s.DB.Where("user_id = ?", req.UserId).Order("created_at DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&orders).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list orders")
    }

    var total int64
    s.DB.Model(&GroceryOrder{}).Where("user_id = ?", req.UserId).Count(&total)

    var pbOrders []*pb.OrderSummary
    for _, o := range orders {
        pbOrders = append(pbOrders, &pb.OrderSummary{
            Id:         o.ID,
            OrderNumber: o.OrderNumber,
            Status:     o.Status,
            Total:      o.Total,
            CreatedAt:  o.CreatedAt.Unix(),
        })
    }

    return &pb.ListOrdersResponse{Orders: pbOrders, Total: int32(total)}, nil
}

// UpdateStock - Update product stock (store admin)
func (s *GroceryServer) UpdateStock(ctx context.Context, req *pb.UpdateStockRequest) (*pb.Empty, error) {
    var product Product
    if err := s.DB.Where("id = ?", req.ProductId).First(&product).Error; err != nil {
        return nil, status.Error(codes.NotFound, "product not found")
    }

    product.Stock = int(req.NewStock)
    product.IsAvailable = req.NewStock > 0
    product.UpdatedAt = time.Now()
    s.DB.Save(&product)

    // Check low stock alert (stock < 10)
    if product.Stock < 10 {
        log.Printf("⚠️ Low stock alert: %s has only %d units left", product.Name, product.Stock)
    }

    return &pb.Empty{}, nil
}

func (s *GroceryServer) calculateDiscount(promoCode string, subtotal float64) float64 {
    switch promoCode {
    case "GROCERY20":
        return subtotal * 0.20
    case "FREEDELIVERY":
        return 0 // Will affect delivery fee separately
    default:
        return 0
    }
}

func generateID() string {
    return "groc_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateOrderNumber() string {
    return "GRO" + time.Now().Format("20060102") + randomString(6)
}

func randomString(n int) string {
    const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
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

    db.AutoMigrate(&Store{}, &Category{}, &Product{}, &GroceryOrder{}, &GroceryOrderItem{})

    seedSampleData(db)

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
}

func seedSampleData(db *gorm.DB) {
    var count int64
    db.Model(&Store{}).Count(&count)
    if count > 0 {
        return
    }

    store := Store{
        ID:          generateID(),
        Name:        "FreshMart Express",
        Description: "Fresh groceries delivered fast",
        Address:     "123 High Street, London",
        Latitude:    51.5074,
        Longitude:   -0.1278,
        Phone:       "+442012345678",
        MinOrder:    10.0,
        DeliveryFee: 2.99,
        DeliveryTimeMin: 30,
        IsActive:    true,
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
    }
    db.Create(&store)

    products := []Product{
        {ID: generateID(), StoreID: store.ID, Name: "Fresh Milk 1L", Price: 1.20, Stock: 50, Unit: "bottle", Category: "Dairy", IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), StoreID: store.ID, Name: "Whole Wheat Bread", Price: 1.50, Stock: 30, Unit: "loaf", Category: "Bakery", IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), StoreID: store.ID, Name: "Free Range Eggs (6)", Price: 1.80, Stock: 40, Unit: "pack", Category: "Dairy", IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), StoreID: store.ID, Name: "Organic Apples", Price: 2.50, Stock: 25, Unit: "kg", Category: "Fruits", IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), StoreID: store.ID, Name: "Chicken Breast", Price: 5.50, Stock: 20, Unit: "kg", Category: "Meat", IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
    }
    db.Create(&products)

    log.Println("✅ Seeded grocery store and products")
}