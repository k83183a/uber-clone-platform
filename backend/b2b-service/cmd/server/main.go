package main

import (
    "context"
    "encoding/json"
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

    pb "github.com/uber-clone/b2b-service/proto"
)

type Company struct {
    ID             string    `gorm:"primaryKey"`
    Name           string    `gorm:"not null"`
    TaxID          string    `gorm:"uniqueIndex"`
    Email          string
    Phone          string
    Address        string
    BillingAddress string
    PaymentMethod  string    `gorm:"default:'invoice'"`
    WalletBalance  float64   `gorm:"default:0"`
    CreditLimit    float64   `gorm:"default:0"`
    Status         string    `gorm:"default:'active'"`
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type Employee struct {
    ID           string    `gorm:"primaryKey"`
    CompanyID    string    `gorm:"index;not null"`
    UserID       string    `gorm:"uniqueIndex;not null"`
    Role         string    `gorm:"default:'employee'"`
    Department   string
    CostCenter   string
    MonthlyBudget float64  `gorm:"default:0"`
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type ApprovalRequest struct {
    ID             string     `gorm:"primaryKey"`
    CompanyID      string     `gorm:"index;not null"`
    EmployeeID     string     `gorm:"index;not null"`
    ServiceType    string     `gorm:"not null"`
    ServiceRequest string     `gorm:"type:text"`
    EstimatedCost  float64    `gorm:"not null"`
    Status         string     `gorm:"default:'pending'"`
    RequestedAt    time.Time
    ApprovedAt     *time.Time
    ApprovedBy     string
    DeniedReason   string
}

type B2BTransaction struct {
    ID          string    `gorm:"primaryKey"`
    CompanyID   string    `gorm:"index;not null"`
    EmployeeID  string    `gorm:"index"`
    ServiceType string    `gorm:"not null"`
    ServiceID   string    `gorm:"index;not null"`
    Amount      float64   `gorm:"not null"`
    Status      string    `gorm:"default:'pending'"`
    CreatedAt   time.Time
    SettledAt   *time.Time
}

type B2BServer struct {
    pb.UnimplementedB2BServiceServer
    DB *gorm.DB
}

// RegisterCompany - Create a new corporate account
func (s *B2BServer) RegisterCompany(ctx context.Context, req *pb.RegisterCompanyRequest) (*pb.CompanyResponse, error) {
    company := &Company{
        ID:             generateID(),
        Name:           req.Name,
        TaxID:          req.TaxId,
        Email:          req.Email,
        Phone:          req.Phone,
        Address:        req.Address,
        BillingAddress: req.BillingAddress,
        PaymentMethod:  req.PaymentMethod,
        WalletBalance:  0,
        CreditLimit:    req.CreditLimit,
        Status:         "active",
        CreatedAt:      time.Now(),
        UpdatedAt:      time.Now(),
    }
    if err := s.DB.Create(company).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to register company")
    }

    return &pb.CompanyResponse{
        Id:            company.ID,
        Name:          company.Name,
        Status:        company.Status,
        WalletBalance: company.WalletBalance,
    }, nil
}

// AddEmployee - Add an employee to a company
func (s *B2BServer) AddEmployee(ctx context.Context, req *pb.AddEmployeeRequest) (*pb.EmployeeResponse, error) {
    var existing Employee
    if err := s.DB.Where("user_id = ?", req.UserId).First(&existing).Error; err == nil {
        return nil, status.Error(codes.AlreadyExists, "employee already added")
    }

    employee := &Employee{
        ID:            generateID(),
        CompanyID:     req.CompanyId,
        UserID:        req.UserId,
        Role:          req.Role,
        Department:    req.Department,
        CostCenter:    req.CostCenter,
        MonthlyBudget: req.MonthlyBudget,
        CreatedAt:     time.Now(),
        UpdatedAt:     time.Now(),
    }
    if err := s.DB.Create(employee).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to add employee")
    }

    return &pb.EmployeeResponse{
        Id:            employee.ID,
        UserId:        employee.UserID,
        Role:          employee.Role,
        MonthlyBudget: employee.MonthlyBudget,
    }, nil
}

// ListEmployees - List all employees of a company
func (s *B2BServer) ListEmployees(ctx context.Context, req *pb.ListEmployeesRequest) (*pb.ListEmployeesResponse, error) {
    var employees []Employee
    if err := s.DB.Where("company_id = ?", req.CompanyId).Find(&employees).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to list employees")
    }

    var pbEmployees []*pb.EmployeeResponse
    for _, e := range employees {
        pbEmployees = append(pbEmployees, &pb.EmployeeResponse{
            Id:            e.ID,
            UserId:        e.UserID,
            Role:          e.Role,
            MonthlyBudget: e.MonthlyBudget,
        })
    }

    return &pb.ListEmployeesResponse{Employees: pbEmployees}, nil
}

// RequestApproval - Create an approval request for a service
func (s *B2BServer) RequestApproval(ctx context.Context, req *pb.RequestApprovalRequest) (*pb.ApprovalResponse, error) {
    approval := &ApprovalRequest{
        ID:             generateID(),
        CompanyID:      req.CompanyId,
        EmployeeID:     req.EmployeeId,
        ServiceType:    req.ServiceType,
        ServiceRequest: req.ServiceRequestJson,
        EstimatedCost:  req.EstimatedCost,
        Status:         "pending",
        RequestedAt:    time.Now(),
    }
    if err := s.DB.Create(approval).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create approval request")
    }

    return &pb.ApprovalResponse{
        ApprovalId: approval.ID,
        Status:     approval.Status,
    }, nil
}

// ApproveRequest - Approve a pending request
func (s *B2BServer) ApproveRequest(ctx context.Context, req *pb.ApproveRequestRequest) (*pb.Empty, error) {
    var approval ApprovalRequest
    if err := s.DB.Where("id = ?", req.ApprovalId).First(&approval).Error; err != nil {
        return nil, status.Error(codes.NotFound, "approval request not found")
    }
    if approval.Status != "pending" {
        return nil, status.Error(codes.FailedPrecondition, "request already processed")
    }

    now := time.Now()
    approval.Status = "approved"
    approval.ApprovedAt = &now
    approval.ApprovedBy = req.ApprovedBy
    s.DB.Save(&approval)

    return &pb.Empty{}, nil
}

// DenyRequest - Deny a pending request
func (s *B2BServer) DenyRequest(ctx context.Context, req *pb.DenyRequestRequest) (*pb.Empty, error) {
    var approval ApprovalRequest
    if err := s.DB.Where("id = ?", req.ApprovalId).First(&approval).Error; err != nil {
        return nil, status.Error(codes.NotFound, "approval request not found")
    }
    if approval.Status != "pending" {
        return nil, status.Error(codes.FailedPrecondition, "request already processed")
    }

    approval.Status = "denied"
    approval.DeniedReason = req.Reason
    s.DB.Save(&approval)

    return &pb.Empty{}, nil
}

// AddWalletFunds - Add funds to company wallet
func (s *B2BServer) AddWalletFunds(ctx context.Context, req *pb.AddFundsRequest) (*pb.BalanceResponse, error) {
    var company Company
    if err := s.DB.Where("id = ?", req.CompanyId).First(&company).Error; err != nil {
        return nil, status.Error(codes.NotFound, "company not found")
    }

    company.WalletBalance += req.Amount
    company.UpdatedAt = time.Now()
    s.DB.Save(&company)

    return &pb.BalanceResponse{Balance: company.WalletBalance}, nil
}

// GetWalletBalance - Get company wallet balance
func (s *B2BServer) GetWalletBalance(ctx context.Context, req *pb.GetBalanceRequest) (*pb.BalanceResponse, error) {
    var company Company
    if err := s.DB.Where("id = ?", req.CompanyId).First(&company).Error; err != nil {
        return nil, status.Error(codes.NotFound, "company not found")
    }

    return &pb.BalanceResponse{Balance: company.WalletBalance}, nil
}

// CreateB2BRide - Create a ride using company budget
func (s *B2BServer) CreateB2BRide(ctx context.Context, req *pb.B2BRideRequest) (*pb.B2BRideResponse, error) {
    var employee Employee
    if err := s.DB.Where("id = ?", req.EmployeeId).First(&employee).Error; err != nil {
        return nil, status.Error(codes.NotFound, "employee not found")
    }

    var company Company
    if err := s.DB.Where("id = ?", employee.CompanyID).First(&company).Error; err != nil {
        return nil, status.Error(codes.NotFound, "company not found")
    }

    if company.PaymentMethod == "wallet" && company.WalletBalance < req.EstimatedCost {
        return nil, status.Error(codes.ResourceExhausted, "insufficient wallet balance")
    }

    rideID := "ride_" + time.Now().Format("20060102150405")

    tx := &B2BTransaction{
        ID:          generateID(),
        CompanyID:   employee.CompanyID,
        EmployeeID:  employee.ID,
        ServiceType: "ride",
        ServiceID:   rideID,
        Amount:      req.EstimatedCost,
        Status:      "pending",
        CreatedAt:   time.Now(),
    }
    s.DB.Create(tx)

    if company.PaymentMethod == "wallet" {
        company.WalletBalance -= req.EstimatedCost
        s.DB.Save(&company)
        tx.Status = "settled"
        now := time.Now()
        tx.SettledAt = &now
        s.DB.Save(tx)
    }

    return &pb.B2BRideResponse{
        RideId: rideID,
        Status: "confirmed",
        Fare:   req.EstimatedCost,
    }, nil
}

// GetCompanyTransactions - List all transactions for a company
func (s *B2BServer) GetCompanyTransactions(ctx context.Context, req *pb.GetTransactionsRequest) (*pb.TransactionsList, error) {
    var transactions []B2BTransaction
    query := s.DB.Where("company_id = ?", req.CompanyId).Order("created_at DESC")

    if req.FromDate != 0 {
        from := time.Unix(req.FromDate, 0)
        query = query.Where("created_at >= ?", from)
    }
    if req.ToDate != 0 {
        to := time.Unix(req.ToDate, 0)
        query = query.Where("created_at <= ?", to)
    }

    if err := query.Find(&transactions).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get transactions")
    }

    var pbTransactions []*pb.Transaction
    for _, t := range transactions {
        pbTransactions = append(pbTransactions, &pb.Transaction{
            Id:          t.ID,
            ServiceType: t.ServiceType,
            ServiceId:   t.ServiceID,
            Amount:      t.Amount,
            CreatedAt:   t.CreatedAt.Unix(),
        })
    }

    return &pb.TransactionsList{Transactions: pbTransactions}, nil
}

func generateID() string {
    return "b2b_" + time.Now().Format("20060102150405") + "_" + randomString(6)
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
        dsn = "host=postgres user=postgres password=postgres dbname=b2bdb port=5432 sslmode=disable"
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }

    db.AutoMigrate(&Company{}, &Employee{}, &ApprovalRequest{}, &B2BTransaction{})

    grpcServer := grpc.NewServer()
    pb.RegisterB2BServiceServer(grpcServer, &B2BServer{DB: db})

    lis, err := net.Listen("tcp", ":50069")
    if err != nil {
        log.Fatal("Failed to listen:", err)
    }

    go func() {
        log.Println("✅ B2B Service running on port 50069")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal("Failed to serve:", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    grpcServer.GracefulStop()
}