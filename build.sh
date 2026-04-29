#!/bin/bash
set -e
echo "═══════════════════════════════════════════════════════════════"
echo "     UBER CLONE – FIRST 4 SERVICES (Auth, User, Ride, Location)"
echo "═══════════════════════════════════════════════════════════════"

# Create directory structure
mkdir -p backend/auth-service/cmd/server backend/auth-service/proto
mkdir -p backend/user-service/cmd/server backend/user-service/proto
mkdir -p backend/ride-service/cmd/server backend/ride-service/proto
mkdir -p backend/location-service/cmd/server
mkdir -p scripts

# AUTH SERVICE
cat > backend/auth-service/cmd/server/main.go << 'AUTH'
package main
import ("context";"log";"net";"os";"os/signal";"syscall";"time";"github.com/golang-jwt/jwt/v5";"github.com/joho/godotenv";"golang.org/x/crypto/bcrypt";"google.golang.org/grpc";"google.golang.org/grpc/codes";"google.golang.org/grpc/status";"gorm.io/driver/postgres";"gorm.io/gorm")
type User struct{ID,Email,Phone,Password,Role string;CreatedAt,UpdatedAt time.Time}
type AuthServer struct{*gorm.DB;[]byte}
func (s *AuthServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.AuthResponse,error){
        h,_:=bcrypt.GenerateFromPassword([]byte(req.Password),bcrypt.DefaultCost)
            u:=User{ID:"user_"+time.Now().Format("20060102150405"),Email:req.Email,Phone:req.Phone,Password:string(h),Role:req.Role,CreatedAt:time.Now(),UpdatedAt:time.Now()}
                s.DB.Create(&u)
                    t,_:=jwt.NewWithClaims(jwt.SigningMethodHS256,jwt.MapClaims{"user_id":u.ID,"role":u.Role,"exp":time.Now().Add(24*time.Hour).Unix()}).SignedString(s.JWTSecret)
                        return &pb.AuthResponse{Token:t,UserId:u.ID,Email:u.Email,Role:u.Role,ExpiresIn:86400},nil
}
func main(){godotenv.Load();dsn:=os.Getenv("DB_DSN");if dsn==""{dsn="host=postgres user=postgres password=postgres dbname=authdb port=5432 sslmode=disable"}
db,_:=gorm.Open(postgres.Open(dsn),&gorm.Config{});db.AutoMigrate(&User{})
s:=grpc.NewServer();pb.RegisterAuthServiceServer(s,&AuthServer{DB:db,JWTSecret:[]byte("secret")})
l,_:=net.Listen("tcp",":50051");go s.Serve(l);log.Println("✅ Auth on :50051")
q:=make(chan os.Signal,1);signal.Notify(q,syscall.SIGINT,syscall.SIGTERM);<-q;s.GracefulStop()}
AUTH
cat > backend/auth-service/proto/auth.proto << 'AUTHPROTO'
syntax="proto3";package auth;service AuthService{rpc Register(RegisterRequest)returns(AuthResponse);}message RegisterRequest{string email=1;string phone=2;string password=3;string role=4;}message AuthResponse{string token=1;string user_id=2;string email=3;string role=4;int64 expires_in=5;}
AUTHPROTO

# USER SERVICE
cat > backend/user-service/cmd/server/main.go << 'USER'
package main
import ("context";"log";"net";"os";"os/signal";"syscall";"time";"github.com/joho/godotenv";"google.golang.org/grpc";"google.golang.org/grpc/codes";"google.golang.org/grpc/status";"gorm.io/driver/postgres";"gorm.io/gorm")
type Profile struct{ID,UserID,Email,FullName,Phone,AvatarURL,Role string;CreatedAt,UpdatedAt time.Time}
type UserServer struct{*gorm.DB}
func (s *UserServer) CreateProfile(ctx context.Context, req *pb.CreateProfileRequest)(*pb.UserResponse,error){
        p:=Profile{ID:"prof_"+time.Now().Format("20060102150405"),UserID:req.UserId,Email:req.Email,FullName:req.FullName,Phone:req.Phone,AvatarURL:req.AvatarUrl,Role:req.Role,CreatedAt:time.Now(),UpdatedAt:time.Now()}
            s.DB.Create(&p)
                return &pb.UserResponse{Id:p.ID,UserId:p.UserID,Email:p.Email,FullName:p.FullName,Phone:p.Phone,Role:p.Role},nil
}
func main(){godotenv.Load();dsn:=os.Getenv("DB_DSN");if dsn==""{dsn="host=postgres user=postgres password=postgres dbname=userdb port=5432 sslmode=disable"}
db,_:=gorm.Open(postgres.Open(dsn),&gorm.Config{});db.AutoMigrate(&Profile{})
s:=grpc.NewServer();pb.RegisterUserServiceServer(s,&UserServer{DB:db})
l,_:=net.Listen("tcp",":50052");go s.Serve(l);log.Println("✅ User on :50052")
q:=make(chan os.Signal,1);signal.Notify(q,syscall.SIGINT,syscall.SIGTERM);<-q;s.GracefulStop()}
USER
cat > backend/user-service/proto/user.proto << 'USERPROTO'
syntax="proto3";package user;service UserService{rpc CreateProfile(CreateProfileRequest)returns(UserResponse);}message CreateProfileRequest{string user_id=1;string email=2;string full_name=3;string phone=4;string avatar_url=5;string role=6;}message UserResponse{string id=1;string user_id=2;string email=3;string full_name=4;string phone=5;string role=6;}
USERPROTO

# RIDE SERVICE
cat > backend/ride-service/cmd/server/main.go << 'RIDE'
package main
import ("context";"log";"net";"os";"os/signal";"syscall";"time";"github.com/joho/godotenv";"google.golang.org/grpc";"google.golang.org/grpc/codes";"google.golang.org/grpc/status";"gorm.io/driver/postgres";"gorm.io/gorm")
type Ride struct{ID,RiderID,DriverID,Status,RideType string;PickupLat,PickupLng,DropoffLat,DropoffLng,Fare float64;CreatedAt time.Time}
type RideServer struct{*gorm.DB}
func (s *RideServer) RequestRide(ctx context.Context, req *pb.RequestRideRequest)(*pb.RideResponse,error){
        r:=Ride{ID:"ride_"+time.Now().Format("20060102150405"),RiderID:req.RiderId,Status:"pending",PickupLat:req.PickupLat,PickupLng:req.PickupLng,DropoffLat:req.DropoffLat,DropoffLng:req.DropoffLng,RideType:req.RideType,Fare:req.FareEstimate,CreatedAt:time.Now()}
            s.DB.Create(&r)
                return &pb.RideResponse{Id:r.ID,Status:r.Status,Fare:r.Fare},nil
}
func main(){godotenv.Load();dsn:=os.Getenv("DB_DSN");if dsn==""{dsn="host=postgres user=postgres password=postgres dbname=ridedb port=5432 sslmode=disable"}
db,_:=gorm.Open(postgres.Open(dsn),&gorm.Config{});db.AutoMigrate(&Ride{})
s:=grpc.NewServer();pb.RegisterRideServiceServer(s,&RideServer{DB:db})
l,_:=net.Listen("tcp",":50053");go s.Serve(l);log.Println("✅ Ride on :50053")
q:=make(chan os.Signal,1);signal.Notify(q,syscall.SIGINT,syscall.SIGTERM);<-q;s.GracefulStop()}
RIDE
cat > backend/ride-service/proto/ride.proto << 'RIDEPROTO'
syntax="proto3";package ride;service RideService{rpc RequestRide(RequestRideRequest)returns(RideResponse);}message RequestRideRequest{string rider_id=1;double pickup_lat=2;double pickup_lng=3;double dropoff_lat=4;double dropoff_lng=5;string ride_type=6;double fare_estimate=7;}message RideResponse{string id=1;string status=2;double fare=3;}
RIDEPROTO

# LOCATION SERVICE
cat > backend/location-service/cmd/server/main.go << 'LOCATION'
package main
import ("log";"net/http";"os";"os/signal";"syscall";"github.com/gorilla/websocket";"github.com/joho/godotenv")
var u=websocket.Upgrader{CheckOrigin:func(r*http.Request)bool{return true}}
func main(){godotenv.Load();http.HandleFunc("/ws/driver",func(w http.ResponseWriter,r*http.Request){c,_:=u.Upgrade(w,r,nil);defer c.Close();for{_,m,_:=c.ReadMessage();log.Printf("Driver: %s",m)}});http.HandleFunc("/ws/rider",func(w http.ResponseWriter,r*http.Request){c,_:=u.Upgrade(w,r,nil);defer c.Close();for{}})
go func(){log.Println("✅ Location on :8080");log.Fatal(http.ListenAndServe(":8080",nil))}()
q:=make(chan os.Signal,1);signal.Notify(q,syscall.SIGINT,syscall.SIGTERM);<-q}
LOCATION

# DOCKER COMPOSE
cat > docker-compose.yml << 'DOCKER'
version:'3.8'
services:postgres:{image:postgis/postgis:15-3.4,environment:{POSTGRES_USER:postgres,POSTGRES_PASSWORD:postgres},ports:["5432:5432"],volumes:[pgdata:/var/lib/postgresql/data]}
volumes:{pgdata:}
DOCKER

echo "✅ Code generated"