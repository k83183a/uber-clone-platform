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

    pb "github.com/uber-clone/food-service/proto"
)

// Restaurant represents a restaurant in the system
type Restaurant struct {
    ID          string    `gorm:"primaryKey"`
    Name        string    `gorm:"not null"`
    Description string
    Address     string    `gorm:"not null"`
    Latitude    float64   `gorm:"not null"`
    Longitude   float64   `gorm:"not null"`
    Phone       string
    Email       string
    ImageURL    string
    Rating      float64   `gorm:"default:0"`
    RatingCount int       `gorm:"default:0"`
    IsActive    bool      `gorm:"default:true"`
    MinOrder    float64   `gorm:"default:0"`
    DeliveryFee float64   `gorm:"default:0"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// Category represents a menu category (e.g., Burgers, Pizza, Drinks)
type Category struct {
    ID          string `gorm:"primaryKey"`
    RestaurantID string `gorm:"index;not null"`
    Name        string `gorm:"not null"`
    Description string
    SortOrder   int `gorm:"default:0"`
    CreatedAt   time.Time
}

// MenuItem represents an item on the menu
type MenuItem struct {
    ID          string  `gorm:"primaryKey"`
    RestaurantID string `gorm:"index;not null"`
    CategoryID   string `gorm:"index"`
    Name         string `gorm:"not null"`
    Description  string
    Price        float64 `gorm:"not null"`
    DiscountPrice float64
    ImageURL     string
    IsAvailable  bool   `gorm:"default:true"`
    IsVegetarian bool   `gorm:"default:false"`
    IsVegan      bool   `gorm:"default:false"`
    PreparationTime int `gorm:"default:15"`
    Calories     int
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

// Order represents a food order
type Order struct {
    ID               string     `gorm:"primaryKey"`
    OrderNumber      string     `gorm:"uniqueIndex"`
    UserID           string     `gorm:"index;not null"`
    RestaurantID     string     `gorm:"index;not null"`
    Status           string     `gorm:"default:'pending'"` // pending, confirmed, preparing, ready, out_for_delivery, delivered, cancelled
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

// OrderItem represents an item in an order
type OrderItem struct {
    ID          string  `gorm:"primaryKey"`
    OrderID     string  `gorm:"index;not null"`
    MenuItemID  string  `gorm:"index"`
    Name        string  `gorm:"not null"`
    Quantity    int     `gorm:"not null"`
    UnitPrice   float64 `gorm:"not null"`
    Subtotal    float64 `gorm:"not null"`
    SpecialInstructions string
    CreatedAt   time.Time
}

// FoodServer handles gRPC requests
type FoodServer struct {
    pb.UnimplementedFoodServiceServer
    DB *gorm.DB
}

// ListRestaurants returns restaurants near a location
func (s *FoodServer) ListRestaurants(ctx context.Context, req *pb.ListRestaurantsRequest) (*pb.ListRestaurantsResponse, error) {
    var restaurants []Restaurant
    query := s.DB.Where("is_active = ?", true)

    if req.Latitude != 0 && req.Longitude != 0 {
        // In production, use PostGIS for distance-based sorting
        query = query.Order("latitude")
    }

    if req.Query != "" {
        query = query.Where("name ILIKE ?", "%"+req.Query+"%")
    }

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&restaurants).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list restaurants")
    }

    var total int64
    s.DB.Model(&Restaurant{}).Where("is_active = ?", true).Count(&total)

    var pbRestaurants []*pb.Restaurant
    for _, r := range restaurants {
        pbRestaurants = append(pbRestaurants, &pb.Restaurant{
            Id:          r.ID,
            Name:        r.Name,
            Description: r.Description,
            Address:     r.Address,
            Latitude:    r.Latitude,
            Longitude:   r.Longitude,
            Phone:       r.Phone,
            ImageUrl:    r.ImageURL,
            Rating:      r.Rating,
            MinOrder:    r.MinOrder,
            DeliveryFee: r.DeliveryFee,
        })
    }

    return &pb.ListRestaurantsResponse{
        Restaurants: pbRestaurants,
        Total:       int32(total),
    }, nil
}

// GetRestaurant returns restaurant details
func (s *FoodServer) GetRestaurant(ctx context.Context, req *pb.GetRestaurantRequest) (*pb.Restaurant, error) {
    var restaurant Restaurant
    if err := s.DB.Where("id = ? AND is_active = ?", req.Id, true).First(&restaurant).Error; err != nil {
        return nil, status.Error(codes.NotFound, "restaurant not found")
    }

    return &pb.Restaurant{
        Id:          restaurant.ID,
        Name:        restaurant.Name,
        Description: restaurant.Description,
        Address:     restaurant.Address,
        Latitude:    restaurant.Latitude,
        Longitude:   restaurant.Longitude,
        Phone:       restaurant.Phone,
        ImageUrl:    restaurant.ImageURL,
        Rating:      restaurant.Rating,
        MinOrder:    restaurant.MinOrder,
        DeliveryFee: restaurant.DeliveryFee,
    }, nil
}

// GetMenu returns menu items for a restaurant
func (s *FoodServer) GetMenu(ctx context.Context, req *pb.GetMenuRequest) (*pb.GetMenuResponse, error) {
    var items []MenuItem
    query := s.DB.Where("restaurant_id = ? AND is_available = ?", req.RestaurantId, true)

    if req.CategoryId != "" {
        query = query.Where("category_id = ?", req.CategoryId)
    }

    if err := query.Find(&items).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get menu")
    }

    var pbItems []*pb.MenuItem
    for _, i := range items {
        pbItems = append(pbItems, &pb.MenuItem{
            Id:              i.ID,
            Name:            i.Name,
            Description:     i.Description,
            Price:           i.Price,
            DiscountPrice:   i.DiscountPrice,
            ImageUrl:        i.ImageURL,
            IsAvailable:     i.IsAvailable,
            PreparationTime: int32(i.PreparationTime),
        })
    }

    return &pb.GetMenuResponse{Items: pbItems}, nil
}

// PlaceOrder creates a new food order
func (s *FoodServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.OrderResponse, error) {
    // Verify restaurant exists
    var restaurant Restaurant
    if err := s.DB.Where("id = ?", req.RestaurantId).First(&restaurant).Error; err != nil {
        return nil, status.Error(codes.NotFound, "restaurant not found")
    }

    // Calculate totals
    var subtotal float64
    var orderItems []OrderItem

    for _, item := range req.Items {
        var menuItem MenuItem
        if err := s.DB.Where("id = ?", item.MenuItemId).First(&menuItem).Error; err != nil {
            return nil, status.Error(codes.NotFound, "menu item not found")
        }

        itemTotal := menuItem.Price * float64(item.Quantity)
        subtotal += itemTotal

        orderItems = append(orderItems, OrderItem{
            ID:          generateID(),
            MenuItemID:  menuItem.ID,
            Name:        menuItem.Name,
            Quantity:    int(item.Quantity),
            UnitPrice:   menuItem.Price,
            Subtotal:    itemTotal,
            CreatedAt:   time.Now(),
        })
    }

    tax := subtotal * 0.20 // 20% VAT
    total := subtotal + restaurant.DeliveryFee + tax

    order := &Order{
        ID:               generateID(),
        OrderNumber:      generateOrderNumber(),
        UserID:           req.UserId,
        RestaurantID:     req.RestaurantId,
        Status:           "pending",
        Subtotal:         subtotal,
        DeliveryFee:      restaurant.DeliveryFee,
        Tax:              tax,
        Total:            total,
        PaymentMethod:    req.PaymentMethod,
        DeliveryAddress:  req.DeliveryAddress,
        DeliveryLat:      req.DeliveryLat,
        DeliveryLng:      req.DeliveryLng,
        DeliveryInstructions: req.DeliveryInstructions,
        EstimatedTime:    45,
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
func (s *FoodServer) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.OrderDetailResponse, error) {
    var order Order
    if err := s.DB.Where("id = ?", req.OrderId).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    var items []OrderItem
    s.DB.Where("order_id = ?", order.ID).Find(&items)

    var pbItems []*pb.OrderItem
    for _, i := range items {
        pbItems = append(pbItems, &pb.OrderItem{
            Id:          i.ID,
            Name:        i.Name,
            Quantity:    int32(i.Quantity),
            UnitPrice:   i.UnitPrice,
            Subtotal:    i.Subtotal,
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
func (s *FoodServer) UpdateOrderStatus(ctx context.Context, req *pb.UpdateOrderStatusRequest) (*pb.Empty, error) {
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
func (s *FoodServer) ListUserOrders(ctx context.Context, req *pb.ListUserOrdersRequest) (*pb.ListOrdersResponse, error) {
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

func generateID() string {
    return "food_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateOrderNumber() string {
    return "ORD" + time.Now().Format("20060102") + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=fooddb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Restaurant{}, &Category{}, &MenuItem{}, &Order{}, &OrderItem{})

    grpcServer := grpc.NewServer()
    pb.RegisterFoodServiceServer(grpcServer, &FoodServer{DB: db})

    lis, err := net.Listen("tcp", ":50056")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ Food Service running on port 50056")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
    log.Println("Food Service stopped")
}