/*
* Alper Reha Yazgan - GoLang CRUD App
* 02.02.2022
go get -u github.com/gin-gonic/gin
go get github.com/joho/godotenv
go get github.com/aws/aws-sdk-go/aws
go get github.com/aws/aws-sdk-go/aws/credentials
go get github.com/aws/aws-sdk-go/aws/session
go get github.com/aws/aws-sdk-go/service/s3
go get github.com/go-playground/validator/v10
go get -u gorm.io/driver/postgres
go get gorm.io/gorm
go get github.com/go-redis/redis/v8
go get github.com/nats-io/nats.go
*/

package main

import (
	"bytes"
	"context"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"time"

	// golang gin framework
	"github.com/gin-gonic/gin"

	// dotenv
	"github.com/joho/godotenv"

	// minio dependencies
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	// validator packages
	"github.com/go-playground/validator/v10"

	// database packages
	// "gorm.io/driver/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	// Redis packages
	"github.com/go-redis/redis/v8"

	// NATS packages
	"github.com/nats-io/nats.go"
)

// App Pool Context
var ctxPool = context.Background()

// NATS pool variable
var nc *nats.Conn

// Database Pool Variable
var db *gorm.DB

// Redis pool variable
var rdb *redis.Client

// s3 session variable
var s3Session *session.Session
var s3Public string
var s3Endpoint string

func InitNatsConnection(natsUrl string) {
	var natsErr error                   // natsUrl from .env » "nats://localhost:4222"
	nc, natsErr = nats.Connect(natsUrl) // connect to nats
	if natsErr != nil {
		log.Fatal("Fatal error happened while initial connection NATS »", natsErr)
	}
}

func InitDbConnection(dbConnString string) {
	var dbErr error
	// db, dbErr = gorm.Open(sqlite.Open(dbConnString), &gorm.Config{})
	db, dbErr = gorm.Open(postgres.Open(dbConnString), &gorm.Config{})
	if dbErr != nil {
		log.Panic("Fatal error happened while initial connection Database » ", dbErr)
	}
}

func InitRedisConnection(redisConnString string) {
	rdb = redis.NewClient(&redis.Options{
		Addr: redisConnString, // redisConnStr from .env » host:port
	})
	// ping redis for check connection
	_, err := rdb.Ping(ctxPool).Result()
	if err != nil {
		log.Fatal("Fatal error happened while initial connection Redis » ", err)
	}
}

type S3Config struct {
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	Endpoint  string
}

func OpenS3Session(s3Config *S3Config) {
	var sessErr error
	creds := credentials.NewStaticCredentials(s3Config.AccessKey, s3Config.SecretKey, "")
	s3Session, sessErr = session.NewSession(&aws.Config{
		Region:           aws.String(s3Config.Region),
		Endpoint:         aws.String(s3Config.Endpoint),
		Credentials:      creds,
		S3ForcePathStyle: aws.Bool(true),
	})
	if sessErr != nil {
		log.Fatal("Fatal error happened while initial connection Minio »", sessErr)
	}
}

// Product Gorm struct
type Product struct {
	*gorm.Model
	Name     string `gorm:"type:varchar(255);not null" json:"name" binding:"required"`
	PhotoKey string `gorm:"type:varchar(255);not null" json:"photo_key" binding:"required"`
}

// CreateProductDto
type CreateProductDto struct {
	Name         string                `form:"name" binding:"required" validate:"min=1,max=255,required"`
	ProductPhoto *multipart.FileHeader `form:"product_photo" binding:"required" validate:"required"`
}

func main() {
	// load env
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file.")
	}

	// get env variables
	dbConnString := os.Getenv("DBCONNSTR")
	natsUrl := os.Getenv("NATS_URL")
	redisConnString := os.Getenv("REDISCONNSTR")
	s3_region := os.Getenv("S3_REGION")
	s3_bucket := os.Getenv("S3_BUCKET")
	s3_access_key := os.Getenv("S3_ACCESS_KEY")
	s3_secret_key := os.Getenv("S3_SECRET_KEY")
	s3Endpoint = os.Getenv("S3_ENDPOINT")
	s3Public = os.Getenv("S3_BUCKET")

	// init nats connection
	InitNatsConnection(natsUrl)
	// subscribe "post.created" to nats
	nc.Subscribe("post.created", func(m *nats.Msg) {
		log.Println("Received a message from NATS post.created: " + string(m.Data))
	})

	// init redis connection
	InitRedisConnection(redisConnString)

	// init db connection
	InitDbConnection(dbConnString)
	// init migrations
	db.AutoMigrate(&Product{})

	// init s3 session
	OpenS3Session(&S3Config{
		Region:    s3_region,
		Bucket:    s3_bucket,
		AccessKey: s3_access_key,
		SecretKey: s3_secret_key,
		Endpoint:  s3Endpoint,
	})

	// init router
	r := gin.Default()

	// hello
	r.GET("/", func(c *gin.Context) {
		// return hello world json
		c.JSON(http.StatusOK, gin.H{
			"message": "Hello World",
		})
		return
	})
	r.GET("/products", GetProductsHandler)
	r.GET("/cache/:cache_id", GetProductByCacheIdHandler)
	r.POST("/products", CreateProductHandler)
	r.DELETE("/products/:id", DeleteProductHandler)

	// start server
	APP_PORT := os.Getenv("APP_PORT")
	if APP_PORT == "" {
		APP_PORT = "9090"
	}
	if err := r.Run(":" + APP_PORT); err != nil {
		log.Fatal(err)
	}
}

// GET /products - GetProductsHandler - get all products
func GetProductsHandler(c *gin.Context) {
	// pagination params
	var page int
	var limit int
	var err error
	page, err = strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil && page < 1 {
		page = 1
	}
	limit, err = strconv.Atoi(c.DefaultQuery("limit", "10"))
	if err != nil && limit < 1 {
		limit = 10
	}

	// get products
	var products []Product
	db.Limit(limit).Offset((page - 1) * limit).Find(&products)

	// return response as { "products": [{},{}] }
	c.JSON(http.StatusOK, gin.H{
		"type":     "get-products",
		"message":  "Products fetched successfully",
		"products": products,
	})
}

// POST /products - CreateProductHandler - Create product
func CreateProductHandler(ctx *gin.Context) {
	// 1.1- validate req.body with binding
	var createProductDto CreateProductDto
	if err := ctx.Bind(&createProductDto); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"type":    "request-binding",
			"message": "Invalid request body",
			"error":   err.Error(),
		})
		return
	}
	// 1.2- validate req.body with validation
	if err := validator.New().Struct(createProductDto); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"type":    "request-validation",
			"message": "Invalid request body",
			"error":   err.Error(),
		})
		return
	}
	// 2 - read file from request
	fileHeader := createProductDto.ProductPhoto
	file, err := fileHeader.Open()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"type":    "file-open",
			"message": "Ensure validate file.",
			"error":   err.Error(),
		})
		return
	}
	var fileSize int64 = fileHeader.Size
	var fileName string = fileHeader.Filename
	// 3 - create file buffer and upload file to minio(s3)
	buffer := make([]byte, fileSize)
	file.Read(buffer)

	_, err = s3.New(s3Session).PutObject(&s3.PutObjectInput{
		Bucket:             aws.String(s3Public),
		Key:                aws.String(fileName),
		Body:               bytes.NewReader(buffer),
		ContentLength:      aws.Int64(fileSize),
		ContentType:        aws.String(http.DetectContentType(buffer)),
		ContentDisposition: aws.String("attachment"),
	})
	if err != nil {
		log.Println(err)
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"type":    "file-upload-cdn",
			"message": "Error uploading file to CDN",
			"error":   err.Error(),
		})
		return
	}
	imageRealUrl := s3Endpoint + "/" + s3Public + "/" + fileName

	// 4 - create database object and save.
	product := Product{
		Name:     createProductDto.Name,
		PhotoKey: imageRealUrl,
	}
	db.Create(&product)
	if product.ID == 0 {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"type":    "database-error",
			"message": "Error creating product on database",
			"error":   "Check stdout for more details",
		})
		return
	}

	// 5 - Save product id to redis with key: product:id for 60 sec
	productId := strconv.Itoa(int(product.ID))
	rdb.Set(ctxPool, productId, fileName, time.Second*60)

	// 5 - publish event to nats (publish product name...)
	// it is not required to publish. We do it for testing so dont handle error
	_ = nc.Publish("product.created", []byte(product.Name))

	// 6 - return response
	ctx.JSON(http.StatusOK, gin.H{
		"type":           "create-product",
		"message":        "File uploaded successfully " + fileName,
		"product":        product,
		"cache_id":       productId,
		"image_temp_url": "/cache/" + productId,
		"image_real_url": imageRealUrl,
	})
}

// GET /cache/:cache_id - GetProductByCacheIdHandler - Get Product Image
func GetProductByCacheIdHandler(ctx *gin.Context) {
	key := ctx.Param("cache_id")
	// 1 - get product id from redis
	productPath, err := rdb.Get(ctxPool, key).Result()
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{
			"type":    "file-cache",
			"message": "File not found in Redis",
			"error":   err.Error(),
		})
		return
	}
	// 2 - get file from s3
	result, err := s3.New(s3Session).GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s3Public),
		Key:    aws.String(productPath),
	})
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{
			"type":    "file-not-exist",
			"message": "Error getting file! Is the file exist, or is the key correct?",
			"error":   err.Error(),
		})
		return
	}

	// 3 - write file to response
	ctx.Header("Content-Disposition", "attachment; filename="+productPath)
	ctx.DataFromReader(http.StatusOK, int64(*result.ContentLength), *result.ContentType, result.Body, nil)
}

// DELETE /products/:id - DeleteProductHandler - Delete Product
func DeleteProductHandler(ctx *gin.Context) {
	// 1 - get product id from url
	id, err := strconv.Atoi(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"type":    "request-validation",
			"message": "Invalid product id",
			"error":   err.Error(),
		})
		return
	}
	// 2 - get product from database
	var product Product
	db.First(&product, id)
	if product.ID == 0 {
		ctx.JSON(http.StatusNotFound, gin.H{
			"type":    "product-not-found",
			"message": "Product not found in database",
			"error":   "Check stdout for more details",
		})
		return
	}
	// 3- delete image from s3
	_, err = s3.New(s3Session).DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(s3Public),
		Key:    aws.String(product.PhotoKey),
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"type":    "file-delete-cdn",
			"message": "Error deleting file from CDN",
			"error":   err.Error(),
		})
		return
	}
	// 4 - delete product from database and check if deleted
	tx := db.Delete(&product)
	// deleted! check if deleted
	if tx.Error != nil {
		// check error is record not found
		switch tx.Error {
		case gorm.ErrRecordNotFound:
			ctx.JSON(http.StatusNotFound, gin.H{
				"type":    "product-not-found",
				"message": "Product not found",
				"error":   "Check stdout for more details",
			})
			return
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{
				"type":    "database-error",
				"message": "Error deleting product",
				"error":   "Check stdout for more details",
			})
			return
		}
	}
	// 5 - return response
	ctx.JSON(http.StatusOK, gin.H{
		"type":    "delete-product",
		"message": "Product deleted successfully",
	})
}
