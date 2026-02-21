package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Fatalln("Error loading .env")
	}

	accountID := os.Getenv("R2_ACCOUNT_ID")
	accessKeyID := os.Getenv("R2_ACCESS_KEY_ID")
	secretKey := os.Getenv("R2_ACCESS_KEY_SECRET")
	bucket := os.Getenv("R2_BUCKET")
	publicBaseURL := os.Getenv("R2_PUBLIC_BASE_URL")
	for _, v := range []string{accountID, accessKeyID, secretKey, bucket} {
		if v == "" {
			log.Fatal("R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_ACCESS_KEY_SECRET, R2_BUCKET must be set")
		}
	}
	if publicBaseURL == "" {
		log.Fatal("R2_PUBLIC_BASE_URL must be set (e.g. https://pub-xxx.r2.dev or custom domain)")
	}
	publicBaseURL = strings.TrimSuffix(publicBaseURL, "/")

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretKey, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		log.Fatal(err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID))
	})

	corsOrigin := os.Getenv("CORS_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "*"
	}
	corsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	http.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := 5
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		cursor := r.URL.Query().Get("cursor")

		input := &s3.ListObjectsV2Input{
			Bucket:  aws.String(bucket),
			MaxKeys: aws.Int32(int32(limit)),
		}
		if cursor != "" {
			input.ContinuationToken = aws.String(cursor)
		}

		out, err := s3Client.ListObjectsV2(context.TODO(), input)
		if err != nil {
			log.Printf("list objects failed: %v", err)
			http.Error(w, "list failed", http.StatusInternalServerError)
			return
		}

		urls := make([]string, 0, len(out.Contents))
		for _, obj := range out.Contents {
			if obj.Key != nil && *obj.Key != "" {
				urls = append(urls, publicBaseURL+"/"+*obj.Key)
			}
		}

		resp := map[string]interface{}{"urls": urls}
		if out.NextContinuationToken != nil && *out.NextContinuationToken != "" {
			resp["nextCursor"] = *out.NextContinuationToken
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		file, header, err := r.FormFile("image")
		if err != nil {
			http.Error(w, "missing or invalid form field 'image'", http.StatusBadRequest)
			return
		}
		defer file.Close()

		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		key := filepath.Base(header.Filename)
		if key == "" || key == "." {
			ext := strings.ToLower(filepath.Ext(header.Filename))
			if ext == "" {
				ext = ".jpg"
			}
			key = fmt.Sprintf("%s-%s%s", time.Now().Format("2006-01-02"), time.Now().Format("150405"), ext)
		}
		log.Printf("new file received: filename=%s key=%s", header.Filename, key)

		_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			Body:        file,
			ContentType: aws.String(contentType),
			ACL:         types.ObjectCannedACLPublicRead,
		})
		if err != nil {
			log.Printf("upload failed: %v", err)
			http.Error(w, "upload failed", http.StatusInternalServerError)
			return
		}
		log.Printf("successfully uploaded to R2: key=%s", key)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"key": key})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, corsMiddleware(http.DefaultServeMux)))
}
