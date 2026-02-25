package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

const MAX_KEYS = 1000

// feedByKey: S3 key -> full URL; used to know if we already have a key when listing again.
var feedByKey map[string]string
var feedByKeyMu sync.RWMutex

// requestSeen: client key (query param) -> set of URLs we've already returned to that key.
var requestSeen map[string]map[string]struct{}
var requestSeenMu sync.Mutex

// voteRequest is the JSON body for POST /vote.
type voteRequest struct {
	Key          string `json:"key"`            // client identifier (who is voting)
	NamuIsTuxedo bool   `json:"namu_is_tuxedo"` // true if voter thinks namu is the tuxedo cat
}

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

	// PostgreSQL: credentials via env vars (do not commit .env; in production consider a secret manager).
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL must be set for the voting database")
	}
	dbPool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("postgres connect: %v", err)
	}
	defer dbPool.Close()
	if err := dbPool.Ping(context.Background()); err != nil {
		log.Fatalf("postgres ping: %v", err)
	}
	_, err = dbPool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS votes (
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			key TEXT NOT NULL,
			namu_is_tuxedo BOOLEAN DEFAULT FALSE,
			vote_count INTEGER DEFAULT 0,

			PRIMARY KEY (key)
		);
	`)
	if err != nil {
		log.Fatalf("create votes table: %v", err)
	}
	log.Print("postgres connected and votes table ready")

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

	feedByKey = make(map[string]string)
	{
		input := &s3.ListObjectsV2Input{
			Bucket:  aws.String(bucket),
			MaxKeys: aws.Int32(MAX_KEYS),
		}
		out, err := s3Client.ListObjectsV2(context.TODO(), input)
		if err != nil {
			log.Fatalf("startup list objects: %v", err)
		}
		for _, obj := range out.Contents {
			if obj.Key != nil && *obj.Key != "" {
				key := *obj.Key
				if _, ok := feedByKey[key]; !ok {
					feedByKey[key] = publicBaseURL + "/" + key
				}
			}
		}
		log.Printf("loaded %d feed URLs at startup", len(feedByKey))
	}
	requestSeen = make(map[string]map[string]struct{})

	corsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
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
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}

		clientKey := r.URL.Query().Get("key")
		if clientKey == "" {
			http.Error(w, "key required", http.StatusBadRequest)
			return
		}

		feedByKeyMu.RLock()
		allURLs := make([]string, 0, len(feedByKey))
		for _, u := range feedByKey {
			allURLs = append(allURLs, u)
		}
		feedByKeyMu.RUnlock()
		n := len(allURLs)
		if n == 0 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"urls": []string{}})
			return
		}
		if limit > n {
			limit = n
		}

		requestSeenMu.Lock()
		seen, ok := requestSeen[clientKey]
		if !ok {
			seen = make(map[string]struct{})
			requestSeen[clientKey] = seen
		}
		available := make([]string, 0, n)
		for _, u := range allURLs {
			if _, sent := seen[u]; !sent {
				available = append(available, u)
			}
		}
		if len(available) == 0 {
			for u := range seen {
				delete(seen, u)
			}
			available = append(available[:0], allURLs...)
		}

		log.Printf("new request: key=%s limit=%d available=%d seen=%d", clientKey, limit, len(available), len(seen))

		count := limit
		if count > len(available) {
			count = len(available)
		}
		idx := rand.Perm(len(available))
		out := make([]string, count)
		for i := 0; i < count; i++ {
			u := available[idx[i]]
			out[i] = u
			seen[u] = struct{}{}
		}
		requestSeenMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"urls": out})
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
		feedByKeyMu.Lock()
		feedByKey[key] = publicBaseURL + "/" + key
		feedByKeyMu.Unlock()
		log.Printf("successfully uploaded to R2: key=%s", key)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"key": key})
	})

	http.HandleFunc("/vote", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req voteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Key == "" {
			http.Error(w, "key required", http.StatusBadRequest)
			return
		}
		_, err := dbPool.Exec(context.Background(),
			`INSERT INTO votes (key, namu_is_tuxedo, vote_count) VALUES ($1, $2, 1)
			 ON CONFLICT (key) DO UPDATE SET namu_is_tuxedo = $2, updated_at = NOW(), vote_count = votes.vote_count + 1`,
			req.Key, req.NamuIsTuxedo)
		if err != nil {
			log.Printf("vote insert: %v", err)
			http.Error(w, "vote failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"ok": "voted"})
	})

	http.HandleFunc("/consensus", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rows, err := dbPool.Query(context.Background(), `
			SELECT namu_is_tuxedo, COUNT(*) AS cnt
			FROM votes
			GROUP BY namu_is_tuxedo
		`)
		if err != nil {
			log.Printf("consensus query: %v", err)
			http.Error(w, "consensus failed", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var namuTuxedoCount, namuNotTuxedoCount int64
		for rows.Next() {
			var isTuxedo bool
			var cnt int64
			if err := rows.Scan(&isTuxedo, &cnt); err != nil {
				log.Printf("consensus scan: %v", err)
				http.Error(w, "consensus failed", http.StatusInternalServerError)
				return
			}
			if isTuxedo {
				namuTuxedoCount = cnt
			} else {
				namuNotTuxedoCount = cnt
			}
		}
		if err := rows.Err(); err != nil {
			log.Printf("consensus rows: %v", err)
			http.Error(w, "consensus failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"namu_is_tuxedo":     namuTuxedoCount,
			"namu_is_not_tuxedo": namuNotTuxedoCount,
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, corsMiddleware(http.DefaultServeMux)))
}
