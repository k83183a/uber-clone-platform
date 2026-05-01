package main

import (
    "context"
    "encoding/json"
    "log"
    "net"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "sync"
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

// ============================================================
// MODELS
// ============================================================

// Restaurant represents a food establishment
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
    CoverImageURL string
    CuisineType string    `gorm:"index"` // Italian, Chinese, Indian, etc.
    Rating      float64   `gorm:"default:0"`
    RatingCount int       `gorm:"default:0"`
    MinOrder    float64   `gorm:"default:0"`
    DeliveryFee float64   `gorm:"default:0"`
    DeliveryTimeMin int   `gorm:"default:30"`
    IsActive    bool      `gorm:"default:true"`
    IsOpen      bool      `gorm:"default:true"`
    OpeningHours string   `gorm:"type:text"` // JSON schedule
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// Category represents a menu category (Appetizers, Mains, Desserts)
type Category struct {
    ID          string `gorm:"primaryKey"`
    RestaurantID string `gorm:"index;not null"`
    Name        string `gorm:"not null"`
    Description string
    SortOrder   int    `gorm:"default:0"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// MenuItem represents an item on the menu
type MenuItem struct {
    ID             string    `gorm:"primaryKey"`
    RestaurantID   string    `gorm:"index;not null"`
    CategoryID     string    `gorm:"index"`
    Name           string    `gorm:"not null"`
    Description    string
    Price          float64   `gorm:"not null"`
    DiscountPrice  float64
    ImageURL       string
    PreparationTime int      `gorm:"default:15"` // minutes
    IsAvailable    bool      `gorm:"default:true"`
    IsVegetarian   bool      `gorm:"default:false"`
    IsVegan        bool      `gorm:"default:false"`
    IsSpicy        bool      `gorm:"default:false"`
    Calories       int
    Popularity     int       `gorm:"default:0"` // number of times ordered
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

// Order represents a food order
type Order struct {
    ID                   string     `gorm:"primaryKey"`
    OrderNumber          string     `gorm:"uniqueIndex"`
    UserID               string     `gorm:"index;not null"`
    RestaurantID         string     `gorm:"index;not null"`
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
    EstimatedTime        int        // minutes
    ActualTime           int
    CreatedAt            time.Time
    ConfirmedAt          *time.Time
    PreparingAt          *time.Time
    ReadyAt              *time.Time
    PickedUpAt           *time.Time
    DeliveredAt          *time.Time
    CancelledAt          *time.Time
    CancelledReason      string
    Rating               int        `default:0`
    RatingComment        string
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

// ============================================================
// GRPC SERVER
// ============================================================

type FoodServer struct {
    pb.UnimplementedFoodServiceServer
    DB *gorm.DB
}

// ============================================================
// RESTAURANT METHODS
// ============================================================

// ListRestaurants - Get restaurants near a location with filters
func (s *FoodServer) ListRestaurants(ctx context.Context, req *pb.ListRestaurantsRequest) (*pb.ListRestaurantsResponse, error) {
    var restaurants []Restaurant
    query := s.DB.Where("is_active = ? AND is_open = ?", true, true)

    if req.Cuisine != "" {
        query = query.Where("cuisine_type = ?", req.Cuisine)
    }

    if req.Query != "" {
        query = query.Where("name ILIKE ? OR description ILIKE ?", "%"+req.Query+"%", "%"+req.Query+"%")
    }

    // Sort by distance (simplified - use PostGIS in production)
    if req.Latitude != 0 && req.Longitude != 0 {
        query = query.Order("ABS(latitude - ?) + ABS(longitude - ?)", req.Latitude, req.Longitude)
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
            Id:           r.ID,
            Name:         r.Name,
            Description:  r.Description,
            Address:      r.Address,
            Latitude:     r.Latitude,
            Longitude:    r.Longitude,
            Phone:        r.Phone,
            ImageUrl:     r.ImageURL,
            CuisineType:  r.CuisineType,
            Rating:       r.Rating,
            MinOrder:     r.MinOrder,
            DeliveryFee:  r.DeliveryFee,
            DeliveryTime: int32(r.DeliveryTimeMin),
        })
    }

    return &pb.ListRestaurantsResponse{
        Restaurants: pbRestaurants,
        Total:       int32(total),
    }, nil
}

// GetRestaurant - Get restaurant details
func (s *FoodServer) GetRestaurant(ctx context.Context, req *pb.GetRestaurantRequest) (*pb.RestaurantDetail, error) {
    var restaurant Restaurant
    if err := s.DB.Where("id = ? AND is_active = ?", req.Id, true).First(&restaurant).Error; err != nil {
        return nil, status.Error(codes.NotFound, "restaurant not found")
    }

    return &pb.RestaurantDetail{
        Id:            restaurant.ID,
        Name:          restaurant.Name,
        Description:   restaurant.Description,
        Address:       restaurant.Address,
        Latitude:      restaurant.Latitude,
        Longitude:     restaurant.Longitude,
        Phone:         restaurant.Phone,
        ImageUrl:      restaurant.ImageURL,
        CoverImageUrl: restaurant.CoverImageURL,
        CuisineType:   restaurant.CuisineType,
        Rating:        restaurant.Rating,
        RatingCount:   int32(restaurant.RatingCount),
        MinOrder:      restaurant.MinOrder,
        DeliveryFee:   restaurant.DeliveryFee,
        DeliveryTime:  int32(restaurant.DeliveryTimeMin),
        IsOpen:        restaurant.IsOpen,
    }, nil
}

// ============================================================
// MENU METHODS
// ============================================================

// GetCategories - Get menu categories for a restaurant
func (s *FoodServer) GetCategories(ctx context.Context, req *pb.GetCategoriesRequest) (*pb.CategoriesResponse, error) {
    var categories []Category
    if err := s.DB.Where("restaurant_id = ?", req.RestaurantId).Order("sort_order ASC").Find(&categories).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get categories")
    }

    var pbCategories []*pb.Category
    for _, c := range categories {
        pbCategories = append(pbCategories, &pb.Category{
            Id:          c.ID,
            Name:        c.Name,
            Description: c.Description,
        })
    }

    return &pb.CategoriesResponse{Categories: pbCategories}, nil
}

// GetMenu - Get menu items for a restaurant (with optional category filter)
func (s *FoodServer) GetMenu(ctx context.Context, req *pb.GetMenuRequest) (*pb.MenuResponse, error) {
    var items []MenuItem
    query := s.DB.Where("restaurant_id = ? AND is_available = ?", req.RestaurantId, true)

    if req.CategoryId != "" {
        query = query.Where("category_id = ?", req.CategoryId)
    }

    if err := query.Order("popularity DESC, name ASC").Find(&items).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get menu")
    }

    var pbItems []*pb.MenuItem
    for _, i := range items {
        pbItems = append(pbItems, &pb.MenuItem{
            Id:            i.ID,
            Name:          i.Name,
            Description:   i.Description,
            Price:         i.Price,
            DiscountPrice: i.DiscountPrice,
            ImageUrl:      i.ImageURL,
            IsVegetarian:  i.IsVegetarian,
            IsVegan:       i.IsVegan,
            IsSpicy:       i.IsSpicy,
            PreparationTime: int32(i.PreparationTime),
        })
    }

    return &pb.MenuResponse{Items: pbItems}, nil
}

// ============================================================
// ORDER METHODS
// ============================================================

// PlaceOrder - Create a new food order
func (s *FoodServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.OrderResponse, error) {
    // Verify restaurant exists and is open
    var restaurant Restaurant
    if err := s.DB.Where("id = ? AND is_active = ?", req.RestaurantId, true).First(&restaurant).Error; err != nil {
        return nil, status.Error(codes.NotFound, "restaurant not found")
    }

    if !restaurant.IsOpen {
        return nil, status.Error(codes.FailedPrecondition, "restaurant is currently closed")
    }

    // Validate items and calculate totals
    var subtotal float64
    var orderItems []OrderItem
    var maxPrepTime int

    for _, item := range req.Items {
        var menuItem MenuItem
        if err := s.DB.Where("id = ? AND restaurant_id = ?", item.MenuItemId, req.RestaurantId).First(&menuItem).Error; err != nil {
            return nil, status.Error(codes.NotFound, "menu item not found")
        }

        if !menuItem.IsAvailable {
            return nil, status.Error(codes.FailedPrecondition, "item not available: "+menuItem.Name)
        }

        price := menuItem.Price
        if menuItem.DiscountPrice > 0 && menuItem.DiscountPrice < price {
            price = menuItem.DiscountPrice
        }

        itemTotal := price * float64(item.Quantity)
        subtotal += itemTotal

        if menuItem.PreparationTime > maxPrepTime {
            maxPrepTime = menuItem.PreparationTime
        }

        orderItems = append(orderItems, OrderItem{
            ID:          generateID(),
            MenuItemID:  menuItem.ID,
            Name:        menuItem.Name,
            Quantity:    int(item.Quantity),
            UnitPrice:   price,
            Subtotal:    itemTotal,
            SpecialInstructions: item.SpecialInstructions,
            CreatedAt:   time.Now(),
        })
    }

    // Apply discount if promo code provided
    discount := 0.0
    if req.PromoCode != "" {
        discount = s.calculateDiscount(req.PromoCode, subtotal)
    }

    serviceFee := subtotal * 0.05 // 5% service fee
    tax := (subtotal - discount) * 0.20 // 20% VAT
    total := subtotal - discount + restaurant.DeliveryFee + serviceFee + tax

    order := &Order{
        ID:                   generateID(),
        OrderNumber:          generateOrderNumber(),
        UserID:               req.UserId,
        RestaurantID:         req.RestaurantId,
        Status:               "pending",
        Subtotal:             subtotal,
        DeliveryFee:          restaurant.DeliveryFee,
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
        EstimatedTime:        restaurant.DeliveryTimeMin + maxPrepTime,
        CreatedAt:            time.Now(),
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
            // Update menu item popularity
            tx.Model(&MenuItem{}).Where("id = ?", item.MenuItemID).Update("popularity", gorm.Expr("popularity + ?", item.Quantity))
        }
        return nil
    })

    if err != nil {
        return nil, status.Error(codes.Internal, "failed to place order")
    }

    log.Printf("🍔 New order %s: £%.2f from %s", order.OrderNumber, order.Total, restaurant.Name)

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
func (s *FoodServer) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.OrderDetailResponse, error) {
    var order Order
    if err := s.DB.Where("id = ? OR order_number = ?", req.OrderId, req.OrderNumber).First(&order).Error; err != nil {
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
        ServiceFee:  order.ServiceFee,
        Tax:         order.Tax,
        Discount:    order.Discount,
        Total:       order.Total,
        Items:       pbItems,
        DeliveryAddress: order.DeliveryAddress,
        EstimatedTime: int32(order.EstimatedTime),
        CreatedAt:   order.CreatedAt.Unix(),
    }, nil
}

// UpdateOrderStatus - Update order status (restaurant/driver side)
func (s *FoodServer) UpdateOrderStatus(ctx context.Context, req *pb.UpdateOrderStatusRequest) (*pb.Empty, error) {
    var order Order
    if err := s.DB.Where("id = ?", req.OrderId).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    now := time.Now()
    updates := map[string]interface{}{
        "status":     req.Status,
    }

    switch req.Status {
    case "confirmed":
        updates["confirmed_at"] = now
    case "preparing":
        updates["preparing_at"] = now
    case "ready":
        updates["ready_at"] = now
    case "out_for_delivery":
        updates["picked_up_at"] = now
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

    log.Printf("📦 Order %s status updated to: %s", order.OrderNumber, req.Status)

    return &pb.Empty{}, nil
}

// CancelOrder - Cancel an order (user side)
func (s *FoodServer) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.Empty, error) {
    var order Order
    if err := s.DB.Where("id = ? AND user_id = ?", req.OrderId, req.UserId).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    if order.Status != "pending" && order.Status != "confirmed" {
        return nil, status.Error(codes.FailedPrecondition, "order cannot be cancelled")
    }

    now := time.Now()
    order.Status = "cancelled"
    order.CancelledAt = &now
    order.CancelledReason = req.Reason

    if err := s.DB.Save(&order).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to cancel order")
    }

    return &pb.Empty{}, nil
}

// RateOrder - Rate an order after delivery
func (s *FoodServer) RateOrder(ctx context.Context, req *pb.RateOrderRequest) (*pb.Empty, error) {
    var order Order
    if err := s.DB.Where("id = ? AND user_id = ?", req.OrderId, req.UserId).First(&order).Error; err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    if order.Status != "delivered" {
        return nil, status.Error(codes.FailedPrecondition, "order not delivered yet")
    }

    order.Rating = int(req.Rating)
    order.RatingComment = req.Comment

    // Update restaurant rating
    var restaurant Restaurant
    s.DB.Where("id = ?", order.RestaurantID).First(&restaurant)
    newRating := (restaurant.Rating*float64(restaurant.RatingCount) + float64(req.Rating)) / float64(restaurant.RatingCount+1)
    restaurant.Rating = newRating
    restaurant.RatingCount++
    s.DB.Save(&restaurant)

    if err := s.DB.Save(&order).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to rate order")
    }

    return &pb.Empty{}, nil
}

// ListUserOrders - List orders for a user
func (s *FoodServer) ListUserOrders(ctx context.Context, req *pb.ListUserOrdersRequest) (*pb.ListOrdersResponse, error) {
    var orders []Order
    query := s.DB.Where("user_id = ?", req.UserId).Order("created_at DESC")

    offset := (req.Page - 1) * req.PageSize
    if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&orders).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list orders")
    }

    var total int64
    s.DB.Model(&Order{}).Where("user_id = ?", req.UserId).Count(&total)

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

// ============================================================
// HELPER FUNCTIONS
// ============================================================

func (s *FoodServer) calculateDiscount(promoCode string, subtotal float64) float64 {
    // In production: query promotions-service
    // For MVP: simple discount codes
    switch strings.ToUpper(promoCode) {
    case "WELCOME20":
        return subtotal * 0.20
    case "FREE10":
        if subtotal >= 15 {
            return 10.0
        }
        return 0
    default:
        return 0
    }
}

func generateID() string {
    return "food_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func generateOrderNumber() string {
    return "FOOD" + time.Now().Format("20060102") + randomString(6)
}

func randomString(n int) string {
    const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    b := make([]byte, n)
    for i := range b {
        b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
    }
    return string(b)
}

// ============================================================
// MAIN
// ============================================================

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

    // Seed sample data if empty
    var count int64
    db.Model(&Restaurant{}).Count(&count)
    if count == 0 {
        seedSampleData(db)
    }

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

func seedSampleData(db *gorm.DB) {
    // Sample restaurant
    restaurant := Restaurant{
        ID:          generateID(),
        Name:        "The Burger Joint",
        Description: "Premium burgers made with 100% British beef",
        Address:     "123 High Street, London",
        Latitude:    51.5074,
        Longitude:   -0.1278,
        Phone:       "+442012345678",
        CuisineType: "American",
        MinOrder:    10.0,
        DeliveryFee: 1.99,
        DeliveryTimeMin: 25,
        IsActive:    true,
        IsOpen:      true,
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
    }
    db.Create(&restaurant)

    // Sample categories
    categories := []Category{
        {ID: generateID(), RestaurantID: restaurant.ID, Name: "Burgers", SortOrder: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), RestaurantID: restaurant.ID, Name: "Sides", SortOrder: 2, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), RestaurantID: restaurant.ID, Name: "Drinks", SortOrder: 3, CreatedAt: time.Now(), UpdatedAt: time.Now()},
    }
    db.Create(&categories)

    // Sample menu items
    menuItems := []MenuItem{
        {ID: generateID(), RestaurantID: restaurant.ID, CategoryID: categories[0].ID, Name: "Classic Cheeseburger", Description: "Beef patty, cheddar cheese, lettuce, tomato", Price: 9.50, PreparationTime: 10, IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), RestaurantID: restaurant.ID, CategoryID: categories[0].ID, Name: "Double Bacon Burger", Description: "Two patties, bacon, cheese, BBQ sauce", Price: 12.50, PreparationTime: 12, IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), RestaurantID: restaurant.ID, CategoryID: categories[0].ID, Name: "Vegan Burger", Description: "Plant-based patty, vegan cheese", Price: 10.50, IsVegetarian: true, IsVegan: true, PreparationTime: 10, IsAvailable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), RestaurantID: restaurant.ID, CategoryID: categories[1].ID, Name: "French Fries", Description: "Crispy golden fries", Price: 3.50, PreparationTime: 5, IsAvailable: true, IsVegetarian: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), RestaurantID: restaurant.ID, CategoryID: categories[1].ID, Name: "Onion Rings", Description: "Crispy battered onion rings", Price: 4.00, PreparationTime: 5, IsVegetarian: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
        {ID: generateID(), RestaurantID: restaurant.ID, CategoryID: categories[2].ID, Name: "Coca Cola", Description: "Refreshing cola", Price: 2.00, PreparationTime: 1, IsAvailable: true, IsVegetarian: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
    }
    db.Create(&menuItems)

    log.Println("✅ Seeded sample restaurant and menu data")
}