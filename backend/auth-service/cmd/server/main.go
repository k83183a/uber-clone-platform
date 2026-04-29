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
