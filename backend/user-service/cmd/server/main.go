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
